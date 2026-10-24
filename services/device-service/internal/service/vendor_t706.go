// T706 厂商凭据体系业务层。
//
// 三条硬口径（设计稿 §4/§6）：
//
//	① 跨厂商读一律 404 device_not_found，与「设备不存在」逐字同形（P-1 同形原则）；
//	② 批量注册整请求判大小，逐条三态 created/existed/failed —— 重复 deviceId 不报 409、
//	  不重发密钥（产线断点续传/整批重放的常态）；
//	③ 审计是披露的前置条件：注册三态计数与每次补取 secret 都必须落 audit_logs，
//	  写不进审计就不发包（与 T728 密钥下载同一条纪律）。
//
// 明文面：device secret 与厂商 key 的明文只出现在返回值里，不落日志、不落审计 detail、
// 不进 DTO。厂商 key 库内只有 HMAC-SHA256(pepper, 全串)，无还原通路。
package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

// VendorError 厂商面错误：HTTP 状态 + 机器可读 slug（报文关键字，矩阵按 slug 判真）。
// 与设备域 AppError 分开：厂商面错误码自成一套（设计稿 §1.3「越权语义自成一套」）。
type VendorError struct {
	Status  int
	Slug    string
	Message string
}

func (e *VendorError) Error() string { return e.Slug + ": " + e.Message }

func vendorErr(status int, slug, format string, args ...any) *VendorError {
	return &VendorError{Status: status, Slug: slug, Message: fmt.Sprintf(format, args...)}
}

// 厂商面 slug（测试与卡面判据按这些字面值断言）
const (
	VendorSlugDeviceNotFound     = "device_not_found"
	VendorSlugReasonRequired     = "vendor_reason_required"
	VendorSlugBatchTooLarge      = "vendor_batch_too_large"
	VendorSlugBatchEmpty         = "vendor_batch_empty"
	VendorSlugModelNotAuthorized = "model_not_authorized"
	VendorSlugDeviceIDInvalid    = "device_id_invalid"
	VendorSlugDeviceIDTaken      = "device_id_taken"
	VendorSlugSecretUnusable     = "vendor_secret_unusable"
	VendorSlugSecretFetchLimit   = "vendor_secret_fetch_limit"
	VendorSlugVendorNotFound     = "vendor_not_found"
	VendorSlugVendorSuspended    = "vendor_suspended"
	VendorSlugKeyLimit           = "vendor_key_limit"
	VendorSlugKeyNotFound        = "vendor_key_not_found"
	VendorSlugPepperMissing      = "vendor_pepper_missing"
	VendorSlugInvalidParam       = "invalid_param"
	VendorSlugStoreUnavailable   = "vendor_store_unavailable"
)

// 批量注册逐条状态
const (
	VendorItemCreated = "created"
	VendorItemExisted = "existed"
	VendorItemFailed  = "failed"
)

// 审计动词（§5.2）与对象类型
const (
	VendorActionRegister   = "device_register"
	VendorActionSecretRead = "device_secret_fetch"
	VendorActionKeyIssue   = "vendor_key_issue"
	VendorActionKeyRevoke  = "vendor_key_revoke"
	VendorRole             = "VENDOR"
	VendorTargetDevice     = "device"
	VendorTargetKey        = "vendor_api_key"
)

// vendorDeviceIDPattern 设计稿 §6.1：型号-变体-YYYYMMDD 8 位日期+3 位流水。
// 实测样本 PRS-ML05-RC-20261020001 / PRS-ML05-RC-20261017001。
var (
	vendorDeviceIDPattern = regexp.MustCompile(`^PRS-[A-Z0-9]{2,8}-[A-Z]{2}-\d{11}$`)
	vendorModelPattern    = regexp.MustCompile(`^[A-Za-z0-9-]{2,32}$`)
)

// VendorRegisterItem 批量注册单条入参
type VendorRegisterItem struct {
	DeviceID string `json:"deviceId"`
	Firmware string `json:"firmware"`
}

