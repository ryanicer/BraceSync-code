//go:build integration
// +build integration

// T403 乙案（Boss 拍「按乙案修」）：设备域读侧「列表可见 = 详情可读」口径归一（真库）。
//
// 卡面症状是「医护能在列表里看到本团队安装记录、点详情却 403」。取证（评论 1816）分两层：
//   - 族内从不出问题：安装记录列表与 InstallInTeam 用的是同一条 join 链；
//   - 分歧在跨资源 —— install_records.patient_id 是安装当时的历史患者（写侧从不回填），
//     devices.patient_id 是现绑患者（Bind/Rebind/Unbind 会改）。设备转绑后两条锚点分叉，
//     于是「本团队安装记录列得出来、点它引用的那台设备 403」（T386 观察项二）。
//
// 甲案只补了契约与门禁，把这个 403 锁成设计现状；乙案改口径：设备族的归属谓词扩成两段 union
// （现绑患者属本团队 OR 该设备被本团队患者的安装记录引用过），且设备列表与设备探测共用同一段
// 文本常量（repo.deviceTeamCondFmt）。
//
// 本文件锁四格，缺一格就是半放宽：
//  1. 转绑现场：记录患者属 A、设备现绑属 B —— A 的设备列表必须看得见这台（甲案口径下这一格是
//     空集，即卡面 403 的现场），且 A 的详情探测同为真；
//  2. 双向不变量：对每一团队，设备列表的命中集与逐台 DeviceInTeam 的结果必须逐台相等。
//     「只放宽列表」与「只放宽探测」都是半放宽，都会在这一格判红。两个方向的变异实测读数
//     记在交付单里（本地真库探针），这里不写死数字；
//  3. 安装记录族不跟随放宽：B 侧列表数不到 A 的那条记录、探测同为假（反向放宽会把别团队的
//     安装备注与签名图链接拉进本团队列表）；
//  4. 负格与反证：现绑他团队且从无本团队安装记录的设备、无主且无安装记录的设备、无团队归属
//     身份、不存在的 deviceId 一律为假；不带 scope 时设备与记录照常全见（不受限角色响应逐字不变）。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t403TeamOwn    = "TEAM-DEV-IT-T403A"
	t403TeamOther  = "TEAM-DEV-IT-T403B"
	t403PatOwn     = "P-DEV-IT-T403-OWN"    // 安装记录的历史患者，属 A
	t403PatOther   = "P-DEV-IT-T403-OTH"    // 分歧设备转绑后的现绑患者，属 B
	t403PatA2      = "P-DEV-IT-T403-A2"     // 属 A 的另一名患者
	t403PatB2      = "P-DEV-IT-T403-B2"     // 属 B 的另一名患者
	t403Device     = "PRS-DEV-IT-T403-DEV"  // 记录患者 A、现绑患者 B（分歧现场）
	t403DeviceA    = "PRS-DEV-IT-T403-A"    // 现绑 A 患者、无安装记录 → union 前半支
	t403DeviceB2   = "PRS-DEV-IT-T403-B2"   // 现绑 B 患者、无安装记录 → A 侧必负
	t403DeviceFree = "PRS-DEV-IT-T403-FREE" // 无主（patient_id NULL）、无安装记录 → 两侧必负
	t403Tech       = "TECH-DEV-IT-T403"
	t403Kw         = "PRS-DEV-IT-T403" // 只命中本用例的设备/记录
)

// t403Devices 本用例种下的全部设备，按「列表命中集 == 探测结果集」逐台对读的遍历全集。
var t403Devices = []string{t403DeviceA, t403Device, t403DeviceB2, t403DeviceFree}

