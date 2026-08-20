//go:build integration
// +build integration

// Package repo 集成测试：真实 PG15（testcontainers）验证 T030 admin 域 SQL
//
// 对齐：docs/ §1（集成层）· T030 验收：
//
//	患者 join 查询（teams/doctors 姓名）/ 筛选分页 / 技师写入与手机号查重 /
//	反馈回复落库 / 方案版本递增 / 感受日志回复 / 权限矩阵读写 / sys_configs UPSERT
//
// 运行：make test-integration（需 Docker）
package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
)

var itStore *PGStore

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		return runIT(m, cfg.DBURL)
	})
}

// runIT 建连 + 迁移 + 种子 + 执行用例（user-service 仅依赖 PG）
func runIT(m *testing.M, dbURL string) int {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "it: pgxpool: %v\n", err)
		return 1
	}
	defer pool.Close()
	itStore = NewPGStore(pool)

	applyMigrations(ctx, pool)
	seedITData(ctx, pool)
	return m.Run()
}

// migrationsDir 定位 scripts/db/migrations（相对本文件 4 级上）
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "db", "migrations")
}

// applyMigrations 顺序执行全部 *.up.sql（单一事实源）
func applyMigrations(ctx context.Context, pool *pgxpool.Pool) {
	entries, err := os.ReadDir(migrationsDir())
	if err != nil {
		panic("it: read migrations dir: " + err.Error())
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		sqlBytes, readErr := os.ReadFile(filepath.Join(migrationsDir(), name))
		if readErr != nil {
			panic("it: read migration: " + readErr.Error())
		}
		if _, execErr := pool.Exec(ctx, string(sqlBytes)); execErr != nil {
			panic(fmt.Sprintf("it: apply %s: %v", name, execErr))
		}
	}
}

const (
	itTeam   = "TEAM-USR-IT"
	itDoctor = "DOC-USR-IT"
	itAdmin  = "ADM-USR-IT"
)

// seedITData 集成测试专用种子（避开 scripts/db/seed 的共享 ID，防用例间污染）
func seedITData(ctx context.Context, pool *pgxpool.Pool) {
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO teams (team_id, name, member_count, patient_count) VALUES ($1, '集成团队', 2, 2)
		 ON CONFLICT (team_id) DO NOTHING`, []any{itTeam}},
		{`INSERT INTO roles (role_id, name, description, permissions_json) VALUES
		   ('ROLE_IT', '集成角色', '测试', '{"scope":"team","modules":["alerts"]}')
		 ON CONFLICT (role_id) DO NOTHING`, nil},
		{`INSERT INTO admins (admin_id, username, name, password_hash, role_id) VALUES
		   ($1, 'it_admin', '集成账号', '$2a$dummy', 'ROLE_IT')
		 ON CONFLICT (admin_id) DO NOTHING`, []any{itAdmin}},
		{`INSERT INTO doctors (doctor_id, name, title, department, team_id, admin_id) VALUES
		   ($1, '集成医生', '主治医师', '骨科', $2, $3)
		 ON CONFLICT (doctor_id) DO NOTHING`, []any{itDoctor, itTeam, itAdmin}},
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, team_id, primary_doctor_id, status) VALUES
		   ('P-USR-IT-1', '集成患者甲', '\x00'::bytea, 'aa01' || repeat('0', 60), 'male', 13, $1, $2, 'active'),
		   ('P-USR-IT-2', '集成患者乙', '\x00'::bytea, 'aa02' || repeat('0', 60), 'female', 11, NULL, NULL, 'pending')
		 ON CONFLICT (patient_id) DO NOTHING`, []any{itTeam, itDoctor}},
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			panic("it: seed: " + err.Error())
		}
	}
}

// ─────────────────────────────────────────────────────────────
// 患者 join / 筛选 / 分页（T030 #1/#2）
// ─────────────────────────────────────────────────────────────

