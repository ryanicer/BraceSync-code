// Package model 单元测试：状态机推导与 device_id 校验
package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextStatusOnReport(t *testing.T) {
	cases := []struct {
		name string
		ev   ReportEvent
		want string
	}{
		{"正常帧→online", ReportEvent{Ts: time.Now(), FaultCode: 0}, StatusOnline},
		{"故障帧→abnormal", ReportEvent{Ts: time.Now(), FaultCode: 3}, StatusAbnormal},
		{"故障码负值视为正常", ReportEvent{Ts: time.Now(), FaultCode: -1}, StatusOnline},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, NextStatusOnReport(c.ev), c.name)
	}
}

func TestNextStatusOnBind(t *testing.T) {
	assert.Equal(t, StatusOffline, NextStatusOnBind(StatusUnbound), "unbound 绑定后→offline（未上报）")
	assert.Equal(t, StatusOnline, NextStatusOnBind(StatusOnline), "换绑不清空在线态")
	assert.Equal(t, StatusAbnormal, NextStatusOnBind(StatusAbnormal), "换绑不清空异常态")
	assert.Equal(t, StatusOffline, NextStatusOnBind(StatusOffline))
}

func TestValidDeviceID(t *testing.T) {
	for _, ok := range []string{"DEV-001", "dev_sim_02", "AB12", "PRS-ML05-RC-2026-0001"} {
		assert.True(t, ValidDeviceID(ok), "%q should be valid", ok)
	}
	for _, bad := range []string{"", "AB1", "-DEV1", "设备一", "DEV 001", "a-very-long-device-id-over-forty-eight-characters-limit-x"} {
		assert.False(t, ValidDeviceID(bad), "%q should be invalid", bad)
	}
}

func TestDeviceToDTO_NoSecretLeak(t *testing.T) {
	dev := &Device{
		DeviceID:        "DEV-001",
		Model:           DefaultModel,
		DeviceSecretEnc: []byte{0xde, 0xad, 0xbe, 0xef}, // 密文材料
		SecretVersion:   1,
		Status:          StatusUnbound,
	}
	dto := dev.ToDTO()
	assert.Equal(t, "DEV-001", dto.DeviceID)
	assert.Equal(t, StatusUnbound, dto.Status)
	assert.Nil(t, dto.PatientID)
	assert.Nil(t, dto.BindTime)
}

func TestDeviceToDTO_WithTimes(t *testing.T) {
	now := time.Now()
	patient, ssid := "P-001", "Home-WiFi"
	dev := &Device{DeviceID: "DEV-002", Model: DefaultModel, Status: StatusOnline,
		PatientID: &patient, WifiSSID: &ssid, BindTime: &now, LastReportAt: &now}
	dto := dev.ToDTO()
	require.NotNil(t, dto.BindTime)
	require.NotNil(t, dto.LastReportAt)
	assert.Contains(t, *dto.BindTime, "T", "ISO 8601 格式")
}

func TestBindingToDTO(t *testing.T) {
	now := time.Now()
	reason := ReasonInstall
	operator := "TECH-1"
	b := &Binding{BindingID: 7, DeviceID: "DEV-001", PatientID: "P-001",
		BindAt: now, Reason: &reason, OperatorID: &operator}
	dto := b.ToDTO()
	assert.Equal(t, "7", dto.BindingID)
	assert.Equal(t, ReasonInstall, *dto.Reason)
	assert.Nil(t, dto.UnbindAt)

	b.UnbindAt = &now
	assert.NotNil(t, b.ToDTO().UnbindAt)
}

func TestAppErrorConstructors(t *testing.T) {
	e := ErrInvalidParam("bad %d", 1)
	assert.Equal(t, CodeInvalidParam, e.Code)
	assert.Equal(t, 400, e.HTTPStatus)
	assert.Equal(t, "code=20400: bad 1", e.Error())

	assert.Equal(t, CodeNotFound, ErrNotFound("x").Code)
	assert.Equal(t, 404, ErrNotFound("x").HTTPStatus)
	assert.Equal(t, CodeConflict, ErrConflict("x").Code)
	assert.Equal(t, 409, ErrConflict("x").HTTPStatus)
	assert.Equal(t, CodeUserResNotFound, ErrUserResNotFound("x").Code)
	assert.Equal(t, CodeInternal, ErrInternal("x").Code)
	assert.Equal(t, 500, ErrInternal("x").HTTPStatus)
}
