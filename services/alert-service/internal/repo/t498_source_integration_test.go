//go:build integration
// +build integration

// Package repo T498：告警侧来源印章的真库回归（迁移 000033 的 alerts 半边）。
//
// repo/t498_source_sql_test.go 已经在无库条件下钉住了 createAlertSQL 的字面形状，
// 但那一半证不了下面这四件只有真库才认账的事：
//
//	① NULLIF($9,'') 真的把空串落成 NULL —— 扫描器派生告警走的就是这条空值支，
//	  库里落成空串就同时踩两个坑：读侧分不出「未表态」，且过不了 CHECK；
//	② alerts 上的 CHECK 确实建起来了、且只管值域不管默认值（迁移刻意没写 DEFAULT）；
//	③ 去重键 uk_alerts_natural 不认来源 —— 同一 (patient, device, type, ts) 换个来源
//	  也必须判重复，否则一次注入就能把真告警的「重复计数」绕开；
//	④ 加列之后既有读方（ListAlerts 的显式列清单）不受影响 —— 迁移注释里那句
//	  「既有读方全部走显式列清单」在告警侧也得有真库反证，不能只靠肉眼看 SQL。
//
// 需要 Docker（testcontainers）。本机无 Docker 时按交付说明在本机上用
// 临时 PG14 实例以「无 tag 副本 + TestMain 桩」的方式预演过，见交件单。
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

const (
	t498AlertPatient = "P-T498-AL"
	t498AlertDevice  = "D-T498-AL"
)

// t498AlertTs 用 2026-08 的固定时刻：CI runner 的真实日历不在业务年内，
// 任何「按 now() 相对偏移」的造数都会被别的用例的时间窗扫到。
func t498AlertTs(h int) time.Time {
	return time.Date(2026, 8, 20, h, 5, 0, 0, time.UTC)
}

// seedT498AlertOwner 建 alerts 的两条外键前置（patients/devices）。
// 用独立 ID，不复用 itDevice/itPatient：那些行被同包其它用例按「全表 alerts」计数，
// 本用例插的告警会连坐它们的断言。
func seedT498AlertOwner(ctx context.Context, t *testing.T) {
	t.Helper()
	// 两个占位符各写各的：同一个 $1 既落 varchar 又进 md5() 会让 PG 推不出参数类型
	// （T366 在 CI 上实测过，SQLSTATE 42P08，写 ::text 也消不掉）
	_, err := itPool.Exec(ctx, `
		INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		VALUES ($1, 'T498 告警患者', '\x00'::bytea, md5($2) || repeat('0', 32), 'active')
		ON CONFLICT (patient_id) DO NOTHING`, t498AlertPatient, t498AlertPatient)
	require.NoError(t, err, "seed patient")

	_, err = itPool.Exec(ctx, `
		INSERT INTO devices (device_id, device_secret_enc, patient_id, status)
		VALUES ($1, '\x00'::bytea, $2, 'online')
		ON CONFLICT (device_id) DO NOTHING`, t498AlertDevice, t498AlertPatient)
	require.NoError(t, err, "seed device")

	t.Cleanup(func() {
		bgc := context.Background()
		_, _ = itPool.Exec(bgc, `DELETE FROM alerts WHERE device_id = $1`, t498AlertDevice)
		_, _ = itPool.Exec(bgc, `DELETE FROM devices WHERE device_id = $1`, t498AlertDevice)
		_, _ = itPool.Exec(bgc, `DELETE FROM patients WHERE patient_id = $1`, t498AlertPatient)
	})
}

func t498AlertInput(ts time.Time, source string) scanner.NewAlert {
	return scanner.NewAlert{
		PatientID:      t498AlertPatient,
		DeviceID:       t498AlertDevice,
		Type:           engine.TypePressureHigh,
		SensorPoint:    "P01",
		Detail:         "T498 集成告警",
		ThresholdValue: 45,
		ActualValue:    60,
		Ts:             ts,
		IngestSource:   source,
	}
}

// t498AlertSourceAt 读回印章列，返回 *string：NULL 必须是 nil 而不是空串，
// 否则「未表态」与「表态成空」在库里就分不开。
func t498AlertSourceAt(ctx context.Context, t *testing.T, ts time.Time) *string {
	t.Helper()
	var src *string
	err := itPool.QueryRow(ctx,
		`SELECT ingest_source FROM alerts WHERE device_id = $1 AND ts = $2`,
		t498AlertDevice, ts).Scan(&src)
	require.NoError(t, err, "读回告警来源列")
	return src
}

// TestITT498AlertSourceThreeValues 三种表态落成三种库内形状：
// real / mock 原样落，空串落 NULL。第三种是扫描器派生告警的实际写法
// （scanner.go 里两处 NewAlert 都刻意不填 IngestSource）。
func TestITT498AlertSourceThreeValues(t *testing.T) {
	ctx := context.Background()
	seedT498AlertOwner(ctx, t)
	repo := NewAlertRepo(itPool)

	for i, tc := range []struct {
		in       string
		wantZero bool // 期望库里是 NULL
	}{
		{in: "real", wantZero: false},
		{in: "mock", wantZero: false},
		{in: "", wantZero: true},
	} {
		id, created, err := repo.CreateAlert(ctx, t498AlertInput(t498AlertTs(i), tc.in))
		require.NoError(t, err, "CreateAlert source=%q", tc.in)
		assert.True(t, created, "三种来源各是一个新告警，不该判重复")
		assert.NotEmpty(t, id, "新建告警要回 alert_id")

		got := t498AlertSourceAt(ctx, t, t498AlertTs(i))
		if tc.wantZero {
			assert.Nil(t, got, `空串表态要落成 NULL（NULLIF 生效），实际 %q`, derefT498(got))
			continue
		}
		require.NotNil(t, got)
		assert.Equal(t, tc.in, *got)
	}
}