// VendorRegisterItemResult 批量注册单条结果。Secret 只在 created 且本次新建出现。
type VendorRegisterItemResult struct {
	DeviceID string `json:"deviceId"`
	Status   string `json:"status"`
	Code     string `json:"code,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Secret   string `json:"secret"`
}

// VendorBatchSummary 批内三态计数（审计 detail 与响应体同源）
type VendorBatchSummary struct {
	Requested int `json:"requested"`
	Created   int `json:"created"`
	Existed   int `json:"existed"`
	Failed    int `json:"failed"`
}

// VendorBatchResult 批量注册响应
type VendorBatchResult struct {
	Model   string                     `json:"model"`
	Results []VendorRegisterItemResult `json:"results"`
	Summary VendorBatchSummary         `json:"summary"`
}

// VendorDeviceView 厂商面设备元数据（含患者标识即违约，见设计稿 §4 第 3 行）
type VendorDeviceView struct {
	DeviceID        string `json:"deviceId"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmwareVersion"`
	Status          string `json:"status"`
	Bound           bool   `json:"bound"`
	RegisteredAt    string `json:"registeredAt"`
}

// VendorIssuedKey 签发结果（明文 key 仅此一次）
type VendorIssuedKey struct {
	KeyID     string `json:"keyId"`
	VendorID  string `json:"vendorId"`
	KeyPrefix string `json:"keyPrefix"`
	KeyTail   string `json:"keyTail"`
	Key       string `json:"key"`
	ExpiresAt string `json:"expiresAt"`
}

// VendorKeyView 管理面 key 列表项（不含 hash、不含明文）
type VendorKeyView struct {
	KeyID     string  `json:"keyId"`
	VendorID  string  `json:"vendorId"`
	KeyPrefix string  `json:"keyPrefix"`
	KeyTail   string  `json:"keyTail"`
	Status    string  `json:"status"`
	IssuedAt  string  `json:"issuedAt"`
	ExpiresAt string  `json:"expiresAt"`
	RevokedAt *string `json:"revokedAt"`
}

// SetVendorStore 注入厂商面数据访问与 pepper（main 启动时调用；未注入时厂商端点回 500）。
// pepper 为空 = 签发与验签一律 fail-closed（设计稿 §2「缺 pepper 时签发端点直接 500 拒绝而非降级」）。
func (s *DeviceService) SetVendorStore(store repo.VendorStore, pepper string) {
	s.vendors = store
	s.vendorPepper = pepper
}

