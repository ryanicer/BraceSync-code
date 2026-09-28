//go:build integration
// +build integration

// T447：迁移 000031 给 install_records.wifi_status 扩到的四值在真库（PG15）里的语义。
//
// 与单测的分工（同 T396 那套三层）：
//   - model/wifi_status_t447_test.go 证「Go 常量 == 迁移文本值集 == shared-types 联合类型」（无需 Docker）；
//   - service/handler 那两份证「脏值回 400 且一次都不落库」（内存桩，桩不执行 CHECK）；
//   - 本文件只证单测证不了的那一半：约束真在库里、值集真是四个、四个值真写得进去、
//     第五个值真被 23514 拒、并且存量行一条没被映射改动。
//
// 「脏值直接写 SQL」这条路在应用层是走不到的（service 先拦），这里绕过 service 是故意的：
// 要测的就是最后一道门本身还在。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

const t447Constraint = "install_records_wifi_status_check"

// t447Devices 本文件专用设备号（每个用例一台，避免用例行互相污染 wifi_status 现值）
func t447Device(i int) string { return fmt.Sprintf("DEV-T447-IT-%d", i) }

// t447Install 造一条安装记录现场：注册设备 → 建记录，返回 install_id。
//
// 故意不做设备绑定：Bind 的判据是「一名患者同时只能绑一台设备」（repo.go:37 alreadyBoundError），
// 而 itPatient 在本包里已被别的用例绑走（首版在这里 require.NoError 判红 3 例，CI 实测）。
// install_records 只要 device_id / patient_id / tech_id 三个 FK 有行即可写入，
// 患者与技师行由 seedITData 提供，设备行由下面 itRegister 提供 —— 绑定与本卡判据无关。
func t447Install(t *testing.T, store Store, i int) int64 {
	t.Helper()
	ctx := context.Background()
	dev := t447Device(i)
	itRegister(ctx, t, store, dev)

	id, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: dev, PatientID: itPatient, TechID: itTech,
		CalibrateTime: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	return id
}

func t447WifiOf(t *testing.T, installID int64) string {
	t.Helper()
	var got string
	require.NoError(t, itPool.QueryRow(context.Background(),
		`SELECT wifi_status FROM install_records WHERE install_id = $1`, installID).Scan(&got))
	return got
}

// TestITT447WifiStatusCheckInCatalog 约束存在性 + 值集：防「迁移编号被跳过 / 约束改名 / CHECK 少值」。
// 单测比的是迁移**文本**，这里比的是库里的**实际定义** —— 只有真库能发现「SQL 写了但没跑 up」。
func TestITT447WifiStatusCheckInCatalog(t *testing.T) {
	ctx := context.Background()
	var (
		name     string
		def      string
		convalid bool
		comment  string
	)
	err := itPool.QueryRow(ctx, `
SELECT c.conname, pg_get_constraintdef(c.oid), c.convalidated,
       COALESCE(obj_description(c.oid, 'pg_constraint'), '')
  FROM pg_constraint c
 WHERE c.conrelid = 'install_records'::regclass AND c.contype = 'c' AND c.conname = $1`,
		t447Constraint).Scan(&name, &def, &convalid, &comment)
	require.NoError(t, err, "库里找不到 %s（迁移 000031 没跑 up，或约束名漂移）", t447Constraint)

	assert.Equal(t, t447Constraint, name)
	assert.True(t, convalid, "convalidated=f 说明约束没对存量行校验过，等于没生效")
	// 迁移 000031 用 COMMENT ON CONSTRAINT 落了「值集与哪三处同源」的现场说明；
	// 注释读得到 = 这条约束是被 T447 有意收口的，而不是历史遗留
	assert.Contains(t, comment, "T447", "约束注释丢失或与本卡无关：实际=%q", comment)

	// 值集按「集合相等」比，不比整串定义，也不做 ToUpper/ToLower 归一：
	// pg_get_constraintdef 的排版（ARRAY 形态、::character varying 类型标注、空格）随版本变，
	// 而归一大小写正好会放过本卡要防的那类脏值（'CONNECTED' 在库外是 400，进不来；
	// 若哪天有人把大写值加进 CHECK，只有区分大小写的集合比对拦得住）。
	assert.ElementsMatch(t,
		[]string{
			model.WifiStatusConnected, model.WifiStatusUnconfigured,
			model.WifiStatusFailed, model.WifiStatusSkipped,
		},
		t447QuotedLiterals(def), "库内 CHECK 的值集与四值不再相等：实际定义=%s", def)
}

// t447QuotedLiterals 取出定义里所有单引号字面量（字面量内的引号按 SQL 惯例写成连续两个单引号），
// 并剔掉类型标注 'character varying' 这类不是取值的成员。
func t447QuotedLiterals(def string) []string {
	var out []string
	inLit := false
	var cur strings.Builder
	for i := 0; i < len(def); i++ {
		if def[i] == '\'' {
			if inLit && i+1 < len(def) && def[i+1] == '\'' { // '' = 字面量内的单引号
				cur.WriteByte('\'')
				i++
				continue
			}
			if inLit {
				out = append(out, cur.String())
				cur.Reset()
			}
			inLit = !inLit
			continue
		}
		if inLit {
			cur.WriteByte(def[i])
		}
	}
	var vals []string
	for _, s := range out {
		if s == "character varying" {
			continue
		}
		vals = append(vals, s)
	}
	return vals
}

