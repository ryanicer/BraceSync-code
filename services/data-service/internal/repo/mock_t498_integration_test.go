//go:build integration
// +build integration

// Package repo T498：来源印章真库回归（迁移 000033 + 注入事务）
//
// 单测证得了「代码传了哪个字符串」，证不了下面这四件事，必须真库跑：
//  1. CHECK 值域真的在库侧兜住（应用层漏表态时库里留得下脏值吗）；
//  2. pressure_records 是分区表 —— 父表上加的列与约束会不会传播到现存分区、
//     以及**之后**由 cron 预建的分区（迁移注释里那句「无需改模板」是断言，不是事实）；
//  3. 幂等键 (device_id, ts) 跨来源生效：真帧不被注入帧改写，反之亦然；
//  4. 注入帧与审计行同事务 —— 审计写失败时帧必须一起消失（本卡刻意反过来：
//     真实链路审计失败只记 WARN，这里失败必须回滚）。
package repo

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

const (
	t498Patient = "P-T498-IT1"
	t498Device  = "D-T498-IT1"
)

// t498Ts 落在迁移 000001 预建的 pressure_records_202608 分区内。
// 刻意不用 now()：CI runner 的真实日历不在本项目的业务年份内，用相对时间会随机
// 撞上「no partition of relation found」，那种红与本卡无关却会被读成印章没生效。
func t498Ts(h int) time.Time {
	return time.Date(2026, 8, 20, h, 5, 0, 0, time.UTC)
}

func t498Frame(ts time.Time, v float32) PendingFrame {
	f := PendingFrame{Ts: ts, Battery: 80}
	for i := range f.Points {
		f.Points[i] = v
	}
	return f
}

func t498MockInput(deviceID string, tsHour int, reason, ip string) MockFrameInput {
	return MockFrameInput{
		DeviceID:  deviceID,
		PatientID: t498Patient,
		Frame:     t498Frame(t498Ts(tsHour), 2.0),
		Operator:  "it-ops",
		Reason:    reason,
		IP:        ip,
	}
}

// seedT498Patient 造患者+设备并注册清理。设备名可由调用方指定，
// 让「同一帧不同来源」这类用例各占一台设备，计数不被别的用例串味。
func seedT498Patient(ctx context.Context, t *testing.T, deviceID string) {
	t.Helper()
	// 两个占位符的写法照抄 seedT366Patient（同键既落 varchar 又进 md5() 会推不出类型）
	_, err := dashPool.Exec(ctx, `
		INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		VALUES ($1, 'T498 患者', '\x00'::bytea, md5($2) || repeat('0', 32), 'active')
		ON CONFLICT (patient_id) DO NOTHING`, t498Patient, t498Patient)
	require.NoError(t, err, "seed patient")

	_, err = dashPool.Exec(ctx, `
		INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		VALUES ($1, '\x00'::bytea, $2, 'online')
		ON CONFLICT (device_id) DO NOTHING`, deviceID, t498Patient)
	require.NoError(t, err, "seed device")

	t.Cleanup(func() {
		bgc := context.Background()
		for _, del := range []struct {
			sql string
			arg any
		}{
			// 审计先删：它的定位子查询要读 pressure_records，帧先没就找不到了
			{`DELETE FROM audit_logs WHERE target_type = 'pressure_record'
			   AND target_id IN (SELECT record_id::text FROM pressure_records WHERE device_id = $1)`, deviceID},
			{`DELETE FROM audit_logs WHERE operator_id = 'it-ops'`, nil},
			{`DELETE FROM pressure_records WHERE device_id = $1`, deviceID},
			{`DELETE FROM alerts WHERE patient_id = $1`, t498Patient},
			{`DELETE FROM devices WHERE device_id = $1`, deviceID},
			{`DELETE FROM patients WHERE patient_id = $1`, t498Patient},
		} {
			var err error
			if del.arg == nil {
				_, err = dashPool.Exec(bgc, del.sql)
			} else {
				_, err = dashPool.Exec(bgc, del.sql, del.arg)
			}
			if err != nil {
				t.Errorf("cleanup %s: %v", strings.TrimSpace(del.sql), err)
			}
		}
	})
}

