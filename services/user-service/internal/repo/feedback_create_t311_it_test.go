//go:build integration
// +build integration

// Package repo 集成测试：T311 反馈创建端点落库（真实 PG）
//
// 覆盖 handler + fakeStore 单测证不到的三件事：
//  1. INSERT 列与 feedbacks 表实际对齐、feedback_id 由 IDENTITY 生成并回传；
//  2. submit_time 走库默认 now()，handler/reply_content/reply_time 落库为 NULL（客服未处理态）；
//  3. patient_id 外键与 status/content 的库约束确实是权威（handler 预拦之外的兜底）。
//
// 独立患者 + 测后清场，口径同 feedback_stats_admin_edit_t248_it_test.go（共享种子库）。
package repo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t311ITPatient = "P-USR-IT-T311"

func seedT311Patient(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := itStore.pool.Exec(ctx, `
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, status)
VALUES ($1, 'T311患者', '\x00'::bytea, 'cd31' || repeat('0', 60), 'female', 13, 'active')
ON CONFLICT (patient_id) DO NOTHING`, t311ITPatient)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM feedbacks WHERE patient_id = $1`, t311ITPatient)
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, t311ITPatient)
	})
}

// TestITCreateFeedback_T311 正例：落库 → 列表端点用的同一条查询能查回，字段逐项一致
func TestITCreateFeedback_T311(t *testing.T) {
	ctx := context.Background()
	seedT311Patient(t, ctx)

	content := "T311配网失败存档；设备 BSYNC-0A12；发生时间 2026-09-22 12:00"
	id, err := itStore.CreateFeedback(ctx, FeedbackCreateInput{
		PatientID: t311ITPatient, Type: "wifi_setup_failure", Content: content, Status: "pending",
	})
	require.NoError(t, err)
	assert.Positive(t, id, "feedback_id 应由 IDENTITY 生成")

	rows, err := itStore.ListFeedbacks(ctx, "T311配网失败存档", FeedbackScope{})
	require.NoError(t, err)
	var got *FeedbackRow
	for i := range rows {
		if rows[i].FeedbackID == id {
			got = &rows[i]
		}
	}
	require.NotNil(t, got, "新建反馈应能被列表查询按 keyword 查回")
	assert.Equal(t, t311ITPatient, got.PatientID)
	assert.Equal(t, "wifi_setup_failure", derefStr(got.Type))
	assert.Equal(t, content, got.Content)
	assert.Equal(t, "pending", got.Status)
	assert.False(t, got.SubmitTime.IsZero(), "submit_time 应由库默认 now() 填充")
	assert.Nil(t, got.Handler, "新建态不得带处理人")
	assert.Nil(t, got.ReplyContent)
	assert.Nil(t, got.ReplyTime)
}

// TestITCreateFeedback_T311_PatientNotFound 外键兜底：患者不存在 → ErrPatientNotFound（handler 映射 404）
func TestITCreateFeedback_T311_PatientNotFound(t *testing.T) {
	ctx := context.Background()
	_, err := itStore.CreateFeedback(ctx, FeedbackCreateInput{
		PatientID: "P-USR-IT-T311-NOT-EXIST", Type: "wifi_setup_failure", Content: "x", Status: "pending",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPatientNotFound), "应映射为 sentinel 而非裸抛外键错误：%v", err)
}

// TestITCreateFeedback_T311_DBConstraints 库约束是权威：status CHECK 与 content 列宽
// （handler 已预拦为 400，此处证明绕过 handler 直写也不会落进脏数据）
func TestITCreateFeedback_T311_DBConstraints(t *testing.T) {
	ctx := context.Background()
	seedT311Patient(t, ctx)

	_, err := itStore.CreateFeedback(ctx, FeedbackCreateInput{
		PatientID: t311ITPatient, Type: "question", Content: "脏状态", Status: "closed",
	})
	assert.Error(t, err, "status 不在 CHECK 枚举内应被库拒绝")

	_, err = itStore.CreateFeedback(ctx, FeedbackCreateInput{
		PatientID: t311ITPatient, Type: "question", Content: strings.Repeat("长", 501), Status: "pending",
	})
	assert.Error(t, err, "content 超 VARCHAR(500) 应被库拒绝")
}
