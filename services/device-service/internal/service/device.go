// Package service device-service 业务逻辑层
//
// 职责：设备注册（密钥加密幂等）、绑定/换绑/解绑（互斥事务编排）、
// 状态机落库（上报/补传）、安装记录 + 校准基线闭环、WiFi 配置状态。
// 鉴权归 gateway（架构 §5.2 内部信任链），本层只做业务与数据校验。
package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

// secretBytes 出厂 device_secret 随机字节数（hex 编码后 64 字符，HMAC-SHA256 密钥）
const secretBytes = 32

// DeviceService 设备域业务服务
type DeviceService struct {
	store repo.Store
	enc   *crypto.Encryptor
	now   func() time.Time

	// T091 配网密钥重发间隔防护（内存态，单实例；重启后清空，仅作窗口防护非密钥失效）
	provisionMu       sync.Mutex
	lastProvision     map[string]time.Time // deviceID → 上次签发时间
	provisionInterval time.Duration        // 同设备最短重发间隔
}

// NewDeviceService 组装 DeviceService
func NewDeviceService(store repo.Store, enc *crypto.Encryptor) *DeviceService {
	return &DeviceService{
		store:             store,
		enc:               enc,
		now:               time.Now,
		lastProvision:     make(map[string]time.Time),
		provisionInterval: loadProvisionInterval(),
	}
}

// mapRepoErr repo 哨兵错误 → AppError（其余按系统错误 90001）
func mapRepoErr(err error, fallback *model.AppError) *model.AppError {
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return fallback
	case errors.Is(err, repo.ErrConflict):
		return model.ErrConflict("resource conflict")
	default:
		return model.ErrInternal("internal error: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────
// 设备注册
// ─────────────────────────────────────────────────────────────

// Register 设备出厂激活：生成 device_secret 加密落库；重复注册幂等返回既有记录。
// created=true 表示本次新建；密钥明文不出本函数（架构 §5.2，不返回明文）。
func (s *DeviceService) Register(ctx context.Context, deviceID, deviceModel string) (*model.Device, bool, *model.AppError) {
	if !model.ValidDeviceID(deviceID) {
		return nil, false, model.ErrInvalidParam("invalid device_id %q (4-48 chars, alnum/-/_)", deviceID)
	}
	if deviceModel == "" {
		deviceModel = model.DefaultModel
	}

	secret, err := crypto.RandomSecret(secretBytes)
	if err != nil {
		return nil, false, model.ErrInternal("generate device secret: %v", err)
	}
	// A-1（T091 补录）：防御性契约校验——device_secret 必须为 64 字符 hex，
	// 与固件 HKDF ikm 约定一致（64 ASCII 字节）。当前 RandomSecret 恒合规，
	// 此处防止未来 RandomSecret 改动静默破坏配网密钥对齐。
	if !model.ValidDeviceSecret(secret) {
		return nil, false, model.ErrInvalidParam("device_secret must be 64-char hex, got %q", secret)
	}
	encSecret, err := s.enc.Encrypt([]byte(secret))
	if err != nil {
		return nil, false, model.ErrInternal("encrypt device secret: %v", err)
	}

	dev := &model.Device{
		DeviceID:        deviceID,
		Model:           deviceModel,
		DeviceSecretEnc: encSecret,
		SecretVersion:   1, // 一期固定=1（架构 §4.3，预留双密钥轮换）
		Status:          model.StatusUnbound,
	}
	created, err := s.store.RegisterDevice(ctx, dev)
	if err != nil {
		return nil, false, model.ErrInternal("register device: %v", err)
	}

	stored, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return nil, false, mapRepoErr(err, model.ErrNotFound("device %q not found after register", deviceID))
	}
	return stored, created, nil
}

// GetDevice 设备详情
func (s *DeviceService) GetDevice(ctx context.Context, deviceID string) (*model.Device, *model.AppError) {
	dev, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return nil, mapRepoErr(err, model.ErrNotFound("device %q not registered", deviceID))
	}
	return dev, nil
}

// ListBindings 绑定历史（追溯：bind_at/unbind_at/reason/operator_id）
func (s *DeviceService) ListBindings(ctx context.Context, deviceID string) ([]model.Binding, *model.AppError) {
	if _, appErr := s.GetDevice(ctx, deviceID); appErr != nil {
		return nil, appErr
	}
	bindings, err := s.store.ListBindings(ctx, deviceID)
	if err != nil {
		return nil, model.ErrInternal("list bindings: %v", err)
	}
	return bindings, nil
}

