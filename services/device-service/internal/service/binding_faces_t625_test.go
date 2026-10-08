// Package service — T625 类 6（绑定关系双账面）盲区用例：两面都在、两面都可读
//
// 本文件是**新增**测试文件，不改动 device_test.go / device_t299_test.go（复用其
// newTestSvc / registerAndPatient 夹具与 services/device-service/internal/testutil.FakeStore）。
//
// 背景（现读，2026-10-08 grep 核对）：
//   - patients.device_id      scripts/db/migrations/000001_init_schema.up.sql:77（可空列）
//     被 scripts/db/migrations/000011_patient_status_default.up.sql:10 的 COMMENT 判为
//     [DEPRECATED]「当前绑定以 devices.patient_id 为准，历史见 device_bindings；本列不再被读取」
//   - devices.patient_id      当前绑定冗余列，000012 给它加了部分唯一索引
//     scripts/db/migrations/000012_devices_patient_partial_unique.up.sql:17
//   - device_bindings         绑定历史/权威面，scripts/db/migrations/000002_p0_fixes.up.sql:11-21
//     （uk_bindings_active = 同设备仅一条 unbind_at IS NULL 的行）
//
// 按派发单口径：本轮**只钉「对账面存在且可读」**，不宣称哪一面是权威源
// （「谁是权威」是产品裁定项，PRD 未定稿前钉死任何一面都会把现状当期望）。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// t625ActiveBindings 取「当前有效」那一面（unbind_at IS NULL 的 device_bindings 行）
func t625ActiveBindings(bindings []model.Binding) []model.Binding {
	active := make([]model.Binding, 0, len(bindings))
	for _, b := range bindings {
		if b.UnbindAt == nil {
			active = append(active, b)
		}
	}
	return active
}

// ─────────────────────────────────────────────────────────────
// 1) 两面都存在且都可读，且绑定动作后同步指向同一患者
// ─────────────────────────────────────────────────────────────

func TestT625_BindingFaces_BothReadableAndMoveTogether(t *testing.T) {
	svc, store := newTestSvc(t)
	ctx := context.Background()
	deviceID, patientID := "DEV-T625-1", "P-T625-1"
	registerAndPatient(t, svc, store, deviceID, patientID)

	// 绑定前两面都是「无绑定」形态：devices.patient_id = NULL，有效绑定行 0 条
	dev, appErr := svc.GetDevice(ctx, deviceID)
	require.Nil(t, appErr)
	assert.Nil(t, dev.PatientID, "面一：devices.patient_id 未绑定应为 NULL")
	before, appErr := svc.ListBindings(ctx, deviceID)
	require.Nil(t, appErr)
	assert.Empty(t, t625ActiveBindings(before), "面二：device_bindings 未绑定应无 active 行")

	_, appErr = svc.Bind(ctx, deviceID, patientID, "TECH-T625", false)
	require.Nil(t, appErr)

	// 面一（devices.patient_id）可读
	dev, appErr = svc.GetDevice(ctx, deviceID)
	require.Nil(t, appErr)
	require.NotNil(t, dev.PatientID, "绑定后 devices.patient_id 必须可读")
	assert.Equal(t, patientID, *dev.PatientID)

	// 面二（device_bindings active 行）同样可读
	after, appErr := svc.ListBindings(ctx, deviceID)
	require.Nil(t, appErr)
	require.NotEmpty(t, after, "绑定历史面必须可查")
	active := t625ActiveBindings(after)
	require.Len(t, active, 1, "同一设备同时只应有一条 active 绑定（uk_bindings_active，000002:21）")
	assert.Equal(t, patientID, active[0].PatientID)
	assert.Equal(t, deviceID, active[0].DeviceID)

	// 两面此刻指向同一患者：这是「对账」能成立的最小形状，不是权威源判定
	assert.Equal(t, *dev.PatientID, active[0].PatientID, "两面读数应可比对（对账的前提是两面都在）")

	// 解绑后两面一起翻面：devices.patient_id 回 NULL，active 行归零（历史行仍留在面二）
	_, appErr = svc.Unbind(ctx, deviceID, "TECH-T625")
	require.Nil(t, appErr)

	dev, appErr = svc.GetDevice(ctx, deviceID)
	require.Nil(t, appErr)
	assert.Nil(t, dev.PatientID, "解绑后 devices.patient_id 必须释放")
	history, appErr := svc.ListBindings(ctx, deviceID)
	require.Nil(t, appErr)
	assert.Empty(t, t625ActiveBindings(history), "解绑后不得残留 active 行")
	assert.Len(t, history, 1, "解绑不删历史：面二仍留那一行（unbind_at 已置）")
}

