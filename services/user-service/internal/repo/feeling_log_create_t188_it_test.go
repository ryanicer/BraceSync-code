//go:build integration
// +build integration

// Package repo 集成测试：T188 患者端佩戴感受写入（SaveFeelingLog）。
//
// 守三件 fake 证不到的事：
//  1. uk（patient_id, log_date）下第二次写是覆盖同一行，不是插第二条；
//  2. 覆盖时医生已写的 reply_content / reply_time 不动（PM 采纳的 Q3 口径）；
//  3. log_date 走 YYYY-MM-DD 文本参数，不因会话时区偏移一天；中文部位名原样回读。
//
// 夹具用独立患者，测后清场，不改 seed（口径同 T290 IT）。
package repo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t188ITPatient = "P-USR-IT-T188"

func seedT188Patient(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := itStore.pool.Exec(ctx, `
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, status)
VALUES ($1, 'T188录入患者', '\x00'::bytea, $2, 'female', 14, 'active')
ON CONFLICT (patient_id) DO NOTHING`,
		t188ITPatient, "t188i"+strings.Repeat("0", 59))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM feeling_logs WHERE patient_id = $1`, t188ITPatient)
		_, _ = itStore.pool.Exec(ctx, `DELETE FROM patients WHERE patient_id = $1`, t188ITPatient)
	})
}

func t188Count(t *testing.T, ctx context.Context, day string) int {
	t.Helper()
	var n int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM feeling_logs WHERE patient_id = $1 AND log_date = $2`,
		t188ITPatient, day).Scan(&n))
	return n
}

func TestITSaveFeelingLog_T188_CreateThenSameDayOverwrite(t *testing.T) {
	ctx := context.Background()
	seedT188Patient(t, ctx)

	notes := "胸椎处压得疼"
	row, err := itStore.SaveFeelingLog(ctx, FeelingLogSaveInput{
		PatientID:       t188ITPatient,
		LogDate:         "2026-09-10",
		ComfortLevel:    "discomfort",
		DiscomfortAreas: []string{"胸椎", "右侧腰"},
		Notes:           &notes,
	})
	require.NoError(t, err)
	assert.Greater(t, row.LogID, int64(0))
	assert.Equal(t, "2026-09-10", row.LogDate.Format("2006-01-02"),
		"log_date 用文本参数直落 DATE 列；若按 timestamptz 转换会被会话时区挪一天")
	require.NotNil(t, row.ComfortLevel)
	assert.Equal(t, "discomfort", *row.ComfortLevel)
	assert.Equal(t, []string{"胸椎", "右侧腰"}, row.DiscomfortAreas, "中文部位名原样回读，不被替换为问号")
	require.NotNil(t, row.Notes)
	assert.Equal(t, notes, *row.Notes)
	assert.Nil(t, row.ReplyContent)
	assert.Nil(t, row.ComfortScore, "写侧不补旧星级口径（PRD V3.17 已注明作废）")
	assert.False(t, row.CreatedAt.IsZero())

	// 同日再录：覆盖同一行，不产生第二条（uk patient_id+log_date）
	row2, err := itStore.SaveFeelingLog(ctx, FeelingLogSaveInput{
		PatientID:    t188ITPatient,
		LogDate:      "2026-09-10",
		ComfortLevel: "fitted",
	})
	require.NoError(t, err)
	assert.Equal(t, row.LogID, row2.LogID, "同日重复提交要落在同一行")
	require.NotNil(t, row2.ComfortLevel)
	assert.Equal(t, "fitted", *row2.ComfortLevel)
	assert.Equal(t, 1, t188Count(t, ctx, "2026-09-10"))
	assert.Empty(t, row2.DiscomfortAreas, "第二次未传部位 ⇒ 覆盖后为空，不残留上次多选")
	assert.Nil(t, row2.Notes)

	logs, err := itStore.ListFeelingLogs(ctx, t188ITPatient)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "fitted", *logs[0].ComfortLevel)
}

// TestITSaveFeelingLog_T188_KeepsDoctorReply Q3 裁定的核心断言：覆盖当日录入内容，
// 但医生回复位（reply_content / reply_time）不能被患者的一次重录洗掉。
func TestITSaveFeelingLog_T188_KeepsDoctorReply(t *testing.T) {
	ctx := context.Background()
	seedT188Patient(t, ctx)

	notes := "骶骨磨红"
	row, err := itStore.SaveFeelingLog(ctx, FeelingLogSaveInput{
		PatientID:       t188ITPatient,
		LogDate:         "2026-09-11",
		ComfortLevel:    "discomfort",
		DiscomfortAreas: []string{"骶骨"},
		Notes:           &notes,
	})
	require.NoError(t, err)

	ok, err := itStore.ReplyFeelingLog(ctx, row.LogID, "磨红处每日拍照记录")
	require.NoError(t, err)
	require.True(t, ok)

	again, err := itStore.SaveFeelingLog(ctx, FeelingLogSaveInput{
		PatientID:    t188ITPatient,
		LogDate:      "2026-09-11",
		ComfortLevel: "fitted",
	})
	require.NoError(t, err)
	require.NotNil(t, again.ReplyContent, "覆盖更新不得洗掉医生回复")
	assert.Equal(t, "磨红处每日拍照记录", *again.ReplyContent)
	assert.NotNil(t, again.ReplyTime)
	assert.Equal(t, row.LogID, again.LogID)
}

func TestITSaveFeelingLog_T188_PatientForeignKeyMiss(t *testing.T) {
	ctx := context.Background()
	_, err := itStore.SaveFeelingLog(ctx, FeelingLogSaveInput{
		PatientID:    "P-USR-IT-T188-不存在",
		LogDate:      "2026-09-12",
		ComfortLevel: "fitted",
	})
	assert.True(t, errors.Is(err, ErrPatientNotFound),
		"外键不命中要映射成哨兵错误，handler 才能回 404 而不是 500，got=%v", err)
}