func TestITListPatientsJoinAndFilter(t *testing.T) {
	ctx := context.Background()

	// 全量：join 姓名返回
	rows, total, err := itStore.ListPatients(ctx, PatientFilter{Page: 1, PageSize: 100})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, rows, 2) // created_at DESC：两条同刻，patient_id 次序稳定
	var p1 *PatientRow
	for i := range rows {
		if rows[i].PatientID == "P-USR-IT-1" {
			p1 = &rows[i]
		}
	}
	require.NotNil(t, p1)
	require.NotNil(t, p1.TeamName)
	assert.Equal(t, "集成团队", *p1.TeamName)
	require.NotNil(t, p1.DoctorName)
	assert.Equal(t, "集成医生", *p1.DoctorName)

	// keyword 命中患者ID（ILIKE）
	rows, total, err = itStore.ListPatients(ctx, PatientFilter{Keyword: "p-usr-it", Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, rows, 2)

	// keyword 命中姓名
	rows, total, err = itStore.ListPatients(ctx, PatientFilter{Keyword: "患者甲", Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	assert.Equal(t, "P-USR-IT-1", rows[0].PatientID)

	// teamId 过滤
	rows, total, err = itStore.ListPatients(ctx, PatientFilter{TeamID: itTeam, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	// 分页
	rows, total, err = itStore.ListPatients(ctx, PatientFilter{Page: 2, PageSize: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, rows, 1)

	// 详情
	p, err := itStore.GetPatient(ctx, "P-USR-IT-2")
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Nil(t, p.TeamName) // 未分配团队 → null
	assert.Equal(t, "pending", p.Status)

	p, err = itStore.GetPatient(ctx, "P-NOPE")
	require.NoError(t, err)
	assert.Nil(t, p)
}

// ─────────────────────────────────────────────────────────────
// 技师写入（T030 #4）
// ─────────────────────────────────────────────────────────────

func TestITTechnicianLifecycle(t *testing.T) {
	ctx := context.Background()
	teamID := itTeam

	created, err := itStore.CreateTechnician(ctx, TechInput{
		TechID: "TECH-USR-IT-1", Name: "集成技师", PhoneEnc: []byte("enc1"), PhoneHash: "hash-it-1", TeamID: &teamID,
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "enabled", created.Status)
	assert.Equal(t, "authorized", created.AuthStatus)
	assert.Equal(t, "集成技师", created.Name)
	assert.Equal(t, "hash-it-1", created.PhoneHash)

	// 查重：自身不算占用；他人占用命中
	taken, err := itStore.TechPhoneHashTaken(ctx, "hash-it-1", "TECH-USR-IT-1")
	require.NoError(t, err)
	assert.False(t, taken)
	taken, err = itStore.TechPhoneHashTaken(ctx, "hash-it-1", "")
	require.NoError(t, err)
	assert.True(t, taken)

	// 编辑：改名 + 换团队
	updated, err := itStore.UpdateTechnician(ctx, "TECH-USR-IT-1", TechInput{
		Name: "集成技师改", PhoneEnc: []byte("enc2"), PhoneHash: "hash-it-2", TeamID: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "集成技师改", updated.Name)
	assert.Nil(t, updated.TeamID)

	// 编辑不存在的技师 → nil
	updated, err = itStore.UpdateTechnician(ctx, "TECH-NOPE", TechInput{Name: "x", PhoneHash: "h"})
	require.NoError(t, err)
	assert.Nil(t, updated)

	// 启停
	exists, err := itStore.ToggleTechnician(ctx, "TECH-USR-IT-1", "disabled")
	require.NoError(t, err)
	assert.True(t, exists)
	row, err := itStore.GetTechnician(ctx, "TECH-USR-IT-1")
	require.NoError(t, err)
	assert.Equal(t, "disabled", row.Status)
	exists, err = itStore.ToggleTechnician(ctx, "TECH-NOPE", "enabled")
	require.NoError(t, err)
	assert.False(t, exists)

	// 列表分页
	list, total, err := itStore.ListTechnicians(ctx, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, list, 1)

	// 团队成员查询
	teamTechs, err := itStore.ListTechniciansByTeam(ctx, "TEAM-EMPTY")
	require.NoError(t, err)
	assert.Empty(t, teamTechs)
}

// ─────────────────────────────────────────────────────────────
// 反馈 / 方案 / 感受日志（T030 #5/#6）
// ─────────────────────────────────────────────────────────────

func TestITFeedbackProcess(t *testing.T) {
	ctx := context.Background()
	pool := itStore.pool

	tag, err := pool.Exec(ctx,
		`INSERT INTO feedbacks (patient_id, type, content, status) VALUES ('P-USR-IT-1', '佩戴咨询', '集成反馈', 'pending')`)
	require.NoError(t, err)
	assert.Equal(t, int64(1), tag.RowsAffected())

	var feedbackID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT feedback_id FROM feedbacks WHERE patient_id='P-USR-IT-1' AND content='集成反馈'`).Scan(&feedbackID))

	reply := "已处理，正常现象"
	ok, err := itStore.ProcessFeedback(ctx, feedbackID, "A-IT", &reply)
	require.NoError(t, err)
	assert.True(t, ok)

	var status, handler string
	var replyContent *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, handler, reply_content FROM feedbacks WHERE feedback_id = $1`, feedbackID).
		Scan(&status, &handler, &replyContent))
	assert.Equal(t, "replied", status)
	assert.Equal(t, "A-IT", handler)
	require.NotNil(t, replyContent)
	assert.Equal(t, reply, *replyContent)

	// resolved 不回退
	_, err = pool.Exec(ctx, `UPDATE feedbacks SET status='resolved' WHERE feedback_id = $1`, feedbackID)
	require.NoError(t, err)
	ok, err = itStore.ProcessFeedback(ctx, feedbackID, "A-IT", nil)
	require.NoError(t, err)
	assert.True(t, ok)
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM feedbacks WHERE feedback_id = $1`, feedbackID).Scan(&status))
	assert.Equal(t, "resolved", status)

	// 不存在
	ok, err = itStore.ProcessFeedback(ctx, 999999, "A", nil)
	require.NoError(t, err)
	assert.False(t, ok)

	// 列表 + keyword
	list, err := itStore.ListFeedbacks(ctx, "集成反馈")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "P-USR-IT-1", list[0].PatientID)
	list, err = itStore.ListFeedbacks(ctx, "不存在的关键词zz")
	require.NoError(t, err)
	assert.Empty(t, list)
	list, err = itStore.ListFeedbacks(ctx, "P-USR-IT-1") // 患者ID 命中
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestITOrthosisPlansVersioning(t *testing.T) {
	ctx := context.Background()

	// 无方案 → ok=false
	latest, ok, err := itStore.LatestPlanVersion(ctx, "P-USR-IT-1")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, latest)

	p1, err := itStore.CreatePlan(ctx, "P-USR-IT-1", itDoctor, "首版方案", "v1.0")
	require.NoError(t, err)
	assert.Equal(t, "v1.0", p1.Version)
	p2, err := itStore.CreatePlan(ctx, "P-USR-IT-1", itDoctor, "二版方案", "v1.1")
	require.NoError(t, err)
	assert.NotEqual(t, p1.PlanID, p2.PlanID)

	latest, ok, err = itStore.LatestPlanVersion(ctx, "P-USR-IT-1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "v1.1", latest)

	plans, err := itStore.ListPlans(ctx, "P-USR-IT-1")
	require.NoError(t, err)
	require.Len(t, plans, 2)
	assert.Equal(t, "v1.1", plans[0].Version) // 倒序
}

func TestITFeelingLogReply(t *testing.T) {
	ctx := context.Background()
	pool := itStore.pool

	_, err := pool.Exec(ctx,
		`INSERT INTO feeling_logs (patient_id, log_date, comfort_score, discomfort_areas, notes)
		 VALUES ('P-USR-IT-1', '2026-08-05', 3.5, ARRAY['lumbar']::varchar[], '腰部不适')
		 ON CONFLICT (patient_id, log_date) DO NOTHING`)
	require.NoError(t, err)

	logs, err := itStore.ListFeelingLogs(ctx, "P-USR-IT-1")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, []string{"lumbar"}, logs[0].DiscomfortAreas)
	assert.Nil(t, logs[0].ReplyContent)

	var logID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT log_id FROM feeling_logs WHERE patient_id='P-USR-IT-1' AND log_date='2026-08-05'`).Scan(&logID))

	ok, err := itStore.ReplyFeelingLog(ctx, logID, "建议复查")
	require.NoError(t, err)
	assert.True(t, ok)

	logs, err = itStore.ListFeelingLogs(ctx, "P-USR-IT-1")
	require.NoError(t, err)
	require.NotNil(t, logs[0].ReplyContent)
	assert.Equal(t, "建议复查", *logs[0].ReplyContent)
	assert.NotNil(t, logs[0].ReplyTime)

	// 覆盖回复
	ok, err = itStore.ReplyFeelingLog(ctx, logID, "已调整方案")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = itStore.ReplyFeelingLog(ctx, 999999, "x")
	require.NoError(t, err)
	assert.False(t, ok)
}

// ─────────────────────────────────────────────────────────────
// 角色 / 权限矩阵 / 系统配置（T030 #7/#8）+ 登录查询（T030 #9）
// ─────────────────────────────────────────────────────────────

func TestITRolesAndPermissions(t *testing.T) {
	ctx := context.Background()

	roles, err := itStore.ListRoles(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, roles)

	var itRole *RoleRow
	for i := range roles {
		if roles[i].RoleID == "ROLE_IT" {
			itRole = &roles[i]
		}
	}
	require.NotNil(t, itRole)
	assert.Contains(t, itRole.PermissionsJSON, "alerts")

	role, err := itStore.GetRole(ctx, "ROLE_IT")
	require.NoError(t, err)
	require.NotNil(t, role)
	role, err = itStore.GetRole(ctx, "ROLE-NOPE")
	require.NoError(t, err)
	assert.Nil(t, role)

	// 权限矩阵写入
	ok, err := itStore.UpdateRolePermissions(ctx, "ROLE_IT", `{"scope":"all","modules":["comm","alerts"]}`)
	require.NoError(t, err)
	assert.True(t, ok)
	role, err = itStore.GetRole(ctx, "ROLE_IT")
	require.NoError(t, err)
	assert.Contains(t, role.PermissionsJSON, "comm")

	ok, err = itStore.UpdateRolePermissions(ctx, "ROLE-NOPE", `{}`)
	require.NoError(t, err)
	assert.False(t, ok)

	// RoleScope
	scope, err := itStore.RoleScope(ctx, "ROLE_IT")
	require.NoError(t, err)
	assert.Equal(t, "all", scope)
	scope, err = itStore.RoleScope(ctx, "ROLE-NOPE")
	require.NoError(t, err)
	assert.Empty(t, scope)
}

func TestITConfigsUpsert(t *testing.T) {
	ctx := context.Background()

	kvs := []ConfigKV{
		{Key: "it_key_a", Value: "1"},
		{Key: "it_key_b", Value: "x"},
	}
	require.NoError(t, itStore.UpsertConfigs(ctx, kvs, "A-IT"))

	got, err := itStore.GetConfigs(ctx, []string{"it_key_a", "it_key_b", "it_missing"})
	require.NoError(t, err)
	assert.Equal(t, "1", got["it_key_a"])
	assert.Equal(t, "x", got["it_key_b"])
	_, exists := got["it_missing"]
	assert.False(t, exists)

	// 幂等覆盖
	kvs[0].Value = "2"
	require.NoError(t, itStore.UpsertConfigs(ctx, kvs, "A-IT"))
	got, err = itStore.GetConfigs(ctx, []string{"it_key_a"})
	require.NoError(t, err)
	assert.Equal(t, "2", got["it_key_a"])
}

func TestITAdminLoginQueries(t *testing.T) {
	ctx := context.Background()

	admin, err := itStore.GetAdminByUsername(ctx, "it_admin")
	require.NoError(t, err)
	require.NotNil(t, admin)
	assert.Equal(t, itAdmin, admin.AdminID)
	assert.Equal(t, "ROLE_IT", admin.RoleID)

	admin, err = itStore.GetAdminByUsername(ctx, "ghost_user")
	require.NoError(t, err)
	assert.Nil(t, admin)

	doctorID, found, err := itStore.DoctorIDByAdmin(ctx, itAdmin)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, itDoctor, doctorID)

	_, found, err = itStore.DoctorIDByAdmin(ctx, "ADM-NOPE")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestITTeamsAndDoctors(t *testing.T) {
	ctx := context.Background()

	teams, err := itStore.ListTeams(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, teams)

	exists, err := itStore.TeamExists(ctx, itTeam)
	require.NoError(t, err)
	assert.True(t, exists)
	exists, err = itStore.TeamExists(ctx, "TEAM-NOPE")
	require.NoError(t, err)
	assert.False(t, exists)

	doctors, err := itStore.ListDoctors(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, doctors)
	var itDoc *DoctorRow
	for i := range doctors {
		if doctors[i].DoctorID == itDoctor {
			itDoc = &doctors[i]
		}
	}
	require.NotNil(t, itDoc)
	assert.Equal(t, 1, itDoc.PatientCount) // P-USR-IT-1 归属该医生

	teamDocs, err := itStore.ListDoctorsByTeam(ctx, itTeam)
	require.NoError(t, err)
	require.Len(t, teamDocs, 1)
	assert.Equal(t, itDoctor, teamDocs[0].DoctorID)
}
