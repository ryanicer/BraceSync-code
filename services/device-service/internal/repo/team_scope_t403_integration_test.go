//go:build integration
// +build integration

// T403：安装记录读侧锚点在「转绑」现场的口径锁定（真库）。
//
// 卡面问的是「医护能在列表里看到本团队安装记录、点详情却 403」。取证结论（评论 1816）：
// 现状列表（ListInstallRecords，谓词 p.team_id，p 由 install_records.patient_id join 出来）
// 与详情探测（InstallInTeam，同一 join 链）同锚，族内不可能出现「列表可见、详情被拒」。
// 真正的分歧发生在跨资源：install_records.patient_id 是安装当时的历史患者（写侧从不回填），
// devices.patient_id 是现绑患者（Bind/Rebind/Unbind 会改）。设备转绑后两条锚点会分叉。
//
// T378 的种子里安装记录的患者恒等于设备的现绑患者，所以这条分歧在既有集成用例里从未出现过
// —— 也就是说「同锚」目前是两条 SQL 恰好同形的副产品，改任一侧都不会有人被拦下。
// 本文件把分歧现场造出来，锁三格：
//  1. 记录患者属 A、设备已转绑到 B：A 的列表可见且详情探测同为真（列表可见 ⇒ 详情可读）；
//  2. 同一条记录对 B 侧：设备探测为真、安装记录探测为假 —— 两族各按各的锚点，互不串；
//  3. 反证：不带 scope 时该记录照常可见，「只收紧不放宽」的红线没被自己判红。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t403TeamOwn   = "TEAM-DEV-IT-T403A"
	t403TeamOther = "TEAM-DEV-IT-T403B"
	t403PatOwn    = "P-DEV-IT-T403-OWN" // 安装记录的历史患者，属 A
	t403PatOther  = "P-DEV-IT-T403-OTH" // 设备转绑后的现绑患者，属 B
	t403Device    = "PRS-DEV-IT-T403-DEV"
	t403Tech      = "TECH-DEV-IT-T403"
	t403Kw        = "PRS-DEV-IT-T403" // 只命中本用例的设备/记录
)

// seedT403Rebind 造转绑分歧：安装记录留在 A 团队患者名下，设备现绑改到 B 团队患者。
// 只写 devices.patient_id 这一列（Bind/Rebind 的读侧效果就是这一列，见 repo.go 的
// UPDATE devices SET patient_id = $2），不建 device_bindings 行，避开 T299 的一患者一活跃绑定制。
func seedT403Rebind(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO teams (team_id, name, member_count, patient_count) VALUES
		   ($1, 'T403A团队', 1, 1), ($2, 'T403B团队', 1, 1)
		 ON CONFLICT (team_id) DO NOTHING`, []any{t403TeamOwn, t403TeamOther}},
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, team_id, status) VALUES
		   ($1, 'T403历史患者', '\x00'::bytea, $2, $3, 'active'),
		   ($4, 'T403现绑患者', '\x00'::bytea, $5, $6, 'active')
		 ON CONFLICT (patient_id) DO NOTHING`,
			[]any{t403PatOwn, "t403a" + strings.Repeat("0", 59), t403TeamOwn,
				t403PatOther, "t403b" + strings.Repeat("0", 59), t403TeamOther}},
		{`INSERT INTO technicians (tech_id, name, phone_enc, phone_hash) VALUES
		   ($1, 'T403技师', '\x00'::bytea, $2)
		 ON CONFLICT (tech_id) DO NOTHING`, []any{t403Tech, "t403c" + strings.Repeat("0", 59)}},
		// 设备现绑先落在 A 患者上，安装记录随后写 A 患者 —— 即「安装当时」两锚点同值
		{`INSERT INTO devices (device_id, device_secret_enc, patient_id, status) VALUES
		   ($1, '\x00'::bytea, $2, 'online')
		 ON CONFLICT (device_id) DO NOTHING`, []any{t403Device, t403PatOwn}},
	}
	for _, s := range stmts {
		_, err := itPool.Exec(ctx, s.sql, s.args...)
		require.NoError(t, err)
	}

	var installID int64
	err := itPool.QueryRow(ctx,
		`SELECT install_id FROM install_records WHERE device_id = $1`, t403Device).Scan(&installID)
	if errors.Is(err, pgx.ErrNoRows) {
		require.NoError(t, itPool.QueryRow(ctx,
			`INSERT INTO install_records (device_id, patient_id, tech_id, calibrate_time)
			 VALUES ($1, $2, $3, now()) RETURNING install_id`,
			t403Device, t403PatOwn, t403Tech).Scan(&installID))
	} else {
		require.NoError(t, err)
	}

	// 转绑：设备改挂 B 团队患者，安装记录留在 A —— 分歧现场
	_, err = itPool.Exec(ctx, `UPDATE devices SET patient_id = $2 WHERE device_id = $1`,
		t403Device, t403PatOther)
	require.NoError(t, err)

	// 现场确认：两条锚点确实分叉了（没分叉就是种子坏了，后面的断言全是空跑）
	var devPatient, recPatient string
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT patient_id FROM devices WHERE device_id = $1`, t403Device).Scan(&devPatient))
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT patient_id FROM install_records WHERE install_id = $1`, installID).Scan(&recPatient))
	require.Equal(t, t403PatOther, devPatient, "种子失败：设备未转绑成功")
	require.Equal(t, t403PatOwn, recPatient, "种子失败：安装记录患者被改动（写侧本不该回填它）")

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = itPool.Exec(cctx, `DELETE FROM install_records WHERE device_id = $1`, t403Device)
		_, _ = itPool.Exec(cctx, `DELETE FROM devices WHERE device_id = $1`, t403Device)
		_, _ = itPool.Exec(cctx, `DELETE FROM technicians WHERE tech_id = $1`, t403Tech)
		_, _ = itPool.Exec(cctx, `DELETE FROM patients WHERE patient_id IN ($1, $2)`, t403PatOwn, t403PatOther)
		_, _ = itPool.Exec(cctx, `DELETE FROM teams WHERE team_id IN ($1, $2)`, t403TeamOwn, t403TeamOther)
	})
	return installID
}

