//go:build integration
// +build integration

// Package repo 集成测试：T248 4.3 admin 档案编辑落库 + 7.1 患者沟通统计栏（真实 PG）
//
// 4.3：diagnosis / cobb_angle 经 admin 通道落库并回读一致（患者自助通道无该键，见 T226/T230）；
// 7.1：FeedbackStats 三项聚合口径 —— 今日窗口命中、pending 全量计数、
// 已回复样本均值非 NULL、无样本窗口为 NULL。
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t248Patient = "P-USR-IT-T248"

// seedT248Patient 独立患者 + 独立反馈样本（共享种子库，测后清场，见 T226 IT 同口径）
func seedT248Patient(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := itStore.pool.Exec(ctx, `
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, status)
VALUES ($1, 'T248患者', '\x00'::bytea, 'ab24' || repeat('0', 60), 'male', 14, 'active')
ON CONFLICT (patient_id) DO NOTHING`, t248Patient)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM feedbacks WHERE patient_id = $1`, t248Patient)
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, t248Patient)
	})
}

func TestITUpdatePatientAdminEdit_T248(t *testing.T) {
	ctx := context.Background()
	seedT248Patient(t, ctx)

	diag := "胸弯 Cobb 24°，支具治疗中"
	cobb := 24.0
	require.NoError(t, itStore.UpdatePatientProfile(ctx, t248Patient, PatientProfileUpdate{
		Diagnosis: &diag, CobbAngle: &cobb,
	}))

	after, err := itStore.GetPatient(ctx, t248Patient)
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, diag, derefStr(after.Diagnosis))
	assert.Equal(t, 24.0, derefF(after.CobbAngle))
	assert.Equal(t, "T248患者", after.Name, "未提供的字段不得被改动")

	// NUMERIC(5,2)：两位小数落库无失真；nil=不改（再写 diagnosis 不得冲掉 cobb_angle）
	cobb2 := 37.55
	require.NoError(t, itStore.UpdatePatientProfile(ctx, t248Patient, PatientProfileUpdate{CobbAngle: &cobb2}))
	after2, err := itStore.GetPatient(ctx, t248Patient)
	require.NoError(t, err)
	assert.Equal(t, 37.55, derefF(after2.CobbAngle))
	assert.Equal(t, diag, derefStr(after2.Diagnosis))
}

func TestITFeedbackStats_T248(t *testing.T) {
	ctx := context.Background()
	seedT248Patient(t, ctx)

	// 样本明确落在今日 / 昨日两段：FeedbackStats 窗口由调用方传入（与 handler 同口径整日边界），
	// 由此验证半开区间确实生效。
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	seed := func(content string, submit, reply time.Time, status string) {
		t.Helper()
		// 🔴 reply_time 未回复时必须写 NULL：Go 零值 time.Time 会落成公元 1 年，
		// 被均值查询的 reply_time IS NOT NULL 过滤器收进样本，得出巨大负值。
		var handlerID, replyText, replyTime any
		if !reply.IsZero() {
			handlerID, replyText, replyTime = "D-USR-IT", "已答复", reply
		}
		_, err := itStore.pool.Exec(ctx, `
INSERT INTO feedbacks (patient_id, type, content, submit_time, handler, reply_content, reply_time, status)
VALUES ($1, 'question', $2, $3, $4, $5, $6, $7)`,
			t248Patient, content, submit, handlerID, replyText, replyTime, status)
		require.NoError(t, err)
	}
	seed("T248今日待回复", todayStart.Add(time.Hour), time.Time{}, "pending")
	seed("T248今日已回复", todayStart.Add(2*time.Hour), todayStart.Add(2*time.Hour+10*time.Minute), "replied")
	seed("T248昨日已回复", yesterdayStart.Add(2*time.Hour), yesterdayStart.Add(2*time.Hour+20*time.Minute), "replied")

	row, err := itStore.FeedbackStats(ctx, todayStart, todayStart.AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, row.TodayCount, int64(2), "今日窗口至少命中自造两条（昨日那条不计）")
	assert.GreaterOrEqual(t, row.PendingCount, int64(1))
	require.NotNil(t, row.AvgReplySec, "存在已回复样本时均值不得为 NULL")
	assert.Greater(t, *row.AvgReplySec, 0.0)

	// 无样本窗口 → 计数 0 且均值 NULL（不以 0 冒充「秒回」）
	farStart := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	far, err := itStore.FeedbackStats(ctx, farStart, farStart.AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.Zero(t, far.TodayCount)
	assert.Nil(t, far.AvgReplySec)
}