// t498SourceAt 读回某设备某时刻的印章（NULL 读成 nil，不许退化成空串）
func t498SourceAt(ctx context.Context, t *testing.T, deviceID string, ts time.Time) *string {
	t.Helper()
	var src *string
	err := dashPool.QueryRow(ctx,
		`SELECT ingest_source FROM pressure_records WHERE device_id = $1 AND ts = $2`, deviceID, ts).
		Scan(&src)
	require.NoError(t, err, "按 (device_id, ts) 读不回帧行")
	return src
}

// t498InsertLegacyFrame 造一行「不带 ingest_source」的帧：迁移上线前的存量行与 seed 手工示例行就是这个形状。
// 🔴 p01..p20 全 NOT NULL（000001 建的，无默认值），少写一列会先炸 23502，
// 看起来像「印章列写不进去」，实际是列清单不全 —— 本机 PG14 预演实测。
func t498InsertLegacyFrame(ctx context.Context, t *testing.T, deviceID string, at time.Time) {
	t.Helper()
	_, err := dashPool.Exec(ctx, `
		INSERT INTO pressure_records (device_id, patient_id, ts,
		  p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
		  p11,p12,p13,p14,p15,p16,p17,p18,p19,p20, upload_time)
		VALUES ($1, $2, $3,
		  1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,
		  1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1, now())`, deviceID, t498Patient, at)
	require.NoError(t, err, "存量行形状（不含 ingest_source）写入失败")
}

// t498InsertSourcedFrame 直写一条带指定 ingest_source 的帧（绕过应用层，专测库侧约束）。
// 与 t498InsertLegacyFrame 共用同一份 20 点列清单 —— 少一列就先撞 NOT NULL，测不到 CHECK。
func t498InsertSourcedFrame(ctx context.Context, deviceID string, at time.Time, source string) error {
	_, err := dashPool.Exec(ctx, `
		INSERT INTO pressure_records (device_id, patient_id, ts,
		  p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
		  p11,p12,p13,p14,p15,p16,p17,p18,p19,p20, ingest_source)
		VALUES ($1, $2, $3,
		  1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,
		  1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1,1.1, $4)`, deviceID, t498Patient, at, source)
	return err
}

// TestITT498ThreeWritePathsStampThreeValues 三条写通路的印章取值：
// 单帧真实上报 real / 批量补传 real（SQL 里写死的字面量）/ 受控注入 mock / 手写示例行 NULL。
// 这四格是本卡「库里分得开真假」的全部依据，任何一格串味都让验收 3 直接失效。
func TestITT498ThreeWritePathsStampThreeValues(t *testing.T) {
	ctx := context.Background()
	seedT498Patient(ctx, t, t498Device)
	repo := NewRecordRepo(dashPool)

	_, inserted, err := repo.InsertRecord(ctx, t498Device, t498Patient, t498Frame(t498Ts(1), 1.5))
	require.NoError(t, err)
	require.True(t, inserted, "单帧真实上报应新插一行")

	_, err = repo.BatchInsert(ctx, t498Device, t498Patient, []PendingFrame{
		t498Frame(t498Ts(2), 1.6), t498Frame(t498Ts(3), 1.7),
	})
	require.NoError(t, err, "批量补传")

	res, err := repo.InsertMockFrame(ctx, t498MockInput(t498Device, 4, "boss acceptance", "10.1.2.3"))
	require.NoError(t, err)
	require.False(t, res.Duplicated)

	// seed 形态：不带该列的手写示例行（迁移上线前的存量行就是这个形状）
	t498InsertLegacyFrame(ctx, t, t498Device, t498Ts(5))

	cases := []struct {
		hour int
		want any
		desc string
	}{
		{1, model.IngestReal, "单帧真实上报"},
		{2, model.IngestReal, "批量补传第一帧"},
		{3, model.IngestReal, "批量补传第二帧"},
		{4, model.IngestMock, "受控注入"},
		{5, nil, "不带该列的手写行（seed/存量形状）"},
	}
	for _, c := range cases {
		got := t498SourceAt(ctx, t, t498Device, t498Ts(c.hour))
		if c.want == nil {
			assert.Nil(t, got, "%s：没表态的行必须是 NULL，不能默认成 real", c.desc)
			continue
		}
		require.NotNil(t, got, "%s：印章列读回为空 = 写侧没表态", c.desc)
		assert.Equal(t, c.want.(string), *got, c.desc)
	}

	// 注入那一帧必须同时留下审计行（同事务的另一半）
	var action, targetType, operator, ip, reason, detailSource string
	require.NoError(t, dashPool.QueryRow(ctx, `
		SELECT a.action, a.target_type, a.operator_id, a.ip,
		       a.detail->>'reason', a.detail->>'ingest_source'
		FROM audit_logs a WHERE a.log_id = $1`, res.AuditLogID).
		Scan(&action, &targetType, &operator, &ip, &reason, &detailSource),
		"响应体给的 audit_log_id 必须查得到行（双向对查的前提）")
	assert.Equal(t, "data_modify", action, "动词沿用 T252 五值表，不新造")
	assert.Equal(t, "pressure_record", targetType, "对象类型单数 snake_case（T485 登记表口径）")
	assert.Equal(t, "it-ops", operator, "自报操作者原样入库")
	assert.Equal(t, "10.1.2.3", ip, "客观那一半：服务端看到的来源 ip")
	assert.Equal(t, "boss acceptance", reason)
	assert.Equal(t, model.IngestMock, detailSource)
}

