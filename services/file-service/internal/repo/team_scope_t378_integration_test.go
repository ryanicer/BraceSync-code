//go:build integration
// +build integration

// T378：文件域归属判定在真库（PG15）里的语义。
//
// 单测（handler/team_scope_t378_test.go）用内存桩，证的是 handler 的调用姿势
// （判定排在读写之前、拒绝路径零写、不受限角色响应不变）。桩里的 owner→团队映射
// 是我自己写的 map，等于「按我以为的 SQL 语义」断言。以下四件事只有真 PG 能证：
//
//  1. teamScopedCond 的 EXISTS 子查询写法本身 —— owner_id 是 VARCHAR(64)、
//     patients.patient_id 是 VARCHAR(32)、alerts.alert_id 是 BIGINT，
//     告警那一跳靠 alert_id::text 对齐；少一次转换 PG 直接报类型错；
//  2. 「患者行不存在」与「患者 team_id 为 NULL」在 EXISTS 语义下都为假 ——
//     即 fail-closed 不是我以为的那个 fail-closed；
//  3. 列表（QueryFiles）与计数（CountFiles）共用 buildFilterSQL，
//     团队参数与 owner_type/file_type/status 共存时占位符不串位；
//  4. FileOwnerInTeam（详情/下载/上传完成的探测）与列表口径不分叉 ——
//     「列表有行、详情 403」是这类改造最常见的返工点。
//
// 反证不可省：不收窄时必须见到全部种子文件（只收紧不放宽）。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
)

const (
	t378Role   = "ROLE-FILE-IT-T378"
	t378AdmA   = "ADM-FILE-IT-T378-A"
	t378AdmNot = "ADM-FILE-IT-T378-NOT"
	t378DocA   = "DOC-FILE-IT-T378-A"
	t378DocNot = "DOC-FILE-IT-T378-NOT"

	t378TeamOwn   = "TEAM-FILE-IT-T378-A"
	t378TeamOther = "TEAM-FILE-IT-T378-B"

	t378PatOwn    = "P-FILE-IT-T378-OWN"   // 本团队患者
	t378PatOther  = "P-FILE-IT-T378-OTH"   // 他团队患者
	t378PatNoTeam = "P-FILE-IT-T378-NT"    // 有患者行但 team_id 为 NULL
	t378PatGhost  = "P-FILE-IT-T378-GHOST" // 患者行不存在（改档/漏删后的悬挂 owner）
	t378DevOwn    = "D-FILE-IT-T378-A"
	t378DevOther  = "D-FILE-IT-T378-B"
	t378Tech      = "TECH-FILE-IT-T378"

	// 文件 ID 统一前缀：同包其他用例也往 files 表插行（owner_type=install_record /
	// patient），集合断言一律按本前缀过滤，避免与别人的种子互相污染。
	t378FilePrefix = "F-FILE-IT-T378-"

	t378FileOwn     = t378FilePrefix + "OWN"
	t378FileOther   = t378FilePrefix + "OTH"
	t378FileNoTeam  = t378FilePrefix + "NT"
	t378FileGhost   = t378FilePrefix + "GHOST"
	t378FileAlertOK = t378FilePrefix + "ALERT-OWN"
	t378FileAlertNo = t378FilePrefix + "ALERT-OTH"
	t378FileTpl     = t378FilePrefix + "TPL"
)

// t378PhoneHash 64 字符占位哈希（phone_hash CHAR(64) NOT NULL，集成库里不是真号）。
func t378PhoneHash(tag string) string {
	return tag + strings.Repeat("0", 64-len(tag))
}