// ─────────────────────────────────────────────────────────────
// 绑定 / 换绑 / 解绑（设备维度互斥：同设备仅一个 active binding）
// ─────────────────────────────────────────────────────────────

// BindResult 绑定结果
type BindResult struct {
	Device  *model.Device
	Rebound bool // true=换绑（旧 binding 已写 unbind_at+reason=rebind）
}

// Bind 绑定/自动换绑（对齐 Ella KNOWN_RED H3/H6 契约：第二绑成功即换绑）：
//   - 设备/患者存在性校验
//   - 已有同患者 active binding → 幂等成功
//   - 已有他患者 active binding → 自动换绑（旧行 unbind_at+reason=rebind，历史可追溯）
//   - 互斥不变式：同设备同一时刻仅一个 active binding（uk_bindings_active 兜底）
//   - bindings 写入与 devices.patient_id/status/bind_time 更新同一事务
func (s *DeviceService) Bind(ctx context.Context, deviceID, patientID, operatorID string) (*BindResult, *model.AppError) {
	if deviceID == "" || patientID == "" {
		return nil, model.ErrInvalidParam("device_id and patient_id are required")
	}
	if _, appErr := s.GetDevice(ctx, deviceID); appErr != nil {
		return nil, appErr
	}
	ok, err := s.store.PatientExists(ctx, patientID)
	if err != nil {
		return nil, model.ErrInternal("check patient: %v", err)
	}
	if !ok {
		return nil, model.ErrUserResNotFound("patient %q not found", patientID)
	}

	prev, err := s.store.Bind(ctx, repo.BindParams{
		DeviceID:   deviceID,
		PatientID:  patientID,
		OperatorID: operatorID,
	})
	if err != nil {
		return nil, mapRepoErr(err, model.ErrNotFound("device %q not registered", deviceID))
	}

	dev, appErr := s.GetDevice(ctx, deviceID)
	if appErr != nil {
		return nil, appErr
	}
	return &BindResult{Device: dev, Rebound: prev != nil}, nil
}

// Rebind 换绑（历史可追溯）：旧 binding 写 unbind_at+reason=rebind+operator，新 binding 写入，
// 与 devices 归属更新同一事务。设备无 active binding 时返回 409（应先走 Bind）。
func (s *DeviceService) Rebind(ctx context.Context, deviceID, patientID, operatorID string) (*BindResult, *model.AppError) {
	if deviceID == "" || patientID == "" {
		return nil, model.ErrInvalidParam("device_id and patient_id are required")
	}
	if _, appErr := s.GetDevice(ctx, deviceID); appErr != nil {
		return nil, appErr
	}
	ok, err := s.store.PatientExists(ctx, patientID)
	if err != nil {
		return nil, model.ErrInternal("check patient: %v", err)
	}
	if !ok {
		return nil, model.ErrUserResNotFound("patient %q not found", patientID)
	}

	prev, err := s.store.Rebind(ctx, repo.BindParams{
		DeviceID:   deviceID,
		PatientID:  patientID,
		OperatorID: operatorID,
	})
	if err != nil {
		if errors.Is(err, repo.ErrConflict) {
			return nil, model.ErrConflict("device %q has no active binding; use bind first", deviceID)
		}
		return nil, mapRepoErr(err, model.ErrNotFound("device %q not registered", deviceID))
	}

	dev, appErr := s.GetDevice(ctx, deviceID)
	if appErr != nil {
		return nil, appErr
	}
	return &BindResult{Device: dev, Rebound: prev != nil}, nil
}

// Unbind 解绑（幂等：重复解绑返回成功，alreadyUnbound=true 表示本就无有效绑定）
func (s *DeviceService) Unbind(ctx context.Context, deviceID, operatorID string) (alreadyUnbound bool, appErr *model.AppError) {
	if deviceID == "" {
		return false, model.ErrInvalidParam("device_id is required")
	}
	hadActive, err := s.store.Unbind(ctx, deviceID, operatorID)
	if err != nil {
		return false, mapRepoErr(err, model.ErrNotFound("device %q not registered", deviceID))
	}
	return !hadActive, nil
}

// ─────────────────────────────────────────────────────────────
// 状态机落库（上报/补传）
// ─────────────────────────────────────────────────────────────