// TestITT498CheckConstraintBoundsTheColumn 值域由库侧 CHECK 兜住：
// 应用层哪天漏了表态、或新写入方塞进第三个词形，必须被库拒绝而不是静默进库。
func TestITT498CheckConstraintBoundsTheColumn(t *testing.T) {
	ctx := context.Background()
	seedT498Patient(ctx, t, t498Device)

	for _, b := range []string{"REAL", "true", "", "Mixed", "re al"} {
		err := t498InsertSourcedFrame(ctx, t498Device, time.Date(2026, 8, 21, 1, 0, 0, 0, time.UTC), b)
		require.Error(t, err, "值域外的 %q 竟然写进去了 = CHECK 没建上（大小写也在这格的值域内）", b)
		assert.Contains(t, err.Error(), "23514", "%q 的报错应是 CHECK 违例，实得 %v", b, err)
		assert.Contains(t, err.Error(), "ingest_source", "报错要点名到列，否则运维看不出是哪条约束")
	}

	// 超过 VARCHAR(8) 的词形：先撞列宽（22001）再谈值域。这一格单独分出来，
	// 是因为「被拒」是这条链路的结论，「被哪条约束拒」不是 —— 混在一起断言会写成假期望。
	for _, b := range []string{"seed_generated", "real_but_long_string_over_8"} {
		err := t498InsertSourcedFrame(ctx, t498Device, time.Date(2026, 8, 21, 1, 30, 0, 0, time.UTC), b)
		require.Error(t, err, "超长值 %q 竟然写进去了", b)
		assert.Contains(t, err.Error(), "22001", "%q 应因列宽被拒，实得 %v", b, err)
	}

	// 正对照：合法值与 NULL 都能写（否则上面的红是「这条 INSERT 本身写不进」的假阳性）
	for _, ok := range []string{model.IngestReal, model.IngestMock} {
		err := t498InsertSourcedFrame(ctx, t498Device, time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC), ok)
		require.NoError(t, err, "合法值 %q 被拒了", ok)
		_, _ = dashPool.Exec(ctx, `DELETE FROM pressure_records WHERE device_id = $1 AND ts = $2`,
			t498Device, time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC))
	}
}