// t378Seed 建两团队对照现场 + 四类「归属不成立」的文件，
// 返回本团队/他团队告警的 alert_id（BIGINT IDENTITY，只能读回，不能自己编）。
func t378Seed(t *testing.T) (alertOwn, alertOther int64) {
	t.Helper()
	ctx := context.Background()
	stmts := []struct {
		sql  string
		args []any
	}{
		// doctors.admin_id / patients.team_id 都有外键：先建身份与团队
		{`INSERT INTO roles (role_id, name, permissions_json) VALUES ($1, 'T378 文件集成角色', '{}')
		   ON CONFLICT (role_id) DO NOTHING`, []any{t378Role}},
		{`INSERT INTO admins (admin_id, username, name, password_hash, role_id)
		   VALUES ($1, 'file_it_t378_a', 'T378本团队医护', 'x', $3),
		          ($2, 'file_it_t378_n', 'T378无团队医护', 'x', $3)
		   ON CONFLICT (admin_id) DO NOTHING`, []any{t378AdmA, t378AdmNot, t378Role}},
		{`INSERT INTO teams (team_id, name) VALUES ($1, 'T378 文件 A 科'), ($2, 'T378 文件 B 科')
		   ON CONFLICT (team_id) DO NOTHING`, []any{t378TeamOwn, t378TeamOther}},
		{`INSERT INTO doctors (doctor_id, name, team_id, admin_id)
		   VALUES ($1, 'T378 文件 A 科医护', $3, $4),
		          ($2, 'T378 文件无团队医护', NULL, $5)
		   ON CONFLICT (doctor_id) DO NOTHING`,
			[]any{t378DocA, t378DocNot, t378TeamOwn, t378AdmA, t378AdmNot}},
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, team_id, status) VALUES
		   ($1, 'T378 本团队患者', '\x00'::bytea, $2, $3, 'active'),
		   ($4, 'T378 他团队患者', '\x00'::bytea, $5, $6, 'active'),
		   ($7, 'T378 无团队患者', '\x00'::bytea, $8, NULL, 'active')
		   ON CONFLICT (patient_id) DO NOTHING`,
			[]any{t378PatOwn, t378PhoneHash("t378a"), t378TeamOwn,
				t378PatOther, t378PhoneHash("t378b"), t378TeamOther,
				t378PatNoTeam, t378PhoneHash("t378c")}},
		{`INSERT INTO devices (device_id, device_secret_enc, patient_id, status) VALUES
		   ($1, '\x00'::bytea, $2, 'online'),
		   ($3, '\x00'::bytea, $4, 'online')
		   ON CONFLICT (device_id) DO NOTHING`,
			[]any{t378DevOwn, t378PatOwn, t378DevOther, t378PatOther}},
		{`INSERT INTO technicians (tech_id, name, phone_enc, phone_hash) VALUES
		   ($1, 'T378 文件技师', '\x00'::bytea, $2)
		   ON CONFLICT (tech_id) DO NOTHING`, []any{t378Tech, t378PhoneHash("t378d")}},
		// 告警：owner_id 存的是 alert_id 的文本形态（前端 FlowRuntime 口径）
		{`INSERT INTO alerts (patient_id, device_id, type, ts) VALUES
		   ($1, $2, 'pressure_high', now()),
		   ($3, $4, 'pressure_high', now())
		   ON CONFLICT (patient_id, device_id, type, ts) DO NOTHING`,
			[]any{t378PatOwn, t378DevOwn, t378PatOther, t378DevOther}},
	}
	for _, s := range stmts {
		_, err := itPool.Exec(ctx, s.sql, s.args...)
		require.NoError(t, err, "t378 it: seed failed: %s", s.sql)
	}

	alertOwn = t378AlertID(t, t378PatOwn)
	alertOther = t378AlertID(t, t378PatOther)

	store := NewPGStore(itPool)
	for _, f := range []struct{ id, ownerType, ownerID string }{
		{t378FileOwn, "patient", t378PatOwn},
		{t378FileOther, "patient", t378PatOther},
		{t378FileNoTeam, "patient", t378PatNoTeam},
		{t378FileGhost, "patient", t378PatGhost},
		{t378FileAlertOK, "alert", strconv.FormatInt(alertOwn, 10)},
		{t378FileAlertNo, "alert", strconv.FormatInt(alertOther, 10)},
		{t378FileTpl, "ReviewTemplate", t378AdmA}, // 非患者材料：任何医护都要看得到
	} {
		fm := newTestFile(f.id)
		fm.FileType = model.FileTypeReviewReport
		fm.OwnerType = f.ownerType
		fm.OwnerID = f.ownerID
		require.NoError(t, store.CreateFile(ctx, fm))
	}

	t.Cleanup(func() {
		cctx := context.Background()
		for _, c := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM files WHERE file_id LIKE $1`, []any{t378FilePrefix + "%"}},
			{`DELETE FROM alerts WHERE patient_id LIKE $1`, []any{"P-FILE-IT-T378-%"}},
			{`DELETE FROM devices WHERE device_id LIKE $1`, []any{"D-FILE-IT-T378-%"}},
			{`DELETE FROM technicians WHERE tech_id = $1`, []any{t378Tech}},
			{`DELETE FROM patients WHERE patient_id LIKE $1`, []any{"P-FILE-IT-T378-%"}},
			{`DELETE FROM doctors WHERE doctor_id LIKE $1`, []any{"DOC-FILE-IT-T378-%"}},
			{`DELETE FROM teams WHERE team_id LIKE $1`, []any{"TEAM-FILE-IT-T378-%"}},
			{`DELETE FROM admins WHERE admin_id LIKE $1`, []any{"ADM-FILE-IT-T378-%"}},
			{`DELETE FROM roles WHERE role_id = $1`, []any{t378Role}},
		} {
			if _, err := itPool.Exec(cctx, c.sql, c.args...); err != nil {
				t.Errorf("t378 it: cleanup %s: %v", c.sql, err)
			}
		}
	})
	return alertOwn, alertOther
}