func (s *DeviceService) vendorStoreReady() *VendorError {
	if s.vendors == nil {
		return vendorErr(500, VendorSlugStoreUnavailable, "vendor store not configured")
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// 批量注册（§6.1）
// ─────────────────────────────────────────────────────────────

// VendorBatchRegister 厂商批量注册：≤N 台整请求判、逐条三态、existed 不重发密钥。
// vendor 由网关鉴权注入（服务端强制归属，请求体里的 vendor_id 一律忽略）。
func (s *DeviceService) VendorBatchRegister(ctx context.Context, vendor *repo.Vendor, keyPrefix, ip string,
	deviceModel string, items []VendorRegisterItem) (*VendorBatchResult, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return nil, err
	}
	if vendor.Status != "active" {
		return nil, vendorErr(403, VendorSlugVendorSuspended, "vendor %s is %s", vendor.VendorID, vendor.Status)
	}
	deviceModel = strings.TrimSpace(deviceModel)
	if deviceModel == "" {
		return nil, vendorErr(400, VendorSlugInvalidParam, "model is required")
	}
	if len(items) == 0 {
		return nil, vendorErr(400, VendorSlugBatchEmpty, "devices is empty")
	}
	maxBatch := s.vendors.SysConfigInt(ctx, "vendor_batch_max_devices", 200)
	if len(items) > maxBatch {
		// 整请求拒，不做部分接受（设计稿 §5.1，避免厂商侧对账错位）
		return nil, vendorErr(400, VendorSlugBatchTooLarge,
			"batch too large: max %d devices per request, got %d", maxBatch, len(items))
	}

	modelAuthorized := false
	for _, m := range vendor.Models {
		if m == deviceModel {
			modelAuthorized = true
			break
		}
	}

	result := &VendorBatchResult{Model: deviceModel, Results: make([]VendorRegisterItemResult, 0, len(items))}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		deviceID := strings.TrimSpace(item.DeviceID)
		row := VendorRegisterItemResult{DeviceID: deviceID}
		switch {
		case !modelAuthorized:
			row.Status, row.Code, row.Reason = VendorItemFailed, VendorSlugModelNotAuthorized,
				"model "+deviceModel+" is not in this vendor's authorized list"
		case !vendorDeviceIDPattern.MatchString(deviceID) || !strings.HasPrefix(deviceID, deviceModel+"-"):
			row.Status, row.Code, row.Reason = VendorItemFailed, VendorSlugDeviceIDInvalid,
				"device id must be <model>-YYYYMMDDNNN of the requested model"
		case seen[deviceID]:
			// 同批重复：按 existed 处理，且不再交付一次明文（明文面不因重放扩大）
			row.Status = VendorItemExisted
		default:
			seen[deviceID] = true
			s.fillRegisterItem(ctx, vendor.VendorID, deviceID, strings.TrimSpace(item.Firmware), &row)
		}
		result.Results = append(result.Results, row)
		switch row.Status {
		case VendorItemCreated:
			result.Summary.Created++
		case VendorItemExisted:
			result.Summary.Existed++
		default:
			result.Summary.Failed++
		}
	}
	result.Summary.Requested = len(items)

	// §5.2 两行都要留：①注册三态一条（批内计数）；②每条 created 一次 device_secret_fetch
	//（channel=on-register，reason 记 on-register）。同事务批写，任一行失败整批不交明文。
	audits := make([]repo.VendorAuditInput, 0, result.Summary.Created+1)
	for _, row := range result.Results {
		if row.Status != VendorItemCreated {
			continue
		}
		audits = append(audits, repo.VendorAuditInput{
			OperatorID:   vendor.VendorID,
			OperatorRole: VendorRole,
			Action:       VendorActionSecretRead,
			TargetType:   VendorTargetDevice,
			TargetID:     row.DeviceID,
			IP:           ip,
			Detail: map[string]any{
				"key_prefix": keyPrefix,
				"reason":     "on-register",
				"channel":    "on-register",
			},
		})
	}
	audits = append(audits, repo.VendorAuditInput{
		OperatorID:   vendor.VendorID,
		OperatorRole: VendorRole,
		Action:       VendorActionRegister,
		TargetType:   VendorTargetDevice,
		TargetID:     firstDeviceID(result.Results),
		IP:           ip,
		Detail: map[string]any{
			"key_prefix": keyPrefix,
			"model":      deviceModel,
			"requested":  result.Summary.Requested,
			"created":    result.Summary.Created,
			"existed":    result.Summary.Existed,
			"failed":     result.Summary.Failed,
		},
	})
	if appErr := s.vendors.WriteVendorAuditBatch(ctx, audits); appErr != nil {
		// 审计写不进 = 披露没有前置条件；设备行已按幂等写入，重放只会得到 existed，
		// 保密文走补取端点（那一次同样会先写审计）。
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "write register audit: %v", appErr)
	}
	return result, nil
}

func (s *DeviceService) fillRegisterItem(ctx context.Context, vendorID, deviceID, firmware string, row *VendorRegisterItemResult) {
	secret, err := crypto.RandomSecret(secretBytes)
	if err != nil {
		row.Status, row.Code, row.Reason = VendorItemFailed, "secret_generation_failed", "generate device secret failed"
		return
	}
	encSecret, encErr := s.enc.Encrypt([]byte(secret))
	if encErr != nil {
		row.Status, row.Code, row.Reason = VendorItemFailed, "secret_generation_failed", "encrypt device secret failed"
		return
	}
	created, regErr := s.vendors.RegisterVendorDevice(ctx, &repo.VendorDevice{
		DeviceID:        deviceID,
		Model:           rowModel(deviceID),
		FirmwareVersion: firmware,
		SecretEnc:       encSecret,
	}, vendorID)
	if regErr != nil {
		row.Status, row.Code, row.Reason = VendorItemFailed, "register_failed", "register device failed"
		return
	}
	if created {
		row.Status, row.Secret = VendorItemCreated, secret
		return
	}
	// 冲突：同厂商=existed（不重发密钥）；他厂商=failed（不泄露归属）
	if _, getErr := s.vendors.GetVendorDevice(ctx, vendorID, deviceID); getErr == nil {
		row.Status = VendorItemExisted
		return
	}
	row.Status, row.Code, row.Reason = VendorItemFailed, VendorSlugDeviceIDTaken, "device id already registered"
}

// rowModel 从 deviceId 截出型号段（PRS-ML05-RC-20261020001 → PRS-ML05-RC）。
// 调用前已经过 HasPrefix(model+"-") 校验，这里只做一次去尾，不再二次推断。
func rowModel(deviceID string) string {
	if idx := strings.LastIndex(deviceID, "-"); idx > 0 {
		return deviceID[:idx]
	}
	return deviceID
}