// TestITT403_InstallAnchorAfterRebind 转绑后安装记录的读侧仍按「记录患者」定团队，
// 且列表与详情同锚 —— 若有人把 InstallInTeam 改成跟设备现绑患者，第 1 步会翻假；
// 若有人把列表谓词改成走设备，第 1 步的「列表可见」与「详情可读」会不一致。
func TestITT403_InstallAnchorAfterRebind(t *testing.T) {
	installID := seedT403Rebind(t)
	store := newITStore()
	ctx := context.Background()

	// 1. A 侧：列表可见 且 详情探测放行（同一条锚点决定两件事）
	visible, total, err := store.ListInstallRecords(ctx, t403Kw,
		ListScope{TeamScoped: true, TeamID: t403TeamOwn}, 1, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), total, "记录患者属 A，A 的列表必须看得见这条转绑前的安装记录")
	require.Len(t, visible, 1)
	assert.Equal(t, installID, visible[0].InstallID)

	listVisible := false
	for _, r := range visible {
		if r.InstallID == installID {
			listVisible = true
		}
	}
	okA, err := store.InstallInTeam(ctx, installID, t403TeamOwn)
	require.NoError(t, err)
	assert.True(t, okA, "详情探测与列表同锚：A 可见即 A 可读，否则就是卡面的 403 矛盾")
	assert.Equal(t, listVisible, okA, "「列表可见 ⇒ 详情可读」是同一条谓词的两次应用，不许分开成立")

	// 2. B 侧：设备已转绑进来，设备可见；同一条安装记录对 B 不可见（记录不属 B）
	devInB, err := store.DeviceInTeam(ctx, t403Device, t403TeamOther)
	require.NoError(t, err)
	assert.True(t, devInB, "设备现绑患者属 B，设备族按现绑锚点放行（本卡不动这一族）")

	okB, err := store.InstallInTeam(ctx, installID, t403TeamOther)
	require.NoError(t, err)
	assert.False(t, okB, "安装记录留在 A：不得因设备转绑进 B 就跟着变成 B 的数据")

	hidden, hiddenTotal, err := store.ListInstallRecords(ctx, t403Kw,
		ListScope{TeamScoped: true, TeamID: t403TeamOther}, 1, 100)
	require.NoError(t, err)
	assert.Zero(t, hiddenTotal, "B 的列表数不出 A 的记录，且总数与行数同口径")
	assert.Len(t, hidden, 0)

	// 3. 跨资源非对称要显式钉住：A 见记录不见设备、B 见设备不见记录。
	//    这一格是 Boss 口径「安装记录按医护本团队可读」容忍的现状；哪天要改成同锚（卡面乙案），
	//    必须连带把设备族一起动，本用例会先判红，不允许只改一侧留下半放宽的缝。
	devA, devATotal, err := store.ListDevices(ctx, t403Kw,
		ListScope{TeamScoped: true, TeamID: t403TeamOwn}, 1, 100)
	require.NoError(t, err)
	assert.Zero(t, devATotal, "设备已转绑出 A，A 的设备列表不该再数到它")
	assert.Len(t, devA, 0)

	devB, devBTotal, err := store.ListDevices(ctx, t403Kw,
		ListScope{TeamScoped: true, TeamID: t403TeamOther}, 1, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), devBTotal)
	require.Len(t, devB, 1)
	assert.Equal(t, t403Device, devB[0].DeviceID)
	require.NotNil(t, devB[0].PatientID, "转绑后设备必有现绑患者，为空说明种子没写进去")
	assert.Equal(t, t403PatOther, *devB[0].PatientID, "设备列表回显的是现绑患者")

	// 4. 反证：不受限身份（无 scope）照常见全量，收紧只作用于医护
	all, allTotal, err := store.ListInstallRecords(ctx, t403Kw, ListScope{}, 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), allTotal)
	require.Len(t, all, 1)
	assert.Equal(t, t403PatOwn, all[0].PatientID, "列表回显的患者必须是记录患者，不是设备现绑患者")
}