// t378AlertID 读回某患者名下的告警 ID（IDENTITY 列不可自行指定）。
func t378AlertID(t *testing.T, patientID string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, itPool.QueryRow(context.Background(),
		`SELECT alert_id FROM alerts WHERE patient_id = $1 ORDER BY alert_id LIMIT 1`,
		patientID).Scan(&id))
	return id
}

// t378Mine 只留本次种子的文件 ID（同包其他用例也往 files 表写行）。
func t378Mine(rows []model.FileMetadata) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if strings.HasPrefix(r.FileID, t378FilePrefix) {
			out = append(out, r.FileID)
		}
	}
	return out
}

func TestITT378DoctorTeamByAdmin(t *testing.T) {
	t378Seed(t)
	store := NewPGStore(itPool)
	ctx := context.Background()

	teamID, ok, err := store.DoctorTeamByAdmin(ctx, t378AdmA)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, t378TeamOwn, teamID, "团队唯一事实源是 doctors.team_id（经 admin_id 这一跳）")

	// doctors.team_id 为 NULL → 空串 + ok=false：调用方据此落空集，不得当成「不过滤」
	empty, ok2, err := store.DoctorTeamByAdmin(ctx, t378AdmNot)
	require.NoError(t, err)
	assert.False(t, ok2)
	assert.Equal(t, "", empty)

	// 有登录身份但没有 doctor 行 → 同样是「无归属」而非报错
	missing, ok3, err := store.DoctorTeamByAdmin(ctx, "ADM-FILE-IT-T378-ABSENT")
	require.NoError(t, err)
	assert.False(t, ok3)
	assert.Equal(t, "", missing)
}