func firstDeviceID(rows []VendorRegisterItemResult) string {
	for _, r := range rows {
		if r.Status != VendorItemFailed {
			return r.DeviceID
		}
	}
	if len(rows) > 0 {
		return rows[0].DeviceID
	}
	return ""
}

// ─────────────────────────────────────────────────────────────
// 补取 secret / 元数据 / 列表
// ─────────────────────────────────────────────────────────────

// VendorFetchSecret 逐台补取明文（reason 必填、每厂商日限、每次落审计）。
func (s *DeviceService) VendorFetchSecret(ctx context.Context, vendor *repo.Vendor, keyPrefix, deviceID, reason, ip string) (string, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return "", err
	}
	if vendor.Status != "active" {
		return "", vendorErr(403, VendorSlugVendorSuspended, "vendor %s is %s", vendor.VendorID, vendor.Status)
	}
	if strings.TrimSpace(reason) == "" {
		return "", vendorErr(400, VendorSlugReasonRequired, "reason is required for secret refetch")
	}
	dayStart := startOfDayBeijing(s.now())
	limit := s.vendors.SysConfigInt(ctx, "vendor_secret_fetch_daily", 2000)
	n, err := s.vendors.CountVendorSecretFetchesSince(ctx, vendor.VendorID, dayStart)
	if err != nil {
		return "", vendorErr(500, VendorSlugStoreUnavailable, "count secret fetches: %v", err)
	}
	if n >= int64(limit) {
		return "", vendorErr(429, VendorSlugSecretFetchLimit,
			"daily secret fetch limit reached: %d/%d", n, limit)
	}

	dev, err := s.vendors.GetVendorDevice(ctx, vendor.VendorID, deviceID)
	if err != nil {
		return "", vendorErr(404, VendorSlugDeviceNotFound, "device not found")
	}
	secret, decErr := s.enc.Decrypt(dev.SecretEnc)
	if decErr != nil {
		return "", vendorErr(409, VendorSlugSecretUnusable, "device secret material is unusable")
	}

	// 审计先行：写不进审计就不交明文（T728 同一条纪律）
	if auditErr := s.vendors.WriteVendorAudit(ctx, repo.VendorAuditInput{
		OperatorID:   vendor.VendorID,
		OperatorRole: VendorRole,
		Action:       VendorActionSecretRead,
		TargetType:   VendorTargetDevice,
		TargetID:     deviceID,
		IP:           ip,
		Detail: map[string]any{
			"key_prefix": keyPrefix,
			"reason":     strings.TrimSpace(reason),
			"channel":    "by-request",
		},
	}); auditErr != nil {
		return "", vendorErr(500, VendorSlugStoreUnavailable, "write secret fetch audit: %v", auditErr)
	}
	return string(secret), nil
}

// VendorDeviceMeta 本厂商设备元数据（不含任何患者标识）。
func (s *DeviceService) VendorDeviceMeta(ctx context.Context, vendor *repo.Vendor, deviceID string) (*VendorDeviceView, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return nil, err
	}
	if vendor.Status != "active" {
		return nil, vendorErr(403, VendorSlugVendorSuspended, "vendor %s is %s", vendor.VendorID, vendor.Status)
	}
	dev, err := s.vendors.GetVendorDevice(ctx, vendor.VendorID, deviceID)
	if err != nil {
		return nil, vendorErr(404, VendorSlugDeviceNotFound, "device not found")
	}
	return vendorDeviceView(dev), nil
}

// VendorListDevices 本厂商设备分页列表（status 为空=不过滤）。
func (s *DeviceService) VendorListDevices(ctx context.Context, vendor *repo.Vendor, status string, page, pageSize int) ([]VendorDeviceView, int64, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return nil, 0, err
	}
	if vendor.Status != "active" {
		return nil, 0, vendorErr(403, VendorSlugVendorSuspended, "vendor %s is %s", vendor.VendorID, vendor.Status)
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	switch status {
	case "", "unbound", "online", "offline", "abnormal":
	default:
		return nil, 0, vendorErr(400, VendorSlugInvalidParam, "invalid status filter")
	}
	rows, total, err := s.vendors.ListVendorDevices(ctx, vendor.VendorID, status, page, pageSize)
	if err != nil {
		return nil, 0, vendorErr(500, VendorSlugStoreUnavailable, "list vendor devices: %v", err)
	}
	views := make([]VendorDeviceView, 0, len(rows))
	for i := range rows {
		views = append(views, *vendorDeviceView(&rows[i]))
	}
	return views, total, nil
}