// TestITT447WifiStatusCheckAcceptsAllFourValues 反证：四值全部写得进、逐字存得住。
// 少一条通过 = 新枚举打断了对应状态（卡面红线）。
func TestITT447WifiStatusCheckAcceptsAllFourValues(t *testing.T) {
	ctx := context.Background()
	store := newITStore()

	t.Cleanup(func() {
		_, _ = itPool.Exec(context.Background(),
			`DELETE FROM install_records WHERE device_id LIKE 'DEV-T447-IT-%'`)
	})

	for i, v := range []string{
		model.WifiStatusConnected,
		model.WifiStatusUnconfigured,
		model.WifiStatusFailed,
		model.WifiStatusSkipped,
	} {
		id := t447Install(t, store, i)
		val := v
		require.NoError(t, store.UpdateInstallMeta(ctx, id, nil, nil, &val),
			"枚举内取值 %q 被 CHECK 拒了：值集与实际写入不一致", val)

		got := t447WifiOf(t, id)
		assert.Equal(t, val, got, "入库值必须与写入方逐字相等（大小写敏感）")
		assert.LessOrEqual(t, len(val), 16, "列宽 VARCHAR(16) 没动，新增值必须仍在列宽内")
	}
}

// TestITT447WifiStatusCheckRejectsFifthValue 第五个取值（含大小写脏值）绕过应用层直写也必拒，
// 且报的是本约束、库里不留行。
func TestITT447WifiStatusCheckRejectsFifthValue(t *testing.T) {
	ctx := context.Background()
	store := newITStore()

	t.Cleanup(func() {
		_, _ = itPool.Exec(context.Background(),
			`DELETE FROM install_records WHERE device_id LIKE 'DEV-T447-IT-%'`)
	})

	for i, bad := range []string{"CONNECTED", "Skipped", "skip", "configuring", "已连接", "unknown_type", ""} {
		id := t447Install(t, store, 100+i)
		// 先固定成合法值，才能区分「被 CHECK 拒」与「NOT NULL/类型错抢先」
		before := t447WifiOf(t, id)
		require.Equal(t, model.WifiStatusUnconfigured, before, "建记录时列默认值不再是 unconfigured")

		// 绕过 service：脏值若经应用层，早在 400 处就被拦（那是 wifi_status_t447_test.go 的判据）
		_, err := itPool.Exec(ctx,
			`UPDATE install_records SET wifi_status = $2 WHERE install_id = $1`, id, bad)
		require.Error(t, err, "wifi_status=%q 竟写进了库里", bad)

		var pe *pgconn.PgError
		require.True(t, errors.As(err, &pe), "报错不是 PG 错误，无法归因到约束：%v", err)
		assert.Equal(t, "23514", pe.SQLState(), "check_violation 之外的错误码说明拦它的不是本约束：%s", pe.Message)
		assert.Equal(t, t447Constraint, pe.ConstraintName,
			"必须由 wifi_status 的 CHECK 拦截（若被列宽或 NOT NULL 抢先，本收口等于没做）")

		assert.Equal(t, model.WifiStatusUnconfigured, t447WifiOf(t, id),
			"wifi_status=%q 被拒后原值必须还在", bad)
	}
}

// TestITT447WifiStatusNilMeansNoColumnTouch COALESCE 通路在真库上的语义：
// nil 不能把该列写成 NULL（列是 NOT NULL，真写成 NULL 会直接报错），也不能洗成默认值。
func TestITT447WifiStatusNilMeansNoColumnTouch(t *testing.T) {
	ctx := context.Background()
	store := newITStore()

	t.Cleanup(func() {
		_, _ = itPool.Exec(context.Background(),
			`DELETE FROM install_records WHERE device_id LIKE 'DEV-T447-IT-%'`)
	})

	id := t447Install(t, store, 200)
	skipped := model.WifiStatusSkipped
	require.NoError(t, store.UpdateInstallMeta(ctx, id, nil, nil, &skipped))
	require.Equal(t, model.WifiStatusSkipped, t447WifiOf(t, id))

	notes := "T447 只改备注"
	require.NoError(t, store.UpdateInstallMeta(ctx, id, &notes, nil, nil))
	assert.Equal(t, model.WifiStatusSkipped, t447WifiOf(t, id),
		"只回填 notes 时 wifi_status 必须保持原值（COALESCE 语义）")

	var gotNotes *string
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT notes FROM install_records WHERE install_id = $1`, id).Scan(&gotNotes))
	require.NotNil(t, gotNotes)
	assert.Equal(t, notes, *gotNotes)
}

// TestITT447WifiStatusLegacyRowsUnchanged 存量值映射＝空集这条判据的库侧证据：
// 迁移不改任何既有行，旧两值原样保留，库里不存在第四个值之外的行。
func TestITT447WifiStatusLegacyRowsUnchanged(t *testing.T) {
	ctx := context.Background()

	var total, outside int
	require.NoError(t, itPool.QueryRow(ctx, `
SELECT COUNT(*), COUNT(*) FILTER (
       WHERE wifi_status NOT IN ('connected', 'unconfigured', 'failed', 'skipped'))
  FROM install_records`).Scan(&total, &outside))
	assert.Zero(t, outside, "库里存在四值之外的 wifi_status：迁移没把 CHECK 覆盖到全部行")

	// 迁移前的旧集合（connected/unconfigured）在 seed 里真存在 ⇒ 本用例不是空表自证
	var old int
	require.NoError(t, itPool.QueryRow(ctx, `
SELECT COUNT(*) FROM install_records
 WHERE wifi_status IN ('connected', 'unconfigured')`).Scan(&old))
	assert.Positive(t, old, "seed 安装记录一条都没有，本用例就证不了「存量没被动过」（空表自证）")
	assert.GreaterOrEqual(t, total, old)
}