func derefT498(p *string) string {
	if p == nil {
		return "<NULL>"
	}
	return *p
}

// TestITT498AlertCheckBoundsTheColumn CHECK 只管值域、不管默认值：
// 合法两值写得进，其它词形被 23514 拦下；空串也被拦 —— 这一条反过来解释
// createAlertSQL 里那个 NULLIF 为什么是承重的：去掉它，扫描器派生告警就写不进库。
// 超长词形先撞列宽 VARCHAR(8)（22001），与值域是两条约束，分开写清。
func TestITT498AlertCheckBoundsTheColumn(t *testing.T) {
	ctx := context.Background()
	seedT498AlertOwner(ctx, t)

	for i, legal := range []string{"real", "mock"} {
		err := t498AlertRawInsert(ctx, t498AlertTs(10+i), legal)
		require.NoError(t, err, "合法值 %q 应写入", legal)
	}

	hour := 12
	for _, bad := range []string{"REAL", "true", "", "Mixed", "re al"} {
		err := t498AlertRawInsert(ctx, t498AlertTs(hour), bad)
		hour++
		require.Error(t, err, "非法值 %q 必须被拒", bad)
		assert.Contains(t, err.Error(), "23514", "非法值 %q 应由 CHECK 拒绝", bad)
		assert.Contains(t, err.Error(), "ingest_source", "报错要点名到列，否则排查找不到是哪条约束")
	}

	// 超过 VARCHAR(8) 的词形：先撞列宽（22001）再谈值域。这里要的是「被拒」这个结论，
	// 具体被哪条约束拒由 SQLSTATE 分开钉住，不混写成 23514。
	for _, tooLong := range []string{"seed_generated", "real_but_long_string_over_8"} {
		err := t498AlertRawInsert(ctx, t498AlertTs(hour), tooLong)
		hour++
		require.Error(t, err, "超长值 %q 必须被拒", tooLong)
		assert.Contains(t, err.Error(), "22001", "超长值 %q 应先撞列宽 VARCHAR(8)", tooLong)
	}

	// 未表态（不写这一列）必须能落 NULL：迁移没给 DEFAULT，读侧才分得出「存量行」
	err := t498AlertRawInsertNoColumn(ctx, t498AlertTs(20))
	require.NoError(t, err, "不写 ingest_source 要能落 NULL（无默认值）")
	assert.Nil(t, t498AlertSourceAt(ctx, t, t498AlertTs(20)))
}

func t498AlertRawInsert(ctx context.Context, ts time.Time, source string) error {
	_, err := itPool.Exec(ctx, `
		INSERT INTO alerts (patient_id, device_id, type, ts, ingest_source)
		VALUES ($1, $2, 'pressure_high', $3, $4)`,
		t498AlertPatient, t498AlertDevice, ts, source)
	return err
}

func t498AlertRawInsertNoColumn(ctx context.Context, ts time.Time) error {
	_, err := itPool.Exec(ctx, `
		INSERT INTO alerts (patient_id, device_id, type, ts)
		VALUES ($1, $2, 'pressure_high', $3)`,
		t498AlertPatient, t498AlertDevice, ts)
	return err
}

// TestITT498AlertNaturalKeyCrossesSources 去重键不认来源：真告警已在库里时，
// 同一自然键的注入告警必须判重复、且不许把已落库那行的印章改写掉。
// 正对照：换一个 ts 的注入告警要能建成（否则「判重复」可能是「根本写不进」的假绿）。
func TestITT498AlertNaturalKeyCrossesSources(t *testing.T) {
	ctx := context.Background()
	seedT498AlertOwner(ctx, t)
	repo := NewAlertRepo(itPool)

	_, created, err := repo.CreateAlert(ctx, t498AlertInput(t498AlertTs(30), "real"))
	require.NoError(t, err)
	require.True(t, created)

	id, dup, err := repo.CreateAlert(ctx, t498AlertInput(t498AlertTs(30), "mock"))
	require.NoError(t, err)
	assert.False(t, dup, "同一自然键换来源也必须判重复")
	assert.Empty(t, id, "重复时不返回 alert_id")

	src := t498AlertSourceAt(ctx, t, t498AlertTs(30))
	require.NotNil(t, src)
	assert.Equal(t, "real", *src, "重复帧不许改写已落库那行的印章")

	_, created2, err := repo.CreateAlert(ctx, t498AlertInput(t498AlertTs(31), "mock"))
	require.NoError(t, err)
	assert.True(t, created2, "正对照：换 ts 的注入告警要能建成")
}

// TestITT498AlertReadPathUnaffectedByNewColumn 加列后既有读方仍能正常投影。
// alertSelectColumns 是显式列清单（不含新列），ListAlerts 走它读一条 mock 来源告警：
// 行要读得出来、扫描要不报错。这条同时也是迁移注释里「加列不影响既有读方」的告警侧反证。
func TestITT498AlertReadPathUnaffectedByNewColumn(t *testing.T) {
	ctx := context.Background()
	seedT498AlertOwner(ctx, t)
	repo := NewAlertRepo(itPool)

	_, created, err := repo.CreateAlert(ctx, t498AlertInput(t498AlertTs(40), "mock"))
	require.NoError(t, err)
	require.True(t, created)

	rows, total, err := repo.ListAlerts(ctx, AlertQueryFilter{PatientID: t498AlertPatient})
	require.NoError(t, err, "ListAlerts 应能带着新列读")
	assert.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, t498AlertDevice, rows[0].DeviceID)
	assert.Equal(t, string(engine.TypePressureHigh), rows[0].Type)
	assert.True(t, t498AlertTs(40).Equal(rows[0].Ts), "读回的采集时间要与写入一致")
}