// seedT403Rebind 造转绑分歧：安装记录留在 A 团队患者名下，设备现绑改到 B 团队患者。
// 另附三台设备覆盖 union 的两半支与 fail-closed 负格（uk_devices_active_patient 限定一名
// 患者至多一台设备，故每台现绑设备各需一名自己的患者）。
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
		   ($4, 'T403现绑患者', '\x00'::bytea, $5, $6, 'active'),
		   ($7, 'T403甲团队患者', '\x00'::bytea, $8, $9, 'active'),
		   ($10, 'T403乙团队患者', '\x00'::bytea, $11, $12, 'active')
		 ON CONFLICT (patient_id) DO NOTHING`,
			[]any{t403PatOwn, "t403a" + strings.Repeat("0", 59), t403TeamOwn,
				t403PatOther, "t403b" + strings.Repeat("0", 59), t403TeamOther,
				t403PatA2, "t403d" + strings.Repeat("0", 59), t403TeamOwn,
				t403PatB2, "t403e" + strings.Repeat("0", 59), t403TeamOther}},
		{`INSERT INTO technicians (tech_id, name, phone_enc, phone_hash) VALUES
		   ($1, 'T403技师', '\x00'::bytea, $2)
		 ON CONFLICT (tech_id) DO NOTHING`, []any{t403Tech, "t403c" + strings.Repeat("0", 59)}},
		// 分歧设备现绑先落在 A 患者上，安装记录随后写 A 患者 —— 即「安装当时」两锚点同值
		{`INSERT INTO devices (device_id, device_secret_enc, patient_id, status) VALUES
		   ($1, '\x00'::bytea, $2, 'online'),
		   ($3, '\x00'::bytea, $4, 'online'),
		   ($5, '\x00'::bytea, $6, 'online'),
		   ($7, '\x00'::bytea, NULL, 'unbound')
		 ON CONFLICT (device_id) DO NOTHING`,
			[]any{t403Device, t403PatOwn, t403DeviceA, t403PatA2,
				t403DeviceB2, t403PatB2, t403DeviceFree}},
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
		_, _ = itPool.Exec(cctx, `DELETE FROM install_records WHERE device_id LIKE 'PRS-DEV-IT-T403%'`)
		_, _ = itPool.Exec(cctx, `DELETE FROM devices WHERE device_id LIKE 'PRS-DEV-IT-T403%'`)
		_, _ = itPool.Exec(cctx, `DELETE FROM technicians WHERE tech_id = $1`, t403Tech)
		_, _ = itPool.Exec(cctx, `DELETE FROM patients WHERE patient_id LIKE 'P-DEV-IT-T403%'`)
		_, _ = itPool.Exec(cctx, `DELETE FROM teams WHERE team_id IN ($1, $2)`, t403TeamOwn, t403TeamOther)
	})
	return installID
}

// t403DevIDs 列表命中集（排序后与探测命中集可比）。
func t403DevIDs(rows []DeviceListItem) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.DeviceID)
	}
	sort.Strings(out)
	return out
}

func t403DeviceRow(rows []DeviceListItem, deviceID string) (DeviceListItem, bool) {
	for _, r := range rows {
		if r.DeviceID == deviceID {
			return r, true
		}
	}
	return DeviceListItem{}, false
}

// t403Readable 逐台问探测，返回该团队可读的设备集。
// 顺带锁一条：不可见必须是「无行」而不是报错 —— 报错会让医护吃 500 而不是 403。
func t403Readable(t *testing.T, store *PGStore, teamID string) []string {
	t.Helper()
	got := make([]string, 0, len(t403Devices))
	for _, id := range t403Devices {
		ok, err := store.DeviceInTeam(context.Background(), id, teamID)
		require.NoError(t, err)
		if ok {
			got = append(got, id)
		}
	}
	sort.Strings(got)
	return got
}

// TestITT403_InstallAnchorAfterRebind 转绑现场的两族口径：安装记录仍按「记录患者」定团队
// （甲案锁的那半不变），设备侧已跟随放宽到历史锚点（乙案加的那半）。
//
// 若有人把 InstallInTeam 改成跟设备现绑患者，第 1 步与第 3 步会一起翻；
// 若有人把设备族改回单锚（现绑），第 2 步 —— 也就是卡面那条 403 —— 会重新出现。
func TestITT403_InstallAnchorAfterRebind(t *testing.T) {
	installID := seedT403Rebind(t)
	store := newITStore()
	ctx := context.Background()

	// 1. A 侧安装记录：列表可见 且 详情探测放行（同一条锚点决定两件事）
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
	assert.Equal(t, listVisible, okA, "「列表可见 ⇔ 详情可读」是同一条谓词的两次应用，不许分开成立")

	// 2. A 侧设备（乙案新增的那半）：现绑已转去 B，但本团队患者被安装过 ⇒ 必须可见且可读。
	devA, devATotal, err := store.ListDevices(ctx, t403Kw,
		ListScope{TeamScoped: true, TeamID: t403TeamOwn}, 1, 100)
	require.NoError(t, err)
	require.Equal(t, int64(2), devATotal, "A 应看到现绑本团队的设备 + 被本团队安装记录引用过的转绑设备")
	require.Len(t, devA, 2)
	row, found := t403DeviceRow(devA, t403Device)
	require.True(t, found, "union 后半支（历史锚点）没生效：转绑出本团队的设备又从医护列表里消失了")
	require.NotNil(t, row.PatientID, "设备列表回显现绑患者，为空说明种子没写进去")
	assert.Equal(t, t403PatOther, *row.PatientID,
		"这一格靠的是历史锚点而非现绑 —— 现绑患者必须仍是他团队那一名")

	inTeamA, err := store.DeviceInTeam(ctx, t403Device, t403TeamOwn)
	require.NoError(t, err)
	assert.True(t, inTeamA, "列表有这台却探测 false ⇒ 点详情仍 403，乙案没修完")

	// 3. B 侧安装记录：记录留在 A，不因设备转绑进 B 就变成 B 的数据
	okB, err := store.InstallInTeam(ctx, installID, t403TeamOther)
	require.NoError(t, err)
	assert.False(t, okB, "安装记录族不跟随放宽：反向会把别团队的安装备注/签名图链接拉进本团队列表")

	hidden, hiddenTotal, err := store.ListInstallRecords(ctx, t403Kw,
		ListScope{TeamScoped: true, TeamID: t403TeamOther}, 1, 100)
	require.NoError(t, err)
	assert.Zero(t, hiddenTotal, "B 的列表数不出 A 的记录，且总数与行数同口径")
	assert.Len(t, hidden, 0)

	// 4. 跨资源非对称（乙案后只剩这一向）：B 见设备不见记录，A 两样都见
	devB, devBTotal, err := store.ListDevices(ctx, t403Kw,
		ListScope{TeamScoped: true, TeamID: t403TeamOther}, 1, 100)
	require.NoError(t, err)
	require.Equal(t, int64(2), devBTotal, "B 看到两台现绑本团队患者的设备，现绑在 A 的那台不跟过来")
	require.Len(t, devB, 2)
	_, aInB := t403DeviceRow(devB, t403DeviceA)
	assert.False(t, aInB, "反向守卫：现绑本团队之外、又无本团队安装记录的设备不得被 union 带进来")

	// 5. 反证：不受限身份（无 scope）照常见全量，收紧只作用于医护
	all, allTotal, err := store.ListInstallRecords(ctx, t403Kw, ListScope{}, 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), allTotal)
	require.Len(t, all, 1)
	assert.Equal(t, t403PatOwn, all[0].PatientID, "列表回显的患者必须是记录患者，不是设备现绑患者")

	allDev, allDevTotal, err := store.ListDevices(ctx, t403Kw, ListScope{}, 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(4), allDevTotal, "无 scope 时四台设备全见（不受限角色响应逐字不变）")
	assert.Len(t, allDev, 4)
}

// TestITT403_DeviceListProbeInvariant 「列表可见 ⇔ 详情可读」的双向不变量（乙案的门禁本体）。
//
// 设备列表谓词与 DeviceInTeam 现在共用同一段常量，这一格正常情况下不可能不等；它的价值在于挡住
// 「改一侧忘改另一侧」。两个方向都做过变异实测（探测侧退回旧单谓词 / 列表侧退回旧单谓词），
// 各让对应团队的「列表命中集」与「探测命中集」不相等，一手读数在交付单里。
func TestITT403_DeviceListProbeInvariant(t *testing.T) {
	seedT403Rebind(t)
	store := newITStore()
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		teamID string
		want   []string // 期望命中集：列表与探测都必须等于它
	}{
		{"本团队（现绑一台 + 历史锚点一台）", t403TeamOwn, []string{t403DeviceA, t403Device}},
		{"他团队（两台现绑，含被 A 安装过的那台）", t403TeamOther, []string{t403Device, t403DeviceB2}},
		{"无团队归属的医护（恒假，不得退化成不过滤）", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, total, err := store.ListDevices(ctx, t403Kw,
				ListScope{TeamScoped: true, TeamID: tc.teamID}, 1, 100)
			require.NoError(t, err)
			assert.Equal(t, int64(len(tc.want)), total, "total 与 list 同口径（页脚不得数出他团队设备）")
			gotList := t403DevIDs(rows)
			readable := t403Readable(t, store, tc.teamID) // teamID 为空时探测不下库

			want := append([]string{}, tc.want...)
			sort.Strings(want)
			assert.Equal(t, want, gotList, "列表命中集")
			assert.Equal(t, want, readable, "逐台探测命中集")
			assert.Equal(t, gotList, readable, "列表可见集必须逐台等于详情可读集 —— 不等就是半放宽")
		})
	}

	// 不存在的 deviceId 与「存在但不可见」同判 false（否则存在性可被当作探测 oracle）
	missing, err := store.DeviceInTeam(ctx, "PRS-DEV-IT-T403-NOPE", t403TeamOwn)
	require.NoError(t, err)
	assert.False(t, missing)

	// teamID 空串时连本团队那台也不放行（探测侧短路）
	noTeam, err := store.DeviceInTeam(ctx, t403DeviceA, "")
	require.NoError(t, err)
	assert.False(t, noTeam)
}
