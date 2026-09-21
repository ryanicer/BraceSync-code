// Package service T299「一患者一设备」绑定写入口校验（正例 + 负例）
package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

// 负例：患者已绑定 D1，再绑 D2 → 409/20409，且 D1 的绑定不被静默夺走
func TestBind_PatientHasOtherDevice_Rejected(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T299-A", "P-T299-1")
	registerAndPatient(t, svc, store, "DEV-T299-B", "P-T299-1")
	ctx := context.Background()

	_, appErr := svc.Bind(ctx, "DEV-T299-A", "P-T299-1", "TECH-1", false)
	require.Nil(t, appErr)

	_, appErr = svc.Bind(ctx, "DEV-T299-B", "P-T299-1", "TECH-1", false)
	require.NotNil(t, appErr, "患者已有生效设备时再绑必须被拒")
	assert.Equal(t, model.CodeConflict, appErr.Code)
	assert.Equal(t, 409, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, "DEV-T299-A", "文案须点明占位设备，便于技师先解绑")

	// 被拒后现场不变：A 仍绑该患者，B 仍未绑定
	devA, appErr := svc.GetDevice(ctx, "DEV-T299-A")
	require.Nil(t, appErr)
	require.NotNil(t, devA.PatientID)
	assert.Equal(t, "P-T299-1", *devA.PatientID, "拒绝不得动原绑定设备")
	devB, appErr := svc.GetDevice(ctx, "DEV-T299-B")
	require.Nil(t, appErr)
	assert.Nil(t, devB.PatientID, "被拒设备不得写入患者")
}

// 正例：先解绑旧设备，再绑新设备 → 成功（规则只拒「同时持有两台」，不拒换机流程）
func TestBind_PatientFreeAfterUnbind_Allowed(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T299-C", "P-T299-2")
	registerAndPatient(t, svc, store, "DEV-T299-D", "P-T299-2")
	ctx := context.Background()

	_, appErr := svc.Bind(ctx, "DEV-T299-C", "P-T299-2", "TECH-1", false)
	require.Nil(t, appErr)

	_, appErr = svc.Unbind(ctx, "DEV-T299-C", "TECH-1")
	require.Nil(t, appErr)

	_, appErr = svc.Bind(ctx, "DEV-T299-D", "P-T299-2", "TECH-1", false)
	require.Nil(t, appErr, "解绑旧设备后新绑必须放行")
	devD, appErr := svc.GetDevice(ctx, "DEV-T299-D")
	require.Nil(t, appErr)
	require.NotNil(t, devD.PatientID)
	assert.Equal(t, "P-T299-2", *devD.PatientID)
}

// 正例：同一设备重复绑定同一患者仍幂等（患者占用者就是本设备，不构成冲突）
func TestBind_SameDeviceSamePatient_IdempotentUnderRule(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T299-E", "P-T299-3")
	ctx := context.Background()

	_, appErr := svc.Bind(ctx, "DEV-T299-E", "P-T299-3", "TECH-1", false)
	require.Nil(t, appErr)
	_, appErr = svc.Bind(ctx, "DEV-T299-E", "P-T299-3", "TECH-1", false)
	assert.Nil(t, appErr, "同设备同患者重复绑定不得被规则误拒")
}

// 负例：换绑目标患者已持有其它设备 → 409，旧绑定与新绑定均不变
func TestRebind_TargetPatientHasOtherDevice_Rejected(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T299-F", "P-T299-4")
	registerAndPatient(t, svc, store, "DEV-T299-G", "P-T299-5")
	ctx := context.Background()

	_, appErr := svc.Bind(ctx, "DEV-T299-F", "P-T299-4", "TECH-1", false)
	require.Nil(t, appErr)
	_, appErr = svc.Bind(ctx, "DEV-T299-G", "P-T299-5", "TECH-1", false)
	require.Nil(t, appErr)

	_, appErr = svc.Rebind(ctx, "DEV-T299-F", "P-T299-5", "TECH-1", false)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeConflict, appErr.Code)
	assert.Contains(t, appErr.Message, "DEV-T299-G")

	devF, appErr := svc.GetDevice(ctx, "DEV-T299-F")
	require.Nil(t, appErr)
	require.NotNil(t, devF.PatientID)
	assert.Equal(t, "P-T299-4", *devF.PatientID, "被拒换绑不得关闭原绑定")
	devG, appErr := svc.GetDevice(ctx, "DEV-T299-G")
	require.Nil(t, appErr)
	require.NotNil(t, devG.PatientID)
	assert.Equal(t, "P-T299-5", *devG.PatientID, "目标患者的原设备不得被静默解绑")

	// 原设备仍只有一条 active binding（历史未被半途改写）
	bindings, appErr := svc.ListBindings(ctx, "DEV-T299-F")
	require.Nil(t, appErr)
	active := 0
	for _, b := range bindings {
		if b.UnbindAt == nil {
			active++
		}
	}
	assert.Equal(t, 1, active)
	assert.Len(t, bindings, 1, "被拒的换绑不得留下新绑定行")
}