// ─────────────────────────────────────────────────────────────
// 2) 换绑场景：面二保留旧行，面一只留最新 —— 两面各自形状可读、可追溯
// ─────────────────────────────────────────────────────────────

func TestT625_BindingFaces_RebindKeepsHistoryFaceOnlyInBindings(t *testing.T) {
	svc, store := newTestSvc(t)
	ctx := context.Background()
	deviceID := "DEV-T625-2"
	registerAndPatient(t, svc, store, deviceID, "P-T625-A")
	store.AddPatient("P-T625-B")

	_, appErr := svc.Bind(ctx, deviceID, "P-T625-A", "TECH-T625", false)
	require.Nil(t, appErr)

	// 自动换绑（同设备第二绑）：面一只剩 B，面二两行（A 关闭 + B 有效）
	res, appErr := svc.Rebind(ctx, deviceID, "P-T625-B", "TECH-T625", false)
	require.Nil(t, appErr)
	require.NotNil(t, res)

	dev, appErr := svc.GetDevice(ctx, deviceID)
	require.Nil(t, appErr)
	require.NotNil(t, dev.PatientID)
	assert.Equal(t, "P-T625-B", *dev.PatientID, "面一：当前绑定只剩最新患者")

	bindings, appErr := svc.ListBindings(ctx, deviceID)
	require.Nil(t, appErr)
	require.Len(t, bindings, 2, "面二：两次绑定都留痕")

	// 按「是否已关闭」而非数组下标取行：FakeStore 按写入顺序返回，PG 侧 SQL 是
	// ORDER BY bind_at DESC（repo/repo.go:465），两侧顺序相反 ⇒ 位置断言会随层漂移
	active := t625ActiveBindings(bindings)
	require.Len(t, active, 1)
	assert.Equal(t, "P-T625-B", active[0].PatientID)
	assert.Equal(t, model.ReasonRebind, *active[0].Reason)

	closed := make([]model.Binding, 0, len(bindings))
	for _, b := range bindings {
		if b.UnbindAt != nil {
			closed = append(closed, b)
		}
	}
	require.Len(t, closed, 1, "旧行必须被关闭时间戳，否则面二无法区分当前/历史")
	assert.Equal(t, "P-T625-A", closed[0].PatientID, "旧行留在面二：历史可追溯")
	assert.Equal(t, model.ReasonRebind, *closed[0].Reason, "旧行 reason 翻为 rebind（与 PG 侧 UPDATE 同语义，repo.go:325）")
}

// ─────────────────────────────────────────────────────────────
// 3) 第三面（patients.device_id 原列值）不可读 ⇒ 三账对不上，pending
// ─────────────────────────────────────────────────────────────

// 缺失的 API（现读确认「一处都没有」）：
//   - services/user-service/internal/repo/pg.go:443 注释即「patients.device_id 已废弃、不再读取」，
//     :452 的 SELECT 取的是 dev.device_id（devices LEFT JOIN，源在 device-service 那一面）；
//     services/user-service/internal/repo/store.go:125 同样把 PatientRow.DeviceID 说明为「来自 devices」。
//   - ⇒ 全仓库没有任何只读口能拿到 patients.device_id 的**列原值**，
//     也就没有任何口能同时返回「列原值 vs devices.patient_id vs device_bindings active 行」这三面。
//
// 需要落地的口子（二选一，随 T625 类 6 的新卡定）：
//
//	A. 只读对账查询/内部端点：GET /internal/patients/:patientId/binding-reconciliation
//	   返回 {patients_device_id, devices_patient_id, active_binding}；
//	B. 或在 user-service repo 增一个把 patients.device_id 原列一并投影出来的只读方法，
//	   并配一条「三面对不上」的扫描 SQL（不改数据，只出报告）。
//
// 在产品裁定「谁是权威」之前，本用例只钉到「两面可读」（见上面两条），
// 第三面一旦可读，就要立刻能比对 —— 下面这段就是那条比对，先留在 skip 后面。
func TestT625_BindingFaces_ThirdFaceMustBeReadableForReconciliation(t *testing.T) {
	reason := "T625 类 6 新卡 未合入：patients.device_id 列原值无任何可读 API" +
		"（需新增只读对账口，见本用例注释里的 A/B 两案）；合入后去掉本行即转绿"
	t.Skip(reason)

	// 期望形状：同一个患者能一次拿到三面读数并逐面比对（此处按新 API 落地后补断言）
	svc, store := newTestSvc(t)
	ctx := context.Background()
	registerAndPatient(t, svc, store, "DEV-T625-3", "P-T625-3")
	_, appErr := svc.Bind(ctx, "DEV-T625-3", "P-T625-3", "TECH-T625", false)
	require.Nil(t, appErr)
	assert.NotNil(t, svc)
	assert.NotNil(t, store)
}
