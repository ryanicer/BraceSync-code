//go:build integration
// +build integration

// T378：feedbacks 三处归属谓词在真库里的语义（FeedbackInTeam / 带 scope 的 ListFeedbacks / FeedbackStats）。
//
// 单测（team_scope_t378_test.go）只能证明 handler 在越权时没调用写方法、并把 scope 原样传下去；
// 证不了三件事，必须落真库：
//  1. FeedbackInTeam 的四格「不可见」在 SQL 层同为无行（内连接 + p.team_id = $2）：
//     他团队 / 患者 team_id 为 NULL / 反馈不存在 / 调用者无团队 —— 任一格外泄都会让 handler 又能分出 404/403；
//  2. 列表与统计条在 TeamScoped 下确实按患者团队收窄，且 TeamScoped && TeamID=="" 落空集/全零而非「不过滤」；
//  3. keyword 分支的 LEFT JOIN patients 与团队 EXISTS 共用占位符编号不串位（$n 由 len(args)+1 推导，
//     错一位就是 SQL 运行时错误或把团队参数当成关键词）。
//
// 反证同样不可省：不带 scope 时必须仍见全量，否则「只收紧不放宽」的红线被自己判红。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t378ITPatientOwn  = "P-USR-IT-T378A" // 属 itTeam
	t378ITPatientFree = "P-USR-IT-T378B" // team_id 为 NULL
	t378ITForeignTeam = "TEAM-USR-IT-OTHER-T378"
)

// seedT378Feedbacks 两名独立患者各一条反馈（共享种子库，测后清场，口径同 T373 IT）
func seedT378Feedbacks(t *testing.T, ctx context.Context) (ownFB, freeFB int64) {
	t.Helper()
	seedPatient := func(pid, name, hash, teamID string) {
		t.Helper()
		var team any
		if teamID != "" {
			team = teamID
		}
		_, err := itStore.pool.Exec(ctx, `
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, team_id, status)
VALUES ($1, $2, '\x00'::bytea, $3, 'female', 14, $4, 'active')
ON CONFLICT (patient_id) DO NOTHING`, pid, name, hash, team)
		require.NoError(t, err)
	}
	seedPatient(t378ITPatientOwn, "T378本团队患者", "t378a"+strings.Repeat("0", 59), itTeam)
	seedPatient(t378ITPatientFree, "T378未分配患者", "t378b"+strings.Repeat("0", 59), "")

	fb := func(pid, content string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, itStore.pool.QueryRow(ctx, `
INSERT INTO feedbacks (patient_id, type, content, submit_time, status)
VALUES ($1, 'question', $2, $3, 'pending')
RETURNING feedback_id`, pid, content, time.Now().UTC()).Scan(&id))
		return id
	}
	ownFB = fb(t378ITPatientOwn, "T378集成样本-本团队")
	freeFB = fb(t378ITPatientFree, "T378集成样本-未分配")

	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM feedbacks WHERE patient_id = ANY($1)`,
			[]string{t378ITPatientOwn, t378ITPatientFree})
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = ANY($1)`,
			[]string{t378ITPatientOwn, t378ITPatientFree})
	})
	return ownFB, freeFB
}

// listHas 列表结果里是否出现该反馈（按 feedback_id 匹配，避免依赖返回序）
func listHas(rows []FeedbackRow, id int64) bool {
	for _, r := range rows {
		if r.FeedbackID == id {
			return true
		}
	}
	return false
}