func vendorDeviceView(d *repo.VendorDevice) *VendorDeviceView {
	return &VendorDeviceView{
		DeviceID:        d.DeviceID,
		Model:           d.Model,
		FirmwareVersion: d.FirmwareVersion,
		Status:          d.Status,
		Bound:           d.PatientID != nil,
		RegisteredAt:    d.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// startOfDayBeijing 自然日边界按北京时间（厂商与运营都在东八区；与「5 万次/自然日」口径一致）。
func startOfDayBeijing(now time.Time) time.Time {
	cst := time.FixedZone("CST", 8*3600)
	t := now.In(cst)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, cst)
}

// ─────────────────────────────────────────────────────────────
// 管理面：厂商登记 / 签发 / 吊销 / 列表
// ─────────────────────────────────────────────────────────────

// CreateVendor 运营管理员登记厂商（设计稿 §1.2 唯一入口）。
func (s *DeviceService) CreateVendor(ctx context.Context, adminID, name string, models []string, note string) (*repo.Vendor, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, vendorErr(400, VendorSlugInvalidParam, "name is required")
	}
	clean := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if !vendorModelPattern.MatchString(m) {
			return nil, vendorErr(400, VendorSlugInvalidParam, "invalid model %q", m)
		}
		clean = append(clean, m)
	}
	if len(clean) == 0 {
		return nil, vendorErr(400, VendorSlugInvalidParam, "models is required")
	}
	vendorID, err := s.vendors.NextVendorID(ctx)
	if err != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "allocate vendor id: %v", err)
	}
	v := &repo.Vendor{
		VendorID:  vendorID,
		Name:      name,
		Models:    clean,
		Status:    "active",
		CreatedBy: &adminID,
	}
	if note != "" {
		v.Note = &note
	}
	if err := s.vendors.CreateVendor(ctx, v); err != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "create vendor: %v", err)
	}
	stored, err := s.vendors.GetVendor(ctx, vendorID)
	if err != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "read back vendor: %v", err)
	}
	return stored, nil
}

// ListVendors 管理面厂商列表
func (s *DeviceService) ListVendors(ctx context.Context) ([]repo.Vendor, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return nil, err
	}
	list, err := s.vendors.ListVendors(ctx)
	if err != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "list vendors: %v", err)
	}
	return list, nil
}

// IssueVendorKey 签发（仅 ROLE_ADMIN；一次性明文、上限 3 把、默认 90 天）。
func (s *DeviceService) IssueVendorKey(ctx context.Context, adminID, vendorID string) (*VendorIssuedKey, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return nil, err
	}
	if s.vendorPepper == "" {
		// fail-closed：缺 pepper 一律拒绝，不降级为「存明文/存空摘要」
		return nil, vendorErr(500, VendorSlugPepperMissing, "VENDOR_KEY_PEPPER is not configured")
	}
	vendor, err := s.vendors.GetVendor(ctx, vendorID)
	if err != nil {
		return nil, vendorErr(404, VendorSlugVendorNotFound, "vendor not found")
	}
	if vendor.Status != "active" {
		return nil, vendorErr(409, VendorSlugVendorSuspended, "vendor %s is %s", vendorID, vendor.Status)
	}
	now := s.now()
	maxActive := s.vendors.SysConfigInt(ctx, "vendor_key_max_active", 3)
	active, err := s.vendors.CountActiveVendorKeys(ctx, vendorID, now)
	if err != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "count active keys: %v", err)
	}
	if active >= maxActive {
		return nil, vendorErr(409, VendorSlugKeyLimit,
			"active key limit reached: %d/%d, revoke one before issuing", active, maxActive)
	}

	raw, genErr := crypto.RandomSecret(32)
	if genErr != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "generate key: %v", genErr)
	}
	fullKey := "bsk_" + vendorID + "_" + raw
	keyIDRaw, genErr := crypto.RandomSecret(4)
	if genErr != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "generate key id: %v", genErr)
	}
	ttlDays := s.vendors.SysConfigInt(ctx, "vendor_key_ttl_days", 90)
	k := &repo.VendorAPIKey{
		KeyID:     "VK" + strings.ToUpper(keyIDRaw),
		VendorID:  vendorID,
		KeyHash:   vendorKeyHash(s.vendorPepper, fullKey),
		KeyPrefix: "bsk_" + vendorID + "_",
		KeyTail:   fullKey[len(fullKey)-4:],
		Status:    "active",
		IssuedBy:  adminID,
		ExpiresAt: now.Add(time.Duration(ttlDays) * 24 * time.Hour),
	}
	if err := s.vendors.CreateVendorAPIKey(ctx, k); err != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "create key: %v", err)
	}
	if auditErr := s.vendors.WriteVendorAudit(ctx, repo.VendorAuditInput{
		OperatorID:   adminID,
		OperatorRole: "ROLE_ADMIN",
		Action:       VendorActionKeyIssue,
		TargetType:   VendorTargetKey,
		TargetID:     k.KeyID,
		Detail: map[string]any{
			"vendor_id": vendorID,
			"key_tail":  k.KeyTail,
			"ttl_days":  ttlDays,
		},
	}); auditErr != nil {
		// key 已可生效但审计缺失：明文不回（调用方会重试签发，重试受上限约束）
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "write key issue audit: %v", auditErr)
	}
	return &VendorIssuedKey{
		KeyID:     k.KeyID,
		VendorID:  vendorID,
		KeyPrefix: k.KeyPrefix,
		KeyTail:   k.KeyTail,
		Key:       fullKey,
		ExpiresAt: k.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}

