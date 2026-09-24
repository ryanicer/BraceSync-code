//go:build integration
// +build integration

// T378：设备域读侧归属谓词在真库里的语义（ListDevices / ListInstallRecords 带 ListScope，
// 以及 DeviceInTeam / InstallInTeam 两个单资源只读探测）。
//
// 单测（team_scope_t378_test.go）只证明 handler 把 scope 原样传下去、越权时不发查询；
// 证不了三件事，必须落真库：
//  1. 未绑定设备（devices.patient_id 为 NULL）在团队谓词下确实被排除 —— LEFT JOIN 出来的
//     p.team_id 为 NULL，等值不成立。这条是 fail-closed 的根基，写错就是把无主设备泄露给医护；
//  2. TeamScoped && TeamID 为空落的是恒假谓词而非「不过滤」，且 total 与 list 同口径
//     （分页页脚和表格行数不一致是 T371 那族口径缺陷的形状，这里一并守住）；
//  3. keyword 与团队谓词共存时占位符不串位 —— 两个参数同时进 SQL，$1/$2 顺序由代码推导，
//     错一位就是运行时报错或把团队号当关键词搜。
//
// 反证不可省：不带 scope 时必须仍见全量（含无主设备），否则「只收紧不放宽」的红线被自己判红。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t378ITTeamOwn    = "TEAM-DEV-IT-T378A"
	t378ITTeamOther  = "TEAM-DEV-IT-T378B"
	t378ITPatient    = "P-DEV-IT-T378-OWN"
	t378ITDeviceOwn  = "PRS-DEV-IT-T378-OWN"
	t378ITDeviceFree = "PRS-DEV-IT-T378-FREE" // 无主设备（patient_id 为 NULL）
)

// seedT378Devices 一名本团队患者 + 一名他团队患者 + 一台无主设备 + 各自一条安装记录。
// 独立 ID，测后清场（口径同 query_integration_test.go 的 seedQueryData）。
func seedT378Devices(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO teams (team_id, name, member_count, patient_count) VALUES
		   ($1, 'T378本团队', 1, 1), ($2, 'T378他团队', 1, 1)
		 ON CONFLICT (team_id) DO NOTHING`, []any{t378ITTeamOwn, t378ITTeamOther}},
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, team_id, status) VALUES
		   ($1, 'T378本团队患者', '\x00'::bytea, $2, $3, 'active'),
		   ($4, 'T378他团队患者', '\x00'::bytea, $5, $6, 'active')
		 ON CONFLICT (patient_id) DO NOTHING`,
			[]any{t378ITPatient, "t378a" + strings.Repeat("0", 59), t378ITTeamOwn,
				"P-DEV-IT-T378-OTH", "t378b" + strings.Repeat("0", 59), t378ITTeamOther}},
		{`INSERT INTO devices (device_id, device_secret_enc, patient_id, status) VALUES
		   ($1, '\x00'::bytea, $2, 'online'),
		   ($3, '\x00'::bytea, NULL, 'unbound'),
		   ($4, '\x00'::bytea, $5, 'online')
		 ON CONFLICT (device_id) DO NOTHING`,
			[]any{t378ITDeviceOwn, t378ITPatient, t378ITDeviceFree, "PRS-DEV-IT-T378-OTH", "P-DEV-IT-T378-OTH"}},
		{`INSERT INTO technicians (tech_id, name, phone_enc, phone_hash) VALUES
		   ($1, 'T378技师', '\x00'::bytea, $2)
		 ON CONFLICT (tech_id) DO NOTHING`, []any{"TECH-DEV-IT-T378", "t378c" + strings.Repeat("0", 59)}},
	}
	for _, s := range stmts {
		_, err := itPool.Exec(ctx, s.sql, s.args...)
		require.NoError(t, err)
	}
	// 三条设备各一条安装记录（他团队那条用于反证「列表按团队收窄」）
	for _, d := range []struct{ deviceID, patientID string }{
		{t378ITDeviceOwn, t378ITPatient},
		{"PRS-DEV-IT-T378-OTH", "P-DEV-IT-T378-OTH"},
	} {
		var n int64
		require.NoError(t, itPool.QueryRow(ctx,
			`SELECT COUNT(*) FROM install_records WHERE device_id = $1`, d.deviceID).Scan(&n))
		if n == 0 {
			_, err := itPool.Exec(ctx,
				`INSERT INTO install_records (device_id, patient_id, tech_id, calibrate_time)
				 VALUES ($1, $2, $3, now())`, d.deviceID, d.patientID, "TECH-DEV-IT-T378")
			require.NoError(t, err)
		}
	}
	t.Cleanup(func() {
		_, _ = itPool.Exec(ctx, `DELETE FROM install_records WHERE device_id LIKE 'PRS-DEV-IT-T378%'`)
		_, _ = itPool.Exec(ctx, `DELETE FROM devices WHERE device_id LIKE 'PRS-DEV-IT-T378%'`)
		_, _ = itPool.Exec(ctx, `DELETE FROM patients WHERE patient_id LIKE 'P-DEV-IT-T378%'`)
		_, _ = itPool.Exec(ctx, `DELETE FROM technicians WHERE tech_id = 'TECH-DEV-IT-T378'`)
		_, _ = itPool.Exec(ctx, `DELETE FROM teams WHERE team_id IN ($1, $2)`, t378ITTeamOwn, t378ITTeamOther)
	})
}