// 错误映射：占位设备未知（并发被唯一索引拦截）时文案省略设备号，仍为 409
func TestMapRepoErr_PatientHasDevice(t *testing.T) {
	appErr := mapRepoErr(&repo.ErrPatientHasDevice{PatientID: "P-1", OtherDeviceID: "DEV-1"},
		model.ErrNotFound("fallback"))
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeConflict, appErr.Code)
	assert.Contains(t, appErr.Message, "DEV-1")

	appErr = mapRepoErr(&repo.ErrPatientHasDevice{PatientID: "P-1"}, model.ErrNotFound("fallback"))
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeConflict, appErr.Code)
	assert.NotContains(t, appErr.Message, "DEV-")

	// 非该错误不误伤：仍走原有映射
	appErr = mapRepoErr(repo.ErrConflict, model.ErrNotFound("fallback"))
	assert.Equal(t, model.CodeConflict, appErr.Code)
	assert.Equal(t, "resource conflict", appErr.Message)

	// 其它唯一约束的 23505 不被当成患者占用（仍按系统错误 90001）
	other := fmt.Errorf("bind: update device: %w",
		&pgconn.PgError{Code: "23505", ConstraintName: "uk_bindings_active"})
	appErr = mapRepoErr(other, model.ErrNotFound("fallback"))
	assert.Equal(t, model.CodeInternal, appErr.Code)
}

// 409 须带机器可读的占用设备号（前端弹 PRD §7C.3 换绑确认框，不解析文案）
func TestMapRepoErr_PatientHasDevice_CarriesStructuredDeviceID(t *testing.T) {
	data, ok := mapRepoErr(&repo.ErrPatientHasDevice{PatientID: "P-1", OtherDeviceID: "DEV-1"},
		model.ErrNotFound("fallback")).Data.(map[string]any)
	require.True(t, ok, "占用设备号须在 data 里")
	assert.Equal(t, "DEV-1", data["occupiedDeviceId"])

	// 并发兜底路径占位设备未知：不放 data，避免前端拿到空串仍去弹框
	assert.Nil(t, mapRepoErr(&repo.ErrPatientHasDevice{PatientID: "P-1"},
		model.ErrNotFound("fallback")).Data)
}

// ─────────────────────────────────────────────────────────────
// T299 确认换绑通道（PM 2026-09-22 01:07 追加：意图显式，静默改绑仍禁止）
// ─────────────────────────────────────────────────────────────

// 正例：患者已占用 A，带 confirmSwap 绑 B → 成功，A 被显式解除且历史可追溯
func TestBind_ConfirmSwap_ReplacesPatientOtherDevice(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T299-H", "P-T299-6")
	registerAndPatient(t, svc, store, "DEV-T299-I", "P-T299-6")
	ctx := context.Background()

	_, appErr := svc.Bind(ctx, "DEV-T299-H", "P-T299-6", "TECH-1", false)
	require.Nil(t, appErr)

	res, appErr := svc.Bind(ctx, "DEV-T299-I", "P-T299-6", "TECH-1", true)
	require.Nil(t, appErr, "确认换绑必须放行")
	assert.True(t, res.Rebound, "患者级换绑须报 swapped=true")
	assert.Equal(t, "DEV-T299-H", res.PatientSwappedFrom, "须带回被解除的原设备号")

	devI, appErr := svc.GetDevice(ctx, "DEV-T299-I")
	require.Nil(t, appErr)
	require.NotNil(t, devI.PatientID)
	assert.Equal(t, "P-T299-6", *devI.PatientID)

	devH, appErr := svc.GetDevice(ctx, "DEV-T299-H")
	require.Nil(t, appErr)
	assert.Nil(t, devH.PatientID, "原设备归属须被清空")
	assert.Equal(t, model.StatusUnbound, devH.Status, "原设备回到未绑定态")

	bindings, appErr := svc.ListBindings(ctx, "DEV-T299-H")
	require.Nil(t, appErr)
	require.Len(t, bindings, 1, "换绑不得为原设备新增绑定行")
	assert.NotNil(t, bindings[0].UnbindAt, "原绑定须写解绑时间，历史可追溯")
	require.NotNil(t, bindings[0].Reason)
	assert.Equal(t, model.ReasonRebind, *bindings[0].Reason, "原绑定解除原因记 rebind")
}