// RevokeVendorKey 吊销（幂等；已吊销再调不重复写时间戳）。
func (s *DeviceService) RevokeVendorKey(ctx context.Context, adminID, keyID, reason string) (string, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return "", err
	}
	key, err := s.vendors.GetVendorAPIKey(ctx, keyID)
	if err != nil {
		return "", vendorErr(404, VendorSlugKeyNotFound, "key not found")
	}
	now := s.now()
	vendorID, changed, err := s.vendors.RevokeVendorAPIKey(ctx, keyID, reason, now)
	if err != nil {
		return "", vendorErr(404, VendorSlugKeyNotFound, "key not found")
	}
	if changed {
		daysLeft := int(key.ExpiresAt.Sub(now).Hours() / 24)
		if auditErr := s.vendors.WriteVendorAudit(ctx, repo.VendorAuditInput{
			OperatorID:   adminID,
			OperatorRole: "ROLE_ADMIN",
			Action:       VendorActionKeyRevoke,
			TargetType:   VendorTargetKey,
			TargetID:     keyID,
			Detail: map[string]any{
				"vendor_id":                   vendorID,
				"reason":                      strings.TrimSpace(reason),
				"days_until_expiry_at_revoke": daysLeft,
			},
		}); auditErr != nil {
			return "", vendorErr(500, VendorSlugStoreUnavailable, "write key revoke audit: %v", auditErr)
		}
	}
	return vendorID, nil
}

// ListVendorKeys 管理面 key 列表（不带 hash、不带明文）。
func (s *DeviceService) ListVendorKeys(ctx context.Context, vendorID string) ([]VendorKeyView, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return nil, err
	}
	if _, err := s.vendors.GetVendor(ctx, vendorID); err != nil {
		return nil, vendorErr(404, VendorSlugVendorNotFound, "vendor not found")
	}
	keys, err := s.vendors.ListVendorAPIKeys(ctx, vendorID)
	if err != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "list keys: %v", err)
	}
	views := make([]VendorKeyView, 0, len(keys))
	for i := range keys {
		k := &keys[i]
		view := VendorKeyView{
			KeyID:     k.KeyID,
			VendorID:  k.VendorID,
			KeyPrefix: k.KeyPrefix,
			KeyTail:   k.KeyTail,
			Status:    k.Status,
			IssuedAt:  k.IssuedAt.UTC().Format(time.RFC3339),
			ExpiresAt: k.ExpiresAt.UTC().Format(time.RFC3339),
		}
		if k.RevokedAt != nil {
			s := k.RevokedAt.UTC().Format(time.RFC3339)
			view.RevokedAt = &s
		}
		views = append(views, view)
	}
	return views, nil
}