func devIDs(rows []DeviceListItem) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.DeviceID)
	}
	return out
}

func installIDs(rows []InstallListItem) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.InstallID)
	}
	return out
}

func TestITT378ListDevicesTeamScope(t *testing.T) {
	seedT378Devices(t)
	store := newITStore()
	ctx := context.Background()

	// 反证：不带 scope 见全量（含无主设备与他团队设备）
	all, allTotal, err := store.ListDevices(ctx, "PRS-DEV-IT-T378", ListScope{}, 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(3), allTotal, "种子应有三台 T378 专用设备")
	assert.Len(t, all, 3)

	// 本团队：只剩绑定本团队患者那台
	own, ownTotal, err := store.ListDevices(ctx, "PRS-DEV-IT-T378",
		ListScope{TeamScoped: true, TeamID: t378ITTeamOwn}, 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), ownTotal, "total 与 list 必须同口径（分页页脚不得数出他团队设备）")
	assert.Equal(t, []string{t378ITDeviceOwn}, devIDs(own))

	// 无团队归属：空集 + total 0，不得回落全量
	none, noneTotal, err := store.ListDevices(ctx, "PRS-DEV-IT-T378", ListScope{TeamScoped: true}, 1, 100)
	require.NoError(t, err)
	assert.Zero(t, noneTotal)
	assert.Len(t, none, 0)

	// keyword + scope 共存：命中本团队那条，且不把团队号当关键词
	kw, kwTotal, err := store.ListDevices(ctx, "T378本团队患者",
		ListScope{TeamScoped: true, TeamID: t378ITTeamOwn}, 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), kwTotal)
	assert.Equal(t, []string{t378ITDeviceOwn}, devIDs(kw))

	// keyword 命中的是他团队那条 → 团队谓词把它滤掉，空集
	kwMiss, kwMissTotal, err := store.ListDevices(ctx, "T378他团队患者",
		ListScope{TeamScoped: true, TeamID: t378ITTeamOwn}, 1, 100)
	require.NoError(t, err)
	assert.Zero(t, kwMissTotal)
	assert.Len(t, kwMiss, 0)
}

func TestITT378ListInstallRecordsTeamScope(t *testing.T) {
	seedT378Devices(t)
	store := newITStore()
	ctx := context.Background()

	all, allTotal, err := store.ListInstallRecords(ctx, "PRS-DEV-IT-T378", ListScope{}, 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(2), allTotal, "两台有主设备各一条安装记录")
	assert.Len(t, all, 2)

	own, ownTotal, err := store.ListInstallRecords(ctx, "PRS-DEV-IT-T378",
		ListScope{TeamScoped: true, TeamID: t378ITTeamOwn}, 1, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), ownTotal)
	require.Len(t, own, 1)
	assert.Equal(t, t378ITPatient, own[0].PatientID)
	assert.NotEmpty(t, installIDs(own))

	none, noneTotal, err := store.ListInstallRecords(ctx, "PRS-DEV-IT-T378", ListScope{TeamScoped: true}, 1, 100)
	require.NoError(t, err)
	assert.Zero(t, noneTotal)
	assert.Len(t, none, 0)
}

