//go:build integration
// +build integration

// Package repo 集成测试 — T028 公开查询/处理（真实 PG15）
//
// 装配复用 integration_test.go 的 TestMain/itPool/种子数据；本文件仅补 T028 用例：
//
//	分页 + patientId/type/status 筛选 + ts DESC 排序
//	process 幂等（重复处理不报错、不重写处理时间）+ 不存在返回 exists=false
//
// 运行：make test-integration（需 Docker）
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/alert-service/internal/engine"
	"github.com/bracesync/bracesync/services/alert-service/internal/scanner"
)

// seedQAlerts 造 N 条 pressure_high（P001/DEV-IT-A001，ts 逐分钟递增）+ 1 条 wear_interrupt
func seedQAlerts(ctx context.Context, t *testing.T, n int) {
	t.Helper()
	_, err := itPool.Exec(ctx, `TRUNCATE TABLE alerts RESTART IDENTITY`)
	require.NoError(t, err)
	repo := NewAlertRepo(itPool)
	base := time.Now().Add(-time.Duration(n) * time.Minute).Truncate(time.Second)
	for i := 0; i < n; i++ {
		_, created, cErr := repo.CreateAlert(ctx, scanner.NewAlert{
			PatientID: itPatient, DeviceID: itDevice, Type: engine.TypePressureHigh,
			SensorPoint: "P03", Detail: "IT 压力高", ThresholdValue: 45, ActualValue: 50,
			Ts: base.Add(time.Duration(i) * time.Minute),
		})
		require.NoError(t, cErr)
		require.True(t, created)
	}
	_, created, err := repo.CreateAlert(ctx, scanner.NewAlert{
		PatientID: itPatient, DeviceID: itDevice, Type: engine.TypeWearInterrupt,
		Detail: "IT 中断", Ts: base.Add(-time.Minute),
	})
	require.NoError(t, err)
	require.True(t, created)
}

func TestIT_T028_ListAlerts_PaginationAndOrder(t *testing.T) {
	ctx := context.Background()
	seedQAlerts(ctx, t, 5) // 5 条 pressure_high + 1 条 wear_interrupt
	r := NewAlertRepo(itPool)

	// 全量：total=6，ts DESC 排序（最新在前）
	rows, total, err := r.ListAlerts(ctx, AlertQueryFilter{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 6, total)
	require.Len(t, rows, 6)
	for i := 1; i < len(rows); i++ {
		assert.False(t, rows[i].Ts.After(rows[i-1].Ts), "ts DESC 排序")
	}

	// 分页：pageSize=2 共 3 页，页间不重叠
	page1, total1, err := r.ListAlerts(ctx, AlertQueryFilter{Page: 1, PageSize: 2})
	require.NoError(t, err)
	assert.EqualValues(t, 6, total1)
	require.Len(t, page1, 2)
	page3, _, err := r.ListAlerts(ctx, AlertQueryFilter{Page: 3, PageSize: 2})
	require.NoError(t, err)
	require.Len(t, page3, 2)
	assert.NotEqual(t, page1[0].AlertID, page3[0].AlertID, "翻页不重叠")

	// 越界页返回空列表但 total 不变
	over, total2, err := r.ListAlerts(ctx, AlertQueryFilter{Page: 99, PageSize: 20})
	require.NoError(t, err)
	assert.Empty(t, over)
	assert.EqualValues(t, 6, total2)
}

func TestIT_T028_ListAlerts_Filters(t *testing.T) {
	ctx := context.Background()
	seedQAlerts(ctx, t, 3)
	r := NewAlertRepo(itPool)

	// patientId 筛选
	rows, total, err := r.ListAlerts(ctx, AlertQueryFilter{PatientID: itPatient})
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	assert.Len(t, rows, 4)

	rows, total, err = r.ListAlerts(ctx, AlertQueryFilter{PatientID: "P-NOT-EXIST"})
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, rows)

	// type 筛选
	rows, total, err = r.ListAlerts(ctx, AlertQueryFilter{Type: string(engine.TypeWearInterrupt)})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, string(engine.TypeWearInterrupt), rows[0].Type)

	// status（process_status）筛选：初始全部 pending
	_, total, err = r.ListAlerts(ctx, AlertQueryFilter{Status: "pending"})
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	_, total, err = r.ListAlerts(ctx, AlertQueryFilter{Status: "processed"})
	require.NoError(t, err)
	assert.Zero(t, total)

	// 组合筛选：patientId + type + status
	rows, total, err = r.ListAlerts(ctx, AlertQueryFilter{
		PatientID: itPatient, Type: string(engine.TypePressureHigh), Status: "pending",
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, rows, 3)
}

func TestIT_T028_ListAlerts_FieldProjection(t *testing.T) {
	ctx := context.Background()
	seedQAlerts(ctx, t, 1)
	r := NewAlertRepo(itPool)

	rows, _, err := r.ListAlerts(ctx, AlertQueryFilter{Type: string(engine.TypePressureHigh)})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	row := rows[0]
	assert.Greater(t, row.AlertID, int64(0))
	assert.Equal(t, itPatient, row.PatientID)
	assert.Equal(t, itDevice, row.DeviceID)
	assert.Equal(t, "P03", row.SensorPoint)
	assert.InDelta(t, 45.0, row.ThresholdValue, 0.001)
	assert.InDelta(t, 50.0, row.ActualValue, 0.001)
	assert.Equal(t, "unread", row.ReadStatus)
	assert.Equal(t, "pending", row.ProcessStatus)
	assert.Equal(t, "active", row.ResolvedStatus)
	assert.Nil(t, row.ResolvedAt)
	assert.Nil(t, row.ProcessedBy)
	assert.Nil(t, row.ProcessedAt)
	assert.Nil(t, row.ProcessNote)
}

func TestIT_T028_ProcessAlert_Idempotent(t *testing.T) {
	ctx := context.Background()
	seedQAlerts(ctx, t, 1)
	r := NewAlertRepo(itPool)

	rows, _, err := r.ListAlerts(ctx, AlertQueryFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	alertID := rows[0].AlertID

	// 首次处理
	exists, err := r.ProcessAlert(ctx, alertID)
	require.NoError(t, err)
	assert.True(t, exists)
	var status string
	var processedAt *time.Time
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT process_status, processed_at FROM alerts WHERE alert_id = $1`, alertID).
		Scan(&status, &processedAt))
	assert.Equal(t, "processed", status)
	require.NotNil(t, processedAt)
	firstAt := *processedAt

	// 重复处理幂等：exists=true 且 processed_at 不重写
	time.Sleep(10 * time.Millisecond)
	exists, err = r.ProcessAlert(ctx, alertID)
	require.NoError(t, err)
	assert.True(t, exists, "重复处理不报错")
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT processed_at FROM alerts WHERE alert_id = $1`, alertID).Scan(&processedAt))
	assert.True(t, processedAt.Equal(firstAt), "幂等：处理时间不被重写")

	// 筛选联动：processed 可见
	_, total, err := r.ListAlerts(ctx, AlertQueryFilter{Status: "processed"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// 不存在的告警
	exists, err = r.ProcessAlert(ctx, 99999999)
	require.NoError(t, err)
	assert.False(t, exists, "不存在返回 exists=false（handler 映射 404）")
}
