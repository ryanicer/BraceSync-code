// Package repo T299 单元层：唯一冲突判据与错误文案（不依赖 DB）
package repo

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestIsUniqueViolation_OnlyActivePatientConstraint(t *testing.T) {
	// 目标约束：并发抢绑会被它拦下，必须翻译成患者占用冲突
	wrapped := fmt.Errorf("bind: update device: %w",
		&pgconn.PgError{Code: "23505", ConstraintName: ukActivePatient})
	assert.True(t, isUniqueViolation(wrapped, ukActivePatient))

	// 同码不同约束（如设备级 uk_bindings_active）不得误判
	other := &pgconn.PgError{Code: "23505", ConstraintName: "uk_bindings_active"}
	assert.False(t, isUniqueViolation(other, ukActivePatient))

	// 不同 SQLSTATE（外键 23503）不得误判
	fk := &pgconn.PgError{Code: "23503", ConstraintName: ukActivePatient}
	assert.False(t, isUniqueViolation(fk, ukActivePatient))

	// 非 PgError 一律不判为唯一冲突
	assert.False(t, isUniqueViolation(errors.New("boom"), ukActivePatient))
}

func TestErrPatientHasDevice_Message(t *testing.T) {
	known := &ErrPatientHasDevice{PatientID: "P-1", OtherDeviceID: "DEV-1"}
	assert.Contains(t, known.Error(), "DEV-1")

	// 并发路径（唯一索引拦截）拿不到占位设备，文案不得留空占位符
	unknown := &ErrPatientHasDevice{PatientID: "P-1"}
	assert.NotContains(t, unknown.Error(), "%")
	assert.Contains(t, unknown.Error(), "another device")
}