// VendorByID 取厂商主体（设备面每次请求按网关注入的 vendor_id 读一次，
// 与 §3「每次查库、不加进程内缓存」同口径：厂商停用即时生效）。
func (s *DeviceService) VendorByID(ctx context.Context, vendorID string) (*repo.Vendor, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return nil, err
	}
	v, err := s.vendors.GetVendor(ctx, vendorID)
	if err != nil {
		return nil, vendorErr(401, "invalid_key", "vendor not found")
	}
	return v, nil
}

// VendorRateLimits 读取 QPS / 日调用上限（sys_configs；handler 限流用）。
func (s *DeviceService) VendorRateLimits(ctx context.Context) (qps, daily int) {
	if s.vendors == nil {
		return 10, 50000
	}
	return s.vendors.SysConfigInt(ctx, "vendor_key_qps_limit", 10),
		s.vendors.SysConfigInt(ctx, "vendor_key_daily_calls", 50000)
}

// VendorDeniedInput 越权尝试入参（网关侧挡住后异步回报，审计 owner 在 device-service）。
type VendorDeniedInput struct {
	VendorID string
	KeyID    string
	Method   string
	Path     string
	Status   int
	IP       string
}

// RecordVendorDenied 落一条 vendor_denied，并在该 key 近一分钟越权 ≥20 次时自动 suspended。
// 审计失败不回传错误给网关（拒绝动作本身已完成），返回 error 仅供 device-service 日志。
func (s *DeviceService) RecordVendorDenied(ctx context.Context, in VendorDeniedInput) error {
	if s.vendors == nil {
		return errors.New("vendor store not configured")
	}
	if err := s.vendors.WriteVendorAudit(ctx, repo.VendorAuditInput{
		OperatorID:   in.VendorID,
		OperatorRole: VendorRole,
		Action:       "vendor_denied",
		TargetType:   "api_path",
		TargetID:     in.Path,
		IP:           in.IP,
		Detail: map[string]any{
			"method": in.Method,
			"status": in.Status,
			"key_id": in.KeyID,
		},
	}); err != nil {
		return err
	}
	if in.KeyID == "" {
		return nil
	}
	n, err := s.vendors.CountVendorDenialsSince(ctx, in.KeyID, s.now().Add(-time.Minute))
	if err != nil {
		return err
	}
	if n >= 20 {
		// 只暂停这一把 key，不暂停整个厂商——泄露 key 的越权不应连带厂商其它产线
		return s.vendors.SuspendVendorKey(ctx, in.KeyID)
	}
	return nil
}

// RecordVendorRateHit 超限留痕（§5.2：命中的键、当前值、阈值）。失败只记日志，不回滚拒绝动作。
func (s *DeviceService) RecordVendorRateHit(ctx context.Context, vendorID, keyID, configKey string, current, threshold int) error {
	if s.vendors == nil {
		return errors.New("vendor store not configured")
	}
	return s.vendors.WriteVendorAudit(ctx, repo.VendorAuditInput{
		OperatorID:   vendorID,
		OperatorRole: VendorRole,
		Action:       "vendor_rate_hit",
		TargetType:   VendorTargetKey,
		TargetID:     keyID,
		Detail: map[string]any{
			"config_key": configKey,
			"current":    current,
			"threshold":  threshold,
		},
	})
}

// VendorKeyCandidatesForPrefix 网关鉴权用的候选行（internal 端点，容器内网可达）。
func (s *DeviceService) VendorKeyCandidatesForPrefix(ctx context.Context, keyPrefix string) ([]repo.VendorKeyCandidate, *VendorError) {
	if err := s.vendorStoreReady(); err != nil {
		return nil, err
	}
	list, err := s.vendors.VendorKeyCandidates(ctx, keyPrefix)
	if err != nil {
		return nil, vendorErr(500, VendorSlugStoreUnavailable, "lookup key candidates: %v", err)
	}
	return list, nil
}

// TouchVendorKey 刷新 last_used_at（best-effort，失败只记 WARN 由 handler 决定）。
func (s *DeviceService) TouchVendorKey(ctx context.Context, keyID string) {
	if s.vendors == nil {
		return
	}
	_ = s.vendors.TouchVendorKey(ctx, keyID, s.now())
}

// vendorKeyHash HMAC-SHA256(pepper, 全串) 的 hex（64 位）。
// 与网关验签侧同一函数语义：pepper 是密钥，全串是消息；不用裸 SHA256（防彩虹表）。
func vendorKeyHash(pepper, fullKey string) string {
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte(fullKey))
	return hex.EncodeToString(mac.Sum(nil))
}