// TestITT498PartitionInheritance 后建的分区也要继承印章列与 CHECK。
// 迁移注释里那句「由 cron 以 PARTITION OF 预建，自动继承，无需改模板」是这条用例的判据；
// 若哪天有人给分区加了显式列清单，这里就会红。
func TestITT498PartitionInheritance(t *testing.T) {
	ctx := context.Background()
	seedT498Patient(ctx, t, t498Device)

	const child = "pressure_records_it_t498_202612"
	from := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	_, err := dashPool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %s PARTITION OF pressure_records FOR VALUES FROM ('2026-12-01') TO ('2027-01-01')`, child))
	require.NoError(t, err, "预建分区失败（与 service/partition.go 同形状：PARTITION OF 父表）")
	t.Cleanup(func() {
		if _, dErr := dashPool.Exec(context.Background(), `DROP TABLE IF EXISTS `+child); dErr != nil {
			t.Errorf("drop partition %s: %v", child, dErr)
		}
	})

	var hasCol int
	require.NoError(t, dashPool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = $1 AND column_name = 'ingest_source'`, child).Scan(&hasCol))
	assert.Equal(t, 1, hasCol, "新分区没继承 ingest_source ⇒ 那个月的注入帧会因「列不存在」写失败")

	var conName string
	require.NoError(t, dashPool.QueryRow(ctx, `
		SELECT conname FROM pg_constraint
		WHERE conrelid = $1::regclass AND contype = 'c' AND conname LIKE '%ingest_source%'`, child).
		Scan(&conName))
	assert.NotEmpty(t, conName, "分区上没有 ingest_source 的 CHECK ⇒ 值域在子表上不生效")

	// 写一条落在该分区的注入帧：确实进了新分区，且印章带上
	res, err := NewRecordRepo(dashPool).InsertMockFrame(ctx, MockFrameInput{
		DeviceID:  t498Device,
		PatientID: t498Patient,
		Frame:     t498Frame(from.Add(3*time.Hour), 2.0),
		Operator:  "it-ops",
		Reason:    "partition leg",
		IP:        "10.1.2.4",
	})
	require.NoError(t, err)
	require.False(t, res.Duplicated)

	var parted string
	require.NoError(t, dashPool.QueryRow(ctx, `
		SELECT p.relname
		FROM pressure_records r JOIN pg_class p ON p.oid = r.tableoid
		WHERE r.record_id = $1`, res.RecordID).Scan(&parted))
	assert.Equal(t, child, parted, "行没落进新分区 = 分区边界或模板不对")

	src := t498SourceAt(ctx, t, t498Device, from.Add(3*time.Hour))
	require.NotNil(t, src)
	assert.Equal(t, model.IngestMock, *src, "落到后建分区上的注入帧印章照常")
}

// TestITT498IdempotencyKeyCrossesSources 幂等键跨来源生效：谁先来谁占位，
// 后到的一方只补审计行、不改写既有帧的来源。这一格挡住的是
// 「注入把真帧洗成 mock」和「真帧把注入痕迹冲掉」两个方向。
func TestITT498IdempotencyKeyCrossesSources(t *testing.T) {
	ctx := context.Background()
	seedT498Patient(ctx, t, t498Device)
	rec := NewRecordRepo(dashPool)

	t.Run("真帧先到，注入被挡", func(t *testing.T) {
		_, inserted, err := rec.InsertRecord(ctx, t498Device, t498Patient, t498Frame(t498Ts(6), 1.4))
		require.NoError(t, err)
		require.True(t, inserted)

		res, err := rec.InsertMockFrame(ctx, t498MockInput(t498Device, 6, "duplicate attempt", "10.1.2.5"))
		require.NoError(t, err)
		assert.True(t, res.Duplicated)
		assert.Equal(t, model.IngestReal, res.ExistingSource, "审计与响应都要说清「挡路的是真帧」")

		src := t498SourceAt(ctx, t, t498Device, t498Ts(6))
		require.NotNil(t, src)
		assert.Equal(t, model.IngestReal, *src, "注入撞键后真帧的来源章不许被改写")
	})

	t.Run("注入先到，真帧撞键也不改写", func(t *testing.T) {
		_, err := rec.InsertMockFrame(ctx, t498MockInput(t498Device, 7, "mock first", "10.1.2.6"))
		require.NoError(t, err)

		_, inserted, err := rec.InsertRecord(ctx, t498Device, t498Patient, t498Frame(t498Ts(7), 9.9))
		require.NoError(t, err)
		assert.False(t, inserted, "同一 (device_id, ts) 第二次写必须判重复")

		src := t498SourceAt(ctx, t, t498Device, t498Ts(7))
		require.NotNil(t, src)
		assert.Equal(t, model.IngestMock, *src, "已入库的注入帧不许被真实上报悄悄换掉")
	})

	t.Run("挡路的存量行没盖章", func(t *testing.T) {
		t498InsertLegacyFrame(ctx, t, t498Device, t498Ts(8))

		res, err := rec.InsertMockFrame(ctx, t498MockInput(t498Device, 8, "legacy collision", "10.1.2.7"))
		require.NoError(t, err)
		assert.True(t, res.Duplicated)
		assert.Equal(t, unstampedSource, res.ExistingSource,
			"NULL 存量行不许被读成 real 或 mock（值域外的记法只出现在审计 detail）")

		var detailSource *string
		require.NoError(t, dashPool.QueryRow(ctx, `
			SELECT detail->>'conflicting_ingest_source' FROM audit_logs WHERE log_id = $1`, res.AuditLogID).
			Scan(&detailSource))
		require.NotNil(t, detailSource, "撞未盖章存量行时审计 detail 必须留 conflicting_ingest_source")
		assert.Equal(t, unstampedSource, *detailSource)
	})
}