func TestITT378FeedbackInTeam(t *testing.T) {
	ctx := context.Background()
	ownFB, freeFB := seedT378Feedbacks(t, ctx)

	var maxID int64
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(feedback_id), 0) FROM feedbacks`).Scan(&maxID))
	missingFB := maxID + 1000 // 真库里必然无此反馈行

	// 1. 反证：本团队反馈判得到，否则医护处理面被整条锁死
	own, err := itStore.FeedbackInTeam(ctx, ownFB, itTeam)
	require.NoError(t, err)
	assert.True(t, own, "本团队反馈必须放行，否则收口过头")

	// 2~5. 四格不可见同形：既不报错也不返回 true（报错会变 500，那就又可分了）
	for _, tc := range []struct {
		name   string
		fbID   int64
		teamID string
	}{
		{"他团队患者名下的反馈", ownFB, t378ITForeignTeam},
		{"患者未分配团队（team_id 为 NULL）", freeFB, itTeam},
		{"反馈不存在（存在性探测）", missingFB, itTeam},
		{"调用者无团队归属（teamID 空串）", ownFB, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, denyErr := itStore.FeedbackInTeam(ctx, tc.fbID, tc.teamID)
			require.NoError(t, denyErr, "不可见必须是「无行」而不是报错")
			assert.False(t, got)
		})
	}
}

func TestITT378ListFeedbacksTeamScope(t *testing.T) {
	ctx := context.Background()
	ownFB, freeFB := seedT378Feedbacks(t, ctx)

	// 反证：不带 scope（运营 / 客服）仍见全量两条
	all, err := itStore.ListFeedbacks(ctx, "", FeedbackScope{})
	require.NoError(t, err)
	assert.True(t, listHas(all, ownFB) && listHas(all, freeFB), "不受限角色不得被收窄")

	// 本团队收窄：只剩那条
	scoped, err := itStore.ListFeedbacks(ctx, "", FeedbackScope{TeamScoped: true, TeamID: itTeam})
	require.NoError(t, err)
	assert.True(t, listHas(scoped, ownFB))
	assert.False(t, listHas(scoped, freeFB), "未分配团队患者那条不得混进本团队列表")

	// 无团队归属 → 空集（不得回落全院）
	none, err := itStore.ListFeedbacks(ctx, "", FeedbackScope{TeamScoped: true})
	require.NoError(t, err)
	assert.Len(t, none, 0, "TeamScoped && 空 TeamID 必须落空集")

	// keyword + scope 同时存在：占位符编号不得串位（keyword 命中 + 团队过滤同时生效）
	kw, err := itStore.ListFeedbacks(ctx, "本团队", FeedbackScope{TeamScoped: true, TeamID: itTeam})
	require.NoError(t, err)
	assert.True(t, listHas(kw, ownFB), "keyword 分支要能命中本团队样本")
	assert.False(t, listHas(kw, freeFB), "keyword 分支里团队谓词同样生效")

	kwMiss, err := itStore.ListFeedbacks(ctx, "未分配", FeedbackScope{TeamScoped: true, TeamID: itTeam})
	require.NoError(t, err)
	assert.Len(t, kwMiss, 0, "keyword 命中他/无团队样本时，团队谓词要把它滤掉")
}

func TestITT378FeedbackStatsTeamScope(t *testing.T) {
	ctx := context.Background()
	seedT378Feedbacks(t, ctx)

	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	todayEnd := todayStart.AddDate(0, 0, 1)

	// 今日两条种子（本团队 + 未分配）都在窗口内 → 全量口径 pending 至少 2
	all, err := itStore.FeedbackStats(ctx, todayStart, todayEnd, FeedbackScope{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, all.PendingCount, int64(2), "反证：不受限统计含两条种子")

	// 本团队：只数得到本团队那条，未分配那条被排除
	own, err := itStore.FeedbackStats(ctx, todayStart, todayEnd, FeedbackScope{TeamScoped: true, TeamID: itTeam})
	require.NoError(t, err)
	assert.Less(t, own.PendingCount, all.PendingCount, "团队统计条必须严格小于全量，否则谓词没生效")

	// 无团队归属：全零（不得回落全量）
	none, err := itStore.FeedbackStats(ctx, todayStart, todayEnd, FeedbackScope{TeamScoped: true})
	require.NoError(t, err)
	assert.Zero(t, none.TodayCount)
	assert.Zero(t, none.PendingCount)
}