func TestITT378FileOwnerInTeam(t *testing.T) {
	t378Seed(t)
	store := NewPGStore(itPool)
	ctx := context.Background()

	probe := func(fileID, teamID string) bool {
		got, err := store.FileOwnerInTeam(ctx, fileID, teamID)
		require.NoError(t, err, "不可见必须是「无行」而不是报错（报错会翻成 500）")
		return got
	}

	// 反证：本团队患者材料必须照常可见，否则收口过头
	assert.True(t, probe(t378FileOwn, t378TeamOwn))
	// 告警材料经 alerts.patient_id 二级反查命中（alert_id::text 与 owner_id 对齐）
	assert.True(t, probe(t378FileAlertOK, t378TeamOwn))
	// 非患者材料不受团队约束
	assert.True(t, probe(t378FileTpl, t378TeamOwn))

	assert.False(t, probe(t378FileOther, t378TeamOwn), "他团队患者材料必须拒")
	assert.False(t, probe(t378FileAlertNo, t378TeamOwn), "他团队告警材料必须拒")
	assert.False(t, probe(t378FileNoTeam, t378TeamOwn), "患者无团队归属＝无主材料，不得默认放行")
	assert.False(t, probe(t378FileGhost, t378TeamOwn), "患者行不存在不得当作可看")
	assert.False(t, probe(t378FilePrefix+"NONE", t378TeamOwn), "查无此文件与跨团队合一（存在性不作探测面）")

	// 无团队医护：患者材料全拒，非患者材料仍可见（与列表口径一致，见下个用例）
	assert.False(t, probe(t378FileOwn, ""), "空 teamID 不得退化成「不过滤」")
	assert.True(t, probe(t378FileTpl, ""), "模板不是患者材料，无团队医护仍应看得到")
}

func TestITT378QueryFilesTeamScope(t *testing.T) {
	t378Seed(t)
	store := NewPGStore(itPool)
	ctx := context.Background()

	// 反证：不收窄时本种子 7 个文件全在（只收紧不放宽）
	all, err := store.QueryFiles(ctx, QueryFilter{FileType: model.FileTypeReviewReport, PageSize: 100})
	require.NoError(t, err)
	assert.Len(t, t378Mine(all), 7, "不收窄时应见全部种子文件")

	own, err := store.QueryFiles(ctx, QueryFilter{
		FileType: model.FileTypeReviewReport, TeamScoped: true, TeamID: t378TeamOwn, PageSize: 100})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{t378FileOwn, t378FileAlertOK, t378FileTpl}, t378Mine(own),
		"收窄后只剩：本团队患者材料 + 本团队告警材料 + 非患者材料")

	// 无团队医护：患者材料清空，但配置类材料不能被一起收掉
	none, err := store.QueryFiles(ctx, QueryFilter{
		FileType: model.FileTypeReviewReport, TeamScoped: true, PageSize: 100})
	require.NoError(t, err)
	assert.Equal(t, []string{t378FileTpl}, t378Mine(none), "空 teamID 落恒假收窄而非全量")

	// total 与 list 同口径（页脚数出他团队行是 T371 那族缺陷的形状）
	unscopedTotal, err := store.CountFiles(ctx, QueryFilter{FileType: model.FileTypeReviewReport})
	require.NoError(t, err)
	assert.Equal(t, int64(len(all)), unscopedTotal)
	scopedTotal, err := store.CountFiles(ctx, QueryFilter{
		FileType: model.FileTypeReviewReport, TeamScoped: true, TeamID: t378TeamOwn})
	require.NoError(t, err)
	assert.Equal(t, int64(len(t378Mine(own))), scopedTotal, "CountFiles 必须与 QueryFiles 同一子句")
	assert.Less(t, scopedTotal, unscopedTotal, "计数也要被收窄（count 漏接 scope 会让分页页脚虚高）")

	// 探测与列表不许分叉：列表说可见的，详情探测也必须说可见
	for _, id := range t378Mine(own) {
		inTeam, probeErr := store.FileOwnerInTeam(ctx, id, t378TeamOwn)
		require.NoError(t, probeErr)
		assert.True(t, inTeam, "列表有行但详情判拒 → 两口径：%s", id)
	}
}