// TestITT498AuditFailureRollsBackFrame 审计写失败 ⇒ 注入帧一并回滚。
// 与真实链路相反的取向（那边审计失败只记 WARN）：这里的产出本身就是假数据，
// 没有留痕的假数据在库里等于「无法解释的真帧」。
func TestITT498AuditFailureRollsBackFrame(t *testing.T) {
	ctx := context.Background()
	seedT498Patient(ctx, t, t498Device)
	rec := NewRecordRepo(dashPool)

	longIP := strings.Repeat("9", 200) // audit_logs.ip 是 VARCHAR(45)
	_, err := rec.InsertMockFrame(ctx, t498MockInput(t498Device, 9, "audit must fail", longIP))
	require.Error(t, err, "审计行写失败却整条成功 = 事务没包住")
	assert.Contains(t, err.Error(), "write mock ingest audit", "报错要指明失败发生在审计那一步")

	var frames int
	require.NoError(t, dashPool.QueryRow(ctx,
		`SELECT count(*) FROM pressure_records WHERE device_id = $1 AND ts = $2`, t498Device, t498Ts(9)).
		Scan(&frames))
	assert.Zero(t, frames, "审计回滚后帧必须也不在库里")

	var audits int
	require.NoError(t, dashPool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE operator_id = 'it-ops' AND detail->>'reason' = 'audit must fail'`).
		Scan(&audits))
	assert.Zero(t, audits)

	// 正对照：同一台设备同一形状换条短 ip 必须能成，否则上面的 0 是「根本写不进」的假绿
	res, err := rec.InsertMockFrame(ctx, t498MockInput(t498Device, 9, "audit ok", "10.1.2.8"))
	require.NoError(t, err)
	assert.False(t, res.Duplicated)
	src := t498SourceAt(ctx, t, t498Device, t498Ts(9))
	require.NotNil(t, src)
	assert.Equal(t, model.IngestMock, *src)
}

// TestITT498DeleteByMarkerLeavesRealRows 运维收口那句「按 ingest_source='mock' 清掉注入帧」
// 在真库上只清得掉注入帧：真帧与存量未盖章行都不许被牵连。
func TestITT498DeleteByMarkerLeavesRealRows(t *testing.T) {
	ctx := context.Background()
	seedT498Patient(ctx, t, t498Device)
	rec := NewRecordRepo(dashPool)

	_, _, err := rec.InsertRecord(ctx, t498Device, t498Patient, t498Frame(t498Ts(10), 1.2))
	require.NoError(t, err, "真帧写入")
	_, err = rec.InsertMockFrame(ctx, t498MockInput(t498Device, 11, "to be cleaned", "10.1.2.9"))
	require.NoError(t, err, "注入帧写入")
	t498InsertLegacyFrame(ctx, t, t498Device, t498Ts(12))

	tag, err := dashPool.Exec(ctx, `DELETE FROM pressure_records WHERE device_id = $1 AND ingest_source = 'mock'`, t498Device)
	require.NoError(t, err)
	assert.EqualValues(t, 1, tag.RowsAffected(), "清理语句只应命中注入帧那一行")

	var realLeft, nullLeft, mockLeft int
	require.NoError(t, dashPool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE ingest_source = 'real'),
		       count(*) FILTER (WHERE ingest_source IS NULL),
		       count(*) FILTER (WHERE ingest_source = 'mock')
		FROM pressure_records WHERE device_id = $1`, t498Device).
		Scan(&realLeft, &nullLeft, &mockLeft))
	assert.Equal(t, 1, realLeft, "真帧被清理牵连 = 按来源清理反而毁了真实数据")
	assert.Equal(t, 1, nullLeft, "未盖章存量行不该被这条清理波及（它既非真帧也非注入帧）")
	assert.Zero(t, mockLeft)
}
