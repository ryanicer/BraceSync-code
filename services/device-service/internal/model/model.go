// Package model device-service 领域模型与 API DTO 定义
//
// 对齐：docs/ §3.5（错误码分段/统一响应体）/ §4.3（devices/device_bindings/install_records/baselines）/ §4.6（状态机）
//
//	docs/ / bindDevice / saveBaseline / getProvisionKey）
//	PRD §8.1（设备状态机）· §7C（技师安装流程）
package model

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

// PointCount 校准基线点位数（对齐 baselines.offset_values CHECK 长度=20）
const PointCount = 20

// DefaultModel 默认设备型号（devices.model DEFAULT）
const DefaultModel = "PRS-ML05-RC"

// ─────────────────────────────────────────────────────────────
// 设备状态机（架构 §4.6 + PRD §8.1）
// ─────────────────────────────────────────────────────────────

// 设备状态枚举（devices.status CHECK 约束一致）
const (
	StatusOnline   = "online"
	StatusOffline  = "offline"
	StatusAbnormal = "abnormal"
	StatusUnbound  = "unbound"
)

// ReportEvent 上报/补传事件（状态机推导输入）
type ReportEvent struct {
	Ts        time.Time // 帧采集时刻（单调推进 last_report_at 的依据）
	FaultCode int       // 故障码，0=正常
}

// NextStatusOnReport 上报事件的状态推导：故障帧 → abnormal，正常帧 → online（故障解除自动恢复）。
// abnormal > offline 优先级（PRD §8.1）由"最新帧口径"天然满足：本函数只依据最新帧判定。
func NextStatusOnReport(ev ReportEvent) string {
	if ev.FaultCode > 0 {
		return StatusAbnormal
	}
	return StatusOnline
}

// NextStatusOnBind 绑定后的状态推导：unbound → offline（已绑定未上报），其余状态保持（换绑不清空在线态）
func NextStatusOnBind(current string) string {
	if current == StatusUnbound {
		return StatusOffline
	}
	return current
}

