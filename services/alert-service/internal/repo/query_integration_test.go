//go:build integration
// +build integration

// Package repo 集成测试 — T028 公开查询/处理（真实 PG15）
//
// 装配复用 integration_test.go 的 TestMain/itPool/种子数据；本文件仅补 T028 用例：
//
//	分页 + patientId/type/status 筛选 + ts DESC 排序
//	process 幂等（重复处理不报错、不重写处理时间）+ 不存在返回 exists=false
//	T257 2.7：pending/processing/processed 三态流转 + in_progress_at 幂等不刷新
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
	assert.Nil(t, row.InProgressAt, "T257 2.7：历史行/未进入处理中 → in_progress_at 为 NULL")
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

	// 首次处理（T278-①：带备注）
	exists, err := r.ProcessAlert(ctx, alertID, "DOCTOR-IT", "已电话指导患者调整佩戴位置")
	require.NoError(t, err)
	assert.True(t, exists)
	var status string
	var processedAt *time.Time
	var processedBy *string
	var processNote *string
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT process_status, processed_at, processed_by, process_note FROM alerts WHERE alert_id = $1`, alertID).
		Scan(&status, &processedAt, &processedBy, &processNote))
	assert.Equal(t, "processed", status)
	require.NotNil(t, processedAt)
	require.NotNil(t, processedBy)
	assert.Equal(t, "DOCTOR-IT", *processedBy, "T257 2.7：processed_by 落操作人")
	require.NotNil(t, processNote, "T278-①：D4 处理备注必须落 process_note")
	assert.Equal(t, "已电话指导患者调整佩戴位置", *processNote)
	firstAt := *processedAt

	// 读侧闭环：ListAlerts 投影回读同一备注（验收口径「回读 processNote 非 null」）
	rows, _, err = r.ListAlerts(ctx, AlertQueryFilter{Status: "processed"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].ProcessNote)
	assert.Equal(t, "已电话指导患者调整佩戴位置", *rows[0].ProcessNote)

	// 重复处理幂等：exists=true 且 processed_at / processed_by / process_note 均不重写
	time.Sleep(10 * time.Millisecond)
	exists, err = r.ProcessAlert(ctx, alertID, "CS-OTHER", "第二个人补的备注")
	require.NoError(t, err)
	assert.True(t, exists, "重复处理不报错")
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT processed_at, processed_by, process_note FROM alerts WHERE alert_id = $1`, alertID).
		Scan(&processedAt, &processedBy, &processNote))
	assert.True(t, processedAt.Equal(firstAt), "幂等：处理时间不被重写")
	assert.Equal(t, "DOCTOR-IT", *processedBy, "幂等：首个操作人不被覆盖")
	assert.Equal(t, "已电话指导患者调整佩戴位置", *processNote, "幂等：首个备注不被覆盖（与 processed_by 同规则）")

	// 无备注调用（老行为 / 前端不带 body）：process_note 保持原值，不被清空
	exists, err = r.ProcessAlert(ctx, alertID, "", "")
	require.NoError(t, err)
	assert.True(t, exists)
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT process_note, processed_by FROM alerts WHERE alert_id = $1`, alertID).Scan(&processNote, &processedBy))
	require.NotNil(t, processNote, "空 note 不得清空已有备注")
	assert.Equal(t, "已电话指导患者调整佩戴位置", *processNote)
	assert.Equal(t, "DOCTOR-IT", *processedBy, "空 operator 同样不覆盖（T257 既有口径）")

	// 筛选联动：processed 可见
	_, total, err := r.ListAlerts(ctx, AlertQueryFilter{Status: "processed"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// 不存在的告警
	exists, err = r.ProcessAlert(ctx, 99999999, "DOCTOR-IT", "")
	require.NoError(t, err)
	assert.False(t, exists, "不存在返回 exists=false（handler 映射 404）")
}

// T257 2.7 三态：pending → processing → processed，以及跳级/幂等/409 判据
func TestIT_T257_StartProcessing_StateFlow(t *testing.T) {
	ctx := context.Background()
	seedQAlerts(ctx, t, 2)
	r := NewAlertRepo(itPool)

	rows, _, err := r.ListAlerts(ctx, AlertQueryFilter{Status: "pending"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 3)
	pendingID, otherID, keepID := rows[0].AlertID, rows[1].AlertID, rows[2].AlertID

	// ① pending → processing：写 in_progress_at
	st, err := r.StartProcessing(ctx, pendingID)
	require.NoError(t, err)
	assert.True(t, st.Exists)
	assert.Equal(t, "processing", st.Status)
	require.NotNil(t, st.InProgressAt, "进入处理中必须记起点")
	first := *st.InProgressAt

	// ② 幂等：再点一次仍是 processing，in_progress_at 不刷新（否则处理耗时被无限拉长）
	time.Sleep(10 * time.Millisecond)
	st2, err := r.StartProcessing(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, "processing", st2.Status)
	require.NotNil(t, st2.InProgressAt)
	assert.True(t, st2.InProgressAt.Equal(first), "幂等：耗时起点不被刷新")

	// ③ processing → processed（正常闭环）
	exists, err := r.ProcessAlert(ctx, pendingID, "DOCTOR-IT", "")
	require.NoError(t, err)
	assert.True(t, exists)
	// 已 processed 再 StartProcessing → 状态仍 processed（handler 据此回 409）
	st3, err := r.StartProcessing(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, "processed", st3.Status)

	// ④ 跳级：pending → processed 直接允许（设计稿允许不经过「处理中」），备注同样随体落库
	exists, err = r.ProcessAlert(ctx, otherID, "CS-IT", "跳级处理时补的备注")
	require.NoError(t, err)
	assert.True(t, exists)
	var status string
	var jumpNote *string
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT process_status, process_note FROM alerts WHERE alert_id = $1`, otherID).Scan(&status, &jumpNote))
	assert.Equal(t, "processed", status)
	require.NotNil(t, jumpNote, "T278-①：跳级路径也要落备注")
	assert.Equal(t, "跳级处理时补的备注", *jumpNote)

	// ⑤ 三态筛选互斥：keepID 停在 processing（另两条已 processed），pending 归零
	stKeep, err := r.StartProcessing(ctx, keepID)
	require.NoError(t, err)
	assert.Equal(t, "processing", stKeep.Status)
	for _, c := range []struct {
		status string
		want   int64
	}{
		{"processing", 1}, {"processed", 2}, {"pending", 0},
	} {
		_, total, err := r.ListAlerts(ctx, AlertQueryFilter{Status: c.status})
		require.NoError(t, err)
		assert.EqualValues(t, c.want, total, "status=%s", c.status)
	}

	// ⑥ 不存在的告警 → Exists=false（handler 映射 404）
	st4, err := r.StartProcessing(ctx, 99999999)
	require.NoError(t, err)
	assert.False(t, st4.Exists)
}