// 正例：患者本就空闲时带 confirmSwap 不产生任何副作用（意图字段不得改语义）
func TestBind_ConfirmSwap_WhenPatientFree_NoSwapSideEffects(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T299-J", "P-T299-7")
	ctx := context.Background()

	res, appErr := svc.Bind(ctx, "DEV-T299-J", "P-T299-7", "TECH-1", true)
	require.Nil(t, appErr)
	assert.False(t, res.Rebound, "首绑不构成换绑")
	assert.Empty(t, res.PatientSwappedFrom)

	// 同设备同患者重复请求仍幂等，不因带意图而多关一次绑定
	res2, appErr := svc.Bind(ctx, "DEV-T299-J", "P-T299-7", "TECH-1", true)
	require.Nil(t, appErr)
	assert.False(t, res2.Rebound)
	bindings, appErr := svc.ListBindings(ctx, "DEV-T299-J")
	require.Nil(t, appErr)
	assert.Len(t, bindings, 1, "幂等重绑不得新增绑定行")
}

// 负例：不带意图仍被拒（与上面的正例成对，证明换绑必须有显式确认）
func TestBind_WithoutConfirmSwap_StillRejected_AfterSwapChannelAdded(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T299-K", "P-T299-8")
	registerAndPatient(t, svc, store, "DEV-T299-L", "P-T299-8")
	ctx := context.Background()

	_, appErr := svc.Bind(ctx, "DEV-T299-K", "P-T299-8", "TECH-1", false)
	require.Nil(t, appErr)

	_, appErr = svc.Bind(ctx, "DEV-T299-L", "P-T299-8", "TECH-1", false)
	require.NotNil(t, appErr, "缺省仍走拒绝路径，静默改绑不复活")
	assert.Equal(t, model.CodeConflict, appErr.Code)

	devK, appErr := svc.GetDevice(ctx, "DEV-T299-K")
	require.Nil(t, appErr)
	require.NotNil(t, devK.PatientID)
	assert.Equal(t, "P-T299-8", *devK.PatientID)
}

// 正例：rebind 写入口同样支持确认换绑
func TestRebind_ConfirmSwap_ReplacesPatientOtherDevice(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T299-M", "P-T299-9")
	registerAndPatient(t, svc, store, "DEV-T299-N", "P-T299-10")
	registerAndPatient(t, svc, store, "DEV-T299-O", "P-T299-10")
	ctx := context.Background()

	_, appErr := svc.Bind(ctx, "DEV-T299-M", "P-T299-9", "TECH-1", false)
	require.Nil(t, appErr)
	_, appErr = svc.Bind(ctx, "DEV-T299-N", "P-T299-10", "TECH-1", false)
	require.Nil(t, appErr)

	res, appErr := svc.Rebind(ctx, "DEV-T299-M", "P-T299-10", "TECH-1", true)
	require.Nil(t, appErr, "确认换绑放行")
	assert.True(t, res.Rebound)
	assert.Equal(t, "DEV-T299-N", res.PatientSwappedFrom)

	devM, appErr := svc.GetDevice(ctx, "DEV-T299-M")
	require.Nil(t, appErr)
	require.NotNil(t, devM.PatientID)
	assert.Equal(t, "P-T299-10", *devM.PatientID, "本机换到目标患者")

	devN, appErr := svc.GetDevice(ctx, "DEV-T299-N")
	require.Nil(t, appErr)
	assert.Nil(t, devN.PatientID, "目标患者原设备被解除")

	bindings, appErr := svc.ListBindings(ctx, "DEV-T299-M")
	require.Nil(t, appErr)
	assert.Len(t, bindings, 2, "本机旧绑定关闭 + 新绑定一条")
}
