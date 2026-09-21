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

	_, appErr := svc.Bind(ctx, "DEV-T299-A", "P-T299-1", "TECH-1")
	require.Nil(t, appErr)

	_, appErr = svc.Bind(ctx, "DEV-T299-B", "P-T299-1", "TECH-1")
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

	_, appErr := svc.Bind(ctx, "DEV-T299-C", "P-T299-2", "TECH-1")
	require.Nil(t, appErr)

	_, appErr = svc.Unbind(ctx, "DEV-T299-C", "TECH-1")
	require.Nil(t, appErr)

	_, appErr = svc.Bind(ctx, "DEV-T299-D", "P-T299-2", "TECH-1")
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

	_, appErr := svc.Bind(ctx, "DEV-T299-E", "P-T299-3", "TECH-1")
	require.Nil(t, appErr)
	_, appErr = svc.Bind(ctx, "DEV-T299-E", "P-T299-3", "TECH-1")
	assert.Nil(t, appErr, "同设备同患者重复绑定不得被规则误拒")
}

// 负例：换绑目标患者已持有其它设备 → 409，旧绑定与新绑定均不变
func TestRebind_TargetPatientHasOtherDevice_Rejected(t *testing.T) {
	svc, store := newTestSvc(t)
	registerAndPatient(t, svc, store, "DEV-T299-F", "P-T299-4")
	registerAndPatient(t, svc, store, "DEV-T299-G", "P-T299-5")
	ctx := context.Background()

	_, appErr := svc.Bind(ctx, "DEV-T299-F", "P-T299-4", "TECH-1")
	require.Nil(t, appErr)
	_, appErr = svc.Bind(ctx, "DEV-T299-G", "P-T299-5", "TECH-1")
	require.Nil(t, appErr)

	_, appErr = svc.Rebind(ctx, "DEV-T299-F", "P-T299-5", "TECH-1")
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