// TestITT378ScopeArgsNoCollision 团队参数与其他过滤参数共存时占位符不串位。
//
// 收窄子句的 $n 由 len(args) 推导：owner_type/file_type/status 先占位、团队号排最后。
// 错位的表现不是「结果为空」而是 PG 直接报类型错、或团队号被当成 owner_id 值 ——
// 所以这里同时断言「无错 + 命中本团队那条 + 拿团队号当 owner_id 查是空集」。
func TestITT378ScopeArgsNoCollision(t *testing.T) {
	t378Seed(t)
	store := NewPGStore(itPool)
	ctx := context.Background()

	f := QueryFilter{
		OwnerType:  "patient",
		Status:     model.FileStatusPending,
		FileType:   model.FileTypeReviewReport,
		TeamScoped: true,
		TeamID:     t378TeamOwn,
		PageSize:   100,
	}
	rows, err := store.QueryFiles(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, []string{t378FileOwn}, t378Mine(rows),
		"三个条件 + 团队参数同时进 SQL，只该按各自字段生效（alert 材料不在本用例的 owner_type=patient 里）")

	cnt, err := store.CountFiles(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, int64(len(rows)), cnt, "同一 filter 的计数与行集必须等量")

	// 团队号若串到 owner_id 位上就会命中这里 —— 必须是空集
	wrong, err := store.QueryFiles(ctx, QueryFilter{OwnerID: t378TeamOwn, PageSize: 100})
	require.NoError(t, err)
	assert.Empty(t, t378Mine(wrong))

	// presign 写侧的 PatientInTeam 四格
	in, err := store.PatientInTeam(ctx, t378PatOwn, t378TeamOwn)
	require.NoError(t, err)
	assert.True(t, in, "反证：本团队患者要能开出上传通道")
	in, err = store.PatientInTeam(ctx, t378PatOther, t378TeamOwn)
	require.NoError(t, err)
	assert.False(t, in, "他团队患者不得开通道")
	in, err = store.PatientInTeam(ctx, t378PatNoTeam, t378TeamOwn)
	require.NoError(t, err)
	assert.False(t, in, "无团队患者不得被任何团队认领")
	in, err = store.PatientInTeam(ctx, t378PatOwn, "")
	require.NoError(t, err)
	assert.False(t, in, "调用者无团队 → 患者材料一律拒")
}

func TestITT378OwnerInTeamDispatch(t *testing.T) {
	alertOwn, alertOther := t378Seed(t)
	store := NewPGStore(itPool)
	ctx := context.Background()

	for _, tc := range []struct {
		ownerType, ownerID, teamID string
		want                       bool
		name                       string
	}{
		{"patient", t378PatOwn, t378TeamOwn, true, "本团队患者：反证"},
		{"patient", t378PatOther, t378TeamOwn, false, "他团队患者：拒"},
		{"alert", strconv.FormatInt(alertOwn, 10), t378TeamOwn, true, "本团队告警：反证"},
		{"alert", strconv.FormatInt(alertOther, 10), t378TeamOwn, false, "他团队告警：拒"},
		{"alert", "999999999", t378TeamOwn, false, "告警不存在：拒"},
		{"ReviewTemplate", t378AdmA, t378TeamOwn, true, "非患者材料：不受团队约束"},
		{"ReviewTemplate", t378AdmA, "", true, "非患者材料 + 无团队医护：仍不受约束"},
		{"install_record", "1", t378TeamOwn, true, "未知 owner_type：按非患者材料处理（与列表 NOT IN 同口径）"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.OwnerInTeam(ctx, tc.ownerType, tc.ownerID, tc.teamID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	// presign 门禁与详情探测必须同结论（否则「签得出传不上去」/「列表看得见」互斥）
	in, err := store.OwnerInTeam(ctx, "alert", strconv.FormatInt(alertOwn, 10), t378TeamOwn)
	require.NoError(t, err)
	require.True(t, in)
	visible, err := store.FileOwnerInTeam(ctx, t378FileAlertOK, t378TeamOwn)
	require.NoError(t, err)
	assert.True(t, visible)
}