// deviceIDPattern device_id 格式：字母数字开头，允许 -/_，长度 4–48（VARCHAR(48)）
var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{3,47}$`)

// ValidDeviceID 校验 device_id 格式
func ValidDeviceID(id string) bool { return deviceIDPattern.MatchString(id) }

// ValidDeviceSecret 校验 device_secret 格式：必须为 64 字符 hex 字符串。
// 固件约定（docs/design/hardware/BLE配网协议确认-小顾-2026-09-05.md §3）：
// HKDF 的 ikm 取 device_secret 的 64 字符 hex ASCII 字节（64B），而非 hex 解码后的 32B。
// 注册/派生两端均须保证 secret 为 64-hex，否则与固件静默派生出不同密钥。
func ValidDeviceSecret(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// ─────────────────────────────────────────────────────────────
// 错误码（架构 §3.5：2xxxx 设备域；1xxxx 用户域；9xxxx 系统级）
// ─────────────────────────────────────────────────────────────

const (
	CodeOK              = 0
	CodeInvalidParam    = 20400 // 参数非法（device_id 格式 / offset_values 长度 / installId 解析）
	CodeNotFound        = 20404 // 设备域资源不存在（device / install_record）
	CodeConflict        = 20409 // 状态冲突（绑定互斥 / 基线已存在 / 安装与绑定不一致）
	CodeTooMany         = 20429 // 请求过频（T091 配网密钥重发间隔内重复领取）
	CodeUserResNotFound = 10404 // 用户域资源不存在（patient / technician，owner: user-service）
	CodeInternal        = 90001 // 系统内部错误
)

// AppError 业务错误：携带统一响应 code 与建议 HTTP 状态
type AppError struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *AppError) Error() string { return fmt.Sprintf("code=%d: %s", e.Code, e.Message) }

func newAppError(code, httpStatus int, format string, args ...any) *AppError {
	return &AppError{Code: code, HTTPStatus: httpStatus, Message: fmt.Sprintf(format, args...)}
}

func ErrInvalidParam(format string, args ...any) *AppError {
	return newAppError(CodeInvalidParam, 400, format, args...)
}

func ErrNotFound(format string, args ...any) *AppError {
	return newAppError(CodeNotFound, 404, format, args...)
}

func ErrConflict(format string, args ...any) *AppError {
	return newAppError(CodeConflict, 409, format, args...)
}

// ErrTooMany T091：配网密钥重发间隔内重复领取 → HTTP 429
func ErrTooMany(format string, args ...any) *AppError {
	return newAppError(CodeTooMany, 429, format, args...)
}

func ErrUserResNotFound(format string, args ...any) *AppError {
	return newAppError(CodeUserResNotFound, 404, format, args...)
}

func ErrInternal(format string, args ...any) *AppError {
	return newAppError(CodeInternal, 500, format, args...)
}

// ─────────────────────────────────────────────────────────────
// 领域实体（对齐 database-design / 000001+000002 migration）
// ─────────────────────────────────────────────────────────────

// Device devices 表行（device_secret_enc 密文不出 service 层）
type Device struct {
	DeviceID        string
	Model           string
	FirmwareVersion string
	DeviceSecretEnc []byte // AES-GCM 密文（nonce||ciphertext），禁止对外返回
	SecretVersion   int
	PatientID       *string // NULL=未绑定
	WifiSSID        *string
	BindTime        *time.Time
	Status          string
	LastReportAt    *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Binding device_bindings 表行（当前绑定权威源，架构 §4.3）
type Binding struct {
	BindingID  int64
	DeviceID   string
	PatientID  string
	BindAt     time.Time
	UnbindAt   *time.Time // NULL=当前有效
	Reason     *string    // install / rebind / unbind
	OperatorID *string
}

// 绑定变更原因（device_bindings.reason 取值）
const (
	ReasonInstall = "install" // 首次绑定（安装流程）
	ReasonRebind  = "rebind"  // 换绑
	ReasonUnbind  = "unbind"  // 解绑
)

// InstallRecord install_records 表行
type InstallRecord struct {
	InstallID     int64
	DeviceID      string
	PatientID     string
	TechID        string
	CalibrateTime time.Time
	BaselineID    *int64 // 校准完成后回填（P0-3 UNIQUE，1:1）
	Notes         *string
	SignatureURL  *string
	WifiStatus    string // connected / unconfigured
	CreatedAt     time.Time
}

// Baseline baselines 表行
type Baseline struct {
	BaselineID   int64
	InstallID    int64
	DeviceID     string
	OffsetValues []float32 // 定长 20（CHECK 约束兜底）
	CalibratorID string
	CreatedAt    time.Time
}

// ─────────────────────────────────────────────────────────────
// API DTO（camelCase，对齐 shared-types Device / api-contracts.ts）
// ─────────────────────────────────────────────────────────────

// DeviceDTO 对齐 shared-types Device（不含任何密钥材料）
type DeviceDTO struct {
	DeviceID        string  `json:"deviceId"`
	Model           string  `json:"model"`
	FirmwareVersion string  `json:"firmwareVersion"`
	PatientID       *string `json:"patientId"`
	WifiSsid        *string `json:"wifiSsid"`
	BindTime        *string `json:"bindTime"`
	Status          string  `json:"status"`
	LastReportAt    *string `json:"lastReportAt"`
}

func fmtTs(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// ToDTO 领域实体 → 前端 DTO（ISO 8601 UTC，架构 §3.5）
func (d *Device) ToDTO() DeviceDTO {
	return DeviceDTO{
		DeviceID:        d.DeviceID,
		Model:           d.Model,
		FirmwareVersion: d.FirmwareVersion,
		PatientID:       d.PatientID,
		WifiSsid:        d.WifiSSID,
		BindTime:        fmtTs(d.BindTime),
		Status:          d.Status,
		LastReportAt:    fmtTs(d.LastReportAt),
	}
}

// BindingDTO 绑定历史条目（运营后台追溯用）
type BindingDTO struct {
	BindingID  string  `json:"bindingId"`
	DeviceID   string  `json:"deviceId"`
	PatientID  string  `json:"patientId"`
	BindAt     string  `json:"bindAt"`
	UnbindAt   *string `json:"unbindAt"`
	Reason     *string `json:"reason"`
	OperatorID *string `json:"operatorId"`
}

// ToDTO Binding → DTO
func (b *Binding) ToDTO() BindingDTO {
	return BindingDTO{
		BindingID:  fmt.Sprintf("%d", b.BindingID),
		DeviceID:   b.DeviceID,
		PatientID:  b.PatientID,
		BindAt:     b.BindAt.UTC().Format(time.RFC3339),
		UnbindAt:   fmtTs(b.UnbindAt),
		Reason:     b.Reason,
		OperatorID: b.OperatorID,
	}
}