// Touch 上报/补传事件：更新 last_report_at（单调推进）+ 状态（仅最新帧）+ updated_at。
// 状态推导：fault_code>0 → abnormal，否则 online（故障解除自动恢复，PRD §8.1）。
func (s *DeviceService) Touch(ctx context.Context, deviceID string, ts time.Time, faultCode int) *model.AppError {
	if deviceID == "" {
		return model.ErrInvalidParam("device_id is required")
	}
	if ts.IsZero() {
		ts = s.now()
	}
	status := model.NextStatusOnReport(model.ReportEvent{Ts: ts, FaultCode: faultCode})
	if err := s.store.Touch(ctx, deviceID, ts, status); err != nil {
		return mapRepoErr(err, model.ErrNotFound("device %q not registered", deviceID))
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// 安装记录 + 校准基线（技师端流程：bind → matrix → save-baseline → complete）
// ─────────────────────────────────────────────────────────────

// CreateInstallRequest 新建安装记录入参
type CreateInstallRequest struct {
	DeviceID      string
	PatientID     string
	TechID        string
	CalibrateTime time.Time // 零值=当前时刻
	Notes         *string
	SignatureURL  *string
}

// CreateInstall 新建安装记录：设备/患者/技师存在性 + 设备须已绑定该患者（防错装）
func (s *DeviceService) CreateInstall(ctx context.Context, req *CreateInstallRequest) (*model.InstallRecord, *model.AppError) {
	if req.DeviceID == "" || req.PatientID == "" || req.TechID == "" {
		return nil, model.ErrInvalidParam("device_id, patient_id and tech_id are required")
	}
	dev, appErr := s.GetDevice(ctx, req.DeviceID)
	if appErr != nil {
		return nil, appErr
	}
	if dev.PatientID == nil || *dev.PatientID != req.PatientID {
		return nil, model.ErrConflict("device %q is not bound to patient %q; bind first", req.DeviceID, req.PatientID)
	}
	ok, err := s.store.TechExists(ctx, req.TechID)
	if err != nil {
		return nil, model.ErrInternal("check technician: %v", err)
	}
	if !ok {
		return nil, model.ErrUserResNotFound("technician %q not found", req.TechID)
	}

	calibrateTime := req.CalibrateTime
	if calibrateTime.IsZero() {
		calibrateTime = s.now()
	}
	rec := &model.InstallRecord{
		DeviceID:      req.DeviceID,
		PatientID:     req.PatientID,
		TechID:        req.TechID,
		CalibrateTime: calibrateTime,
		Notes:         req.Notes,
		SignatureURL:  req.SignatureURL,
	}
	id, err := s.store.CreateInstall(ctx, rec)
	if err != nil {
		return nil, model.ErrInternal("create install: %v", err)
	}
	rec.InstallID = id

	stored, err := s.store.GetInstall(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err, model.ErrNotFound("install record %d not found", id))
	}
	return stored, nil
}

// SaveBaseline 校准基线落库：offset_values 长度必须=20（服务层校验 + DB CHECK 双保险），
// 一次安装至多一条基线（uk_install_baseline）
func (s *DeviceService) SaveBaseline(ctx context.Context, installID int64, offsetValues []float32, calibratorID string) (int64, *model.AppError) {
	if installID <= 0 {
		return 0, model.ErrInvalidParam("invalid install_id %d", installID)
	}
	if len(offsetValues) != model.PointCount {
		return 0, model.ErrInvalidParam("offset_values length must be %d, got %d", model.PointCount, len(offsetValues))
	}
	if calibratorID == "" {
		return 0, model.ErrInvalidParam("calibrator_id is required")
	}

	id, err := s.store.SaveBaseline(ctx, installID, offsetValues, calibratorID)
	if err != nil {
		return 0, mapRepoErr(err, model.ErrNotFound("install record %d not found", installID))
	}
	return id, nil
}

// UpdateInstallMeta 回填安装记录的 notes / signature_url（saveBaseline 携带元数据时使用）
func (s *DeviceService) UpdateInstallMeta(ctx context.Context, installID int64, notes, signatureURL *string) *model.AppError {
	if err := s.store.UpdateInstallMeta(ctx, installID, notes, signatureURL); err != nil {
		return mapRepoErr(err, model.ErrNotFound("install record %d not found", installID))
	}
	return nil
}

// SetWifiSSID 配网成功后维护 devices.wifi_ssid（架构 §2.3）
func (s *DeviceService) SetWifiSSID(ctx context.Context, deviceID, ssid string) *model.AppError {
	if ssid == "" || len(ssid) > 128 {
		return model.ErrInvalidParam("wifi ssid must be 1-128 chars")
	}
	if err := s.store.SetWifiSSID(ctx, deviceID, ssid); err != nil {
		return mapRepoErr(err, model.ErrNotFound("device %q not registered", deviceID))
	}
	return nil
}