func TestITT378DeviceAndInstallProbes(t *testing.T) {
	seedT378Devices(t)
	store := newITStore()
	ctx := context.Background()

	var installOwn, installOther, maxID int64
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT i.install_id FROM install_records i JOIN devices d ON d.device_id = i.device_id
		 WHERE d.device_id = $1`, t378ITDeviceOwn).Scan(&installOwn))
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT i.install_id FROM install_records i JOIN devices d ON d.device_id = i.device_id
		 WHERE d.device_id = 'PRS-DEV-IT-T378-OTH'`).Scan(&installOther))
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT COALESCE(MAX(install_id), 0) FROM install_records`).Scan(&maxID))

	// 1. 设备探测：本团队放行（反证），四格合一律 false
	own, err := store.DeviceInTeam(ctx, t378ITDeviceOwn, t378ITTeamOwn)
	require.NoError(t, err)
	assert.True(t, own, "本团队设备必须放行，否则收口过头")

	for _, tc := range []struct {
		name     string
		deviceID string
		teamID   string
	}{
		{"他团队患者名下的设备", "PRS-DEV-IT-T378-OTH", t378ITTeamOwn},
		{"未绑定患者的设备", t378ITDeviceFree, t378ITTeamOwn},
		{"设备不存在（存在性探测）", "PRS-DEV-IT-T378-NOPE", t378ITTeamOwn},
		{"调用者无团队归属（teamID 空串）", t378ITDeviceOwn, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, probeErr := store.DeviceInTeam(ctx, tc.deviceID, tc.teamID)
			require.NoError(t, probeErr, "不可见必须是「无行」而不是报错")
			assert.False(t, got)
		})
	}

	// 2. 安装记录探测：同四格
	ownIns, err := store.InstallInTeam(ctx, installOwn, t378ITTeamOwn)
	require.NoError(t, err)
	assert.True(t, ownIns)

	for _, tc := range []struct {
		name      string
		installID int64
		teamID    string
	}{
		{"他团队患者名下的记录", installOther, t378ITTeamOwn},
		{"记录不存在（存在性探测）", maxID + 1000, t378ITTeamOwn},
		{"调用者无团队归属（teamID 空串）", installOwn, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, probeErr := store.InstallInTeam(ctx, tc.installID, tc.teamID)
			require.NoError(t, probeErr)
			assert.False(t, got)
		})
	}
}

func TestITT378DoctorTeamByAdmin(t *testing.T) {
	ctx := context.Background()
	const adminID = "ADM-DEV-IT-T378"
	const doctorID = "DOC-DEV-IT-T378"
	seedT378Devices(t)
	_, err := itPool.Exec(ctx, `INSERT INTO doctors (doctor_id, name, title, department, team_id, admin_id)
		VALUES ($1, 'T378集成医生', '主治医师', '骨科', $2, $3)
		ON CONFLICT (doctor_id) DO NOTHING`, doctorID, t378ITTeamOwn, adminID)
	require.NoError(t, err)
	_, err = itPool.Exec(ctx, `INSERT INTO doctors (doctor_id, name, title, department, team_id, admin_id)
		VALUES ($1, 'T378无团队医生', '主治医师', '骨科', NULL, $2)
		ON CONFLICT (doctor_id) DO NOTHING`, "DOC-DEV-IT-T378-NOT", "ADM-DEV-IT-T378-NOT")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = itPool.Exec(ctx, `DELETE FROM doctors WHERE doctor_id IN ($1, $2)`, doctorID, "DOC-DEV-IT-T378-NOT")
	})

	store := newITStore()

	teamID, ok, err := store.DoctorTeamByAdmin(ctx, adminID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, t378ITTeamOwn, teamID)

	// team_id 为 NULL → 空串 + ok=false（调用方据此落空集，不得当成「不过滤」）
	empty, ok2, err := store.DoctorTeamByAdmin(ctx, "ADM-DEV-IT-T378-NOT")
	require.NoError(t, err)
	assert.False(t, ok2)
	assert.Equal(t, "", empty)

	// 无 doctor 行 → 同样是「无归属」而非报错（报错会变 500，医护被踢成另一种码）
	missing, ok3, err := store.DoctorTeamByAdmin(ctx, "ADM-DEV-IT-T378-ABSENT")
	require.NoError(t, err)
	assert.False(t, ok3)
	assert.Equal(t, "", missing)
}
