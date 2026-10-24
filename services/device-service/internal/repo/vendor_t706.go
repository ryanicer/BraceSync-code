// T706 厂商凭据体系的数据面（repo 层）。
//
// 与 repo.Store 分接口：VendorStore 只被厂商面使用，不挤进 core 设备域接口，
// 既有 FakeStore 因而不必为一个新面补一堆方法；PGStore 同时实现两者。
//
// 三张对象全部由迁移 000042 建立：
//
//	vendors               厂商主体（VND000001，nextval 发号）
//	vendor_api_keys       只存 HMAC 摘要 + 定位前缀/尾 4 位（明文一次性，库内无还原通路）
//	devices.vendor_id     归属列（nullable；NULL=运营/手工注册，非厂商来源）
//
// 审计直写沿用 device-service 先例（audit_t448 / audit_t485 / secret_t728）：audit_logs
// 的 owner 是 user-service，但各服务同库，仓内无跨服务写审计通道 ⇒ 同库直写。
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrVendorKeyLimit 同厂商有效 key 达到上限（签发侧 409）。
var ErrVendorKeyLimit = errors.New("vendor key active limit reached")

// ErrVendorDeviceConflict 批量注册命中他厂商名下的既有 deviceId（该条 failed，不泄露归属）
var ErrVendorDeviceConflict = errors.New("device id belongs to another vendor")

// Vendor 厂商主体行
type Vendor struct {
	VendorID  string
	Name      string
	Models    []string // 授权型号清单（库内逗号分，读出即拆）
	Status    string
	Note      *string
	CreatedBy *string
	CreatedAt time.Time
}

// VendorAPIKey 一把 key 的台账行（Hash 只在内部鉴权查找时取用，任何 DTO 都不带）
type VendorAPIKey struct {
	KeyID        string
	VendorID     string
	KeyHash      string
	KeyPrefix    string
	KeyTail      string
	Status       string
	IssuedBy     string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	RevokeReason *string
	LastUsedAt   *time.Time
}

// VendorKeyCandidate 网关鉴权按前缀取回的候选行（只含比对与状态判定所需字段）
type VendorKeyCandidate struct {
	KeyID        string
	VendorID     string
	KeyHash      string
	Status       string
	ExpiresAt    time.Time
	VendorStatus string
}

// VendorDevice 厂商面可见的设备行（不含患者姓名/手机等任何患者标识）
type VendorDevice struct {
	DeviceID        string
	Model           string
	FirmwareVersion string
	Status          string
	PatientID       *string // 仅供 bound 布尔推导，不出响应体
	SecretEnc       []byte
	CreatedAt       time.Time
}

// VendorAuditInput 厂商面审计行（detail 一律不含明文 secret / 完整 key）
type VendorAuditInput struct {
	OperatorID   string
	OperatorRole string
	Action       string
	TargetType   string
	TargetID     string
	Detail       map[string]any
	IP           string
}

// VendorStore 厂商凭据面的数据访问接口（仅 PGStore 实现；测试用内存实现）
type VendorStore interface {
	// 厂商主体
	NextVendorID(ctx context.Context) (string, error)
	CreateVendor(ctx context.Context, v *Vendor) error
	GetVendor(ctx context.Context, vendorID string) (*Vendor, error)
	ListVendors(ctx context.Context) ([]Vendor, error)

	// API key
	CreateVendorAPIKey(ctx context.Context, k *VendorAPIKey) error
	GetVendorAPIKey(ctx context.Context, keyID string) (*VendorAPIKey, error)
	ListVendorAPIKeys(ctx context.Context, vendorID string) ([]VendorAPIKey, error)
	CountActiveVendorKeys(ctx context.Context, vendorID string, now time.Time) (int, error)
	RevokeVendorAPIKey(ctx context.Context, keyID, reason string, now time.Time) (vendorID string, ok bool, err error)
	VendorKeyCandidates(ctx context.Context, keyPrefix string) ([]VendorKeyCandidate, error)
	TouchVendorKey(ctx context.Context, keyID string, now time.Time) error

	// 厂商设备面
	RegisterVendorDevice(ctx context.Context, d *VendorDevice, vendorID string) (created bool, err error)
	GetVendorDevice(ctx context.Context, vendorID, deviceID string) (*VendorDevice, error)
	ListVendorDevices(ctx context.Context, vendorID, status string, page, pageSize int) ([]VendorDevice, int64, error)
	CountVendorSecretFetchesSince(ctx context.Context, vendorID string, since time.Time) (int64, error)
	CountVendorDenialsSince(ctx context.Context, keyID string, since time.Time) (int64, error)
	SuspendVendorKey(ctx context.Context, keyID string) error

	// 配置（sys_configs，改数不改码）
	SysConfigInt(ctx context.Context, key string, def int) int

	// 审计
	WriteVendorAudit(ctx context.Context, in VendorAuditInput) error
	WriteVendorAuditBatch(ctx context.Context, ins []VendorAuditInput) error
}

// ─────────────────────────────────────────────────────────────
// 厂商主体
// ─────────────────────────────────────────────────────────────

// NextVendorID 平台发号：VND + 6 位数字（VND000001）。序列 nextval 防并发重号。
func (r *PGStore) NextVendorID(ctx context.Context) (string, error) {
	var n int64
	if err := r.pool.QueryRow(ctx, `SELECT nextval('vendors_id_seq')`).Scan(&n); err != nil {
		return "", fmt.Errorf("next vendor id: %w", err)
	}
	if n > 999999 {
		return "", fmt.Errorf("vendor id space exhausted at %d", n)
	}
	return fmt.Sprintf("VND%06d", n), nil
}

func (r *PGStore) CreateVendor(ctx context.Context, v *Vendor) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO vendors (vendor_id, name, models, status, note, created_by)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
		v.VendorID, v.Name, strings.Join(v.Models, ","), v.Status, v.Note, v.CreatedBy)
	if err != nil {
		return fmt.Errorf("create vendor: %w", err)
	}
	return nil
}

func (r *PGStore) GetVendor(ctx context.Context, vendorID string) (*Vendor, error) {
	v := &Vendor{}
	var models string
	err := r.pool.QueryRow(ctx,
		`SELECT vendor_id, name, models, status, note, created_by, created_at
		   FROM vendors WHERE vendor_id = $1`, vendorID,
	).Scan(&v.VendorID, &v.Name, &models, &v.Status, &v.Note, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get vendor: %w", err)
	}
	v.Models = splitModels(models)
	return v, nil
}

func (r *PGStore) ListVendors(ctx context.Context) ([]Vendor, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT vendor_id, name, models, status, note, created_by, created_at
		   FROM vendors ORDER BY vendor_id`)
	if err != nil {
		return nil, fmt.Errorf("list vendors: %w", err)
	}
	defer rows.Close()
	list := make([]Vendor, 0, 8)
	for rows.Next() {
		var v Vendor
		var models string
		if err := rows.Scan(&v.VendorID, &v.Name, &models, &v.Status, &v.Note, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan vendor: %w", err)
		}
		v.Models = splitModels(models)
		list = append(list, v)
	}
	return list, rows.Err()
}

func splitModels(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────
// API key
// ─────────────────────────────────────────────────────────────

func (r *PGStore) CreateVendorAPIKey(ctx context.Context, k *VendorAPIKey) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO vendor_api_keys (key_id, vendor_id, key_hash, key_prefix, key_tail,
		                              status, issued_by, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		k.KeyID, k.VendorID, k.KeyHash, k.KeyPrefix, k.KeyTail, k.Status, k.IssuedBy, k.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create vendor api key: %w", err)
	}
	return nil
}

// GetVendorAPIKey 单把 key 的台账行（吊销前读到期时间用于审计读数）。
func (r *PGStore) GetVendorAPIKey(ctx context.Context, keyID string) (*VendorAPIKey, error) {
	k := &VendorAPIKey{}
	err := r.pool.QueryRow(ctx,
		`SELECT key_id, vendor_id, key_hash, key_prefix, key_tail, status, issued_by,
		        issued_at, expires_at, revoked_at, revoke_reason, last_used_at
		   FROM vendor_api_keys WHERE key_id = $1`, keyID,
	).Scan(&k.KeyID, &k.VendorID, &k.KeyHash, &k.KeyPrefix, &k.KeyTail, &k.Status,
		&k.IssuedBy, &k.IssuedAt, &k.ExpiresAt, &k.RevokedAt, &k.RevokeReason, &k.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get vendor api key: %w", err)
	}
	return k, nil
}

func (r *PGStore) ListVendorAPIKeys(ctx context.Context, vendorID string) ([]VendorAPIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT key_id, vendor_id, key_hash, key_prefix, key_tail, status, issued_by,
		        issued_at, expires_at, revoked_at, revoke_reason, last_used_at
		   FROM vendor_api_keys WHERE vendor_id = $1 ORDER BY issued_at DESC`, vendorID)
	if err != nil {
		return nil, fmt.Errorf("list vendor api keys: %w", err)
	}
	defer rows.Close()
	list := make([]VendorAPIKey, 0, 4)
	for rows.Next() {
		var k VendorAPIKey
		if err := rows.Scan(&k.KeyID, &k.VendorID, &k.KeyHash, &k.KeyPrefix, &k.KeyTail, &k.Status,
			&k.IssuedBy, &k.IssuedAt, &k.ExpiresAt, &k.RevokedAt, &k.RevokeReason, &k.LastUsedAt); err != nil {
			return nil, fmt.Errorf("scan vendor api key: %w", err)
		}
		list = append(list, k)
	}
	return list, rows.Err()
}

// CountActiveVendorKeys 有效把数 = status=active 且未到期（过期行即便 status 未翻也按无效算，
// 与鉴权侧同一判据，避免「库里 4 行 active、其中 1 行已过期」时上限被绕开）。
func (r *PGStore) CountActiveVendorKeys(ctx context.Context, vendorID string, now time.Time) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vendor_api_keys
		  WHERE vendor_id = $1 AND status = 'active' AND expires_at > $2`, vendorID, now).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count active vendor keys: %w", err)
	}
	return n, nil
}

// RevokeVendorAPIKey 吊销（幂等）：已吊销行不重复写时间戳；命中返回 vendor_id 供审计。
func (r *PGStore) RevokeVendorAPIKey(ctx context.Context, keyID, reason string, now time.Time) (string, bool, error) {
	var vendorID string
	err := r.pool.QueryRow(ctx,
		`UPDATE vendor_api_keys
		    SET status = 'revoked', revoked_at = $2, revoke_reason = NULLIF($3, '')
		  WHERE key_id = $1 AND status <> 'revoked'
		  RETURNING vendor_id`, keyID, now, reason).Scan(&vendorID)
	if errors.Is(err, pgx.ErrNoRows) {
		// 幂等分支：查一次是否存在（存在=已吊销，不存在=404）
		var existingVendor string
		scanErr := r.pool.QueryRow(ctx,
			`SELECT vendor_id FROM vendor_api_keys WHERE key_id = $1`, keyID).Scan(&existingVendor)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return "", false, ErrNotFound
		}
		if scanErr != nil {
			return "", false, fmt.Errorf("revoke vendor api key: %w", scanErr)
		}
		return existingVendor, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("revoke vendor api key: %w", err)
	}
	return vendorID, true, nil
}

// VendorKeyCandidates 网关鉴权按 key_prefix 取候选行（同厂商 ≤3 把有效 + 历史行）。
// 返回 hash 供网关本地等时比较；此端点只在容器内网可达（/internal/*）。
func (r *PGStore) VendorKeyCandidates(ctx context.Context, keyPrefix string) ([]VendorKeyCandidate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT k.key_id, k.vendor_id, k.key_hash, k.status, k.expires_at, v.status
		   FROM vendor_api_keys k JOIN vendors v ON v.vendor_id = k.vendor_id
		  WHERE k.key_prefix = $1
		  ORDER BY k.issued_at DESC`, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("vendor key candidates: %w", err)
	}
	defer rows.Close()
	list := make([]VendorKeyCandidate, 0, 4)
	for rows.Next() {
		var c VendorKeyCandidate
		if err := rows.Scan(&c.KeyID, &c.VendorID, &c.KeyHash, &c.Status, &c.ExpiresAt, &c.VendorStatus); err != nil {
			return nil, fmt.Errorf("scan vendor key candidate: %w", err)
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// TouchVendorKey 刷新 last_used_at（鉴权通过后调用；失败不阻断请求，由调用方记 WARN）。
func (r *PGStore) TouchVendorKey(ctx context.Context, keyID string, now time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vendor_api_keys SET last_used_at = $2
		  WHERE key_id = $1 AND (last_used_at IS NULL OR last_used_at < $2 - interval '60 seconds')`,
		keyID, now)
	if err != nil {
		return fmt.Errorf("touch vendor api key: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// 厂商设备面
// ─────────────────────────────────────────────────────────────

// RegisterVendorDevice 厂商面注册：与运营面同一条 devices 通路（同表、同列、同幂等），
// 只多写 vendor_id。ON CONFLICT DO NOTHING 命中既有行时 created=false，由 service 决定
// 是 existed（同厂商）还是 failed（他厂商），这里不猜归属。
func (r *PGStore) RegisterVendorDevice(ctx context.Context, d *VendorDevice, vendorID string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO devices (device_id, model, firmware_version, device_secret_enc, secret_version,
		                      status, contact_area_cm2, vendor_id)
		 VALUES ($1, $2, NULLIF($3, ''), $4, 1, 'unbound', $5, $6)
		 ON CONFLICT (device_id) DO NOTHING`,
		d.DeviceID, d.Model, d.FirmwareVersion, d.SecretEnc, defaultContactAreaCm2, vendorID)
	if err != nil {
		return false, fmt.Errorf("register vendor device: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// defaultContactAreaCm2 与 model.DefaultContactAreaCm2 同值。repo 不 import model（分层方向），
// 两处字面值由 model 的单测与迁移 000040 的默认值钉住（0.64）。
const defaultContactAreaCm2 = 0.64

// GetVendorDevice 归属校验读：vendor_id 不匹配与不存在一律 ErrNotFound（同形 404，
// 不让「存在但不归你」与「不存在」在时序/报文上有差分）。
func (r *PGStore) GetVendorDevice(ctx context.Context, vendorID, deviceID string) (*VendorDevice, error) {
	d := &VendorDevice{}
	err := r.pool.QueryRow(ctx,
		`SELECT device_id, model, COALESCE(firmware_version, ''), status, patient_id,
		        device_secret_enc, created_at
		   FROM devices WHERE device_id = $1 AND vendor_id = $2`, deviceID, vendorID,
	).Scan(&d.DeviceID, &d.Model, &d.FirmwareVersion, &d.Status, &d.PatientID, &d.SecretEnc, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get vendor device: %w", err)
	}
	return d, nil
}

// ListVendorDevices 本厂商设备列表：结果集恒被 vendor_id 收窄；status 为空=不过滤。
// SQL 文本只拼接两条常量片段（where 子句与 LIMIT/OFFSET 占位号）；vendor_id 与 status
// 一律走占位符，任何请求值都不进 SQL 文本。
func (r *PGStore) ListVendorDevices(ctx context.Context, vendorID, status string, page, pageSize int) ([]VendorDevice, int64, error) {
	where := `WHERE vendor_id = $1`
	args := []any{vendorID}
	if status != "" {
		where += ` AND status = $2`
		args = append(args, status)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM devices `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count vendor devices: %w", err)
	}
	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)
	query := fmt.Sprintf(
		`SELECT device_id, model, COALESCE(firmware_version, ''), status, patient_id, device_secret_enc, created_at
		   FROM devices %s ORDER BY created_at DESC, device_id LIMIT $%d OFFSET $%d`,
		where, len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list vendor devices: %w", err)
	}
	defer rows.Close()
	list := make([]VendorDevice, 0, pageSize)
	for rows.Next() {
		var d VendorDevice
		if err := rows.Scan(&d.DeviceID, &d.Model, &d.FirmwareVersion, &d.Status, &d.PatientID,
			&d.SecretEnc, &d.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan vendor device: %w", err)
		}
		list = append(list, d)
	}
	return list, total, rows.Err()
}

// CountVendorDenialsSince 单把 key 自 since 起的越权尝试次数（auto-suspend 判据）。
func (r *PGStore) CountVendorDenialsSince(ctx context.Context, keyID string, since time.Time) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs
		  WHERE action = 'vendor_denied'
		    AND detail->>'key_id' = $1
		    AND created_at >= $2`, keyID, since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count vendor denials: %w", err)
	}
	return n, nil
}

// SuspendVendorKey 自动暂停（仅 active → suspended；已吊销/过期不动）。
func (r *PGStore) SuspendVendorKey(ctx context.Context, keyID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vendor_api_keys SET status = 'suspended' WHERE key_id = $1 AND status = 'active'`, keyID)
	if err != nil {
		return fmt.Errorf("suspend vendor key: %w", err)
	}
	return nil
}

// CountVendorSecretFetchesSince 单厂商自 since 起补取 secret 的次数（审计行计数，不另建计数表）。
// action=device_secret_fetch 且 detail.channel=by-request：注册响应里交付的那批不算补取。
func (r *PGStore) CountVendorSecretFetchesSince(ctx context.Context, vendorID string, since time.Time) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs
		  WHERE action = 'device_secret_fetch'
		    AND operator_id = $1
		    AND created_at >= $2
		    AND detail->>'channel' = 'by-request'`, vendorID, since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count vendor secret fetches: %w", err)
	}
	return n, nil
}

// ─────────────────────────────────────────────────────────────
// 配置与审计
// ─────────────────────────────────────────────────────────────

// SysConfigInt 读一枚整数配置；行缺失/值非整数时回默认值（与既有 config 读侧同口径）。
func (r *PGStore) SysConfigInt(ctx context.Context, key string, def int) int {
	var raw string
	err := r.pool.QueryRow(ctx, `SELECT config_value FROM sys_configs WHERE config_key = $1`, key).Scan(&raw)
	if err != nil {
		return def
	}
	var n int
	if _, scanErr := fmt.Sscanf(strings.TrimSpace(raw), "%d", &n); scanErr != nil {
		return def
	}
	return n
}

// WriteVendorAudit 厂商面审计直写（audit_logs 单行；detail 由调用方保证零明文）。
func (r *PGStore) WriteVendorAudit(ctx context.Context, in VendorAuditInput) error {
	payload, err := json.Marshal(in.Detail)
	if err != nil {
		return fmt.Errorf("marshal vendor audit detail: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO audit_logs (operator_id, operator_role, action, target_type, target_id, detail, ip)
		 VALUES (NULLIF($1, ''), NULLIF($2, ''), $3, NULLIF($4, ''), NULLIF($5, ''), $6::jsonb, NULLIF($7, ''))`,
		in.OperatorID, in.OperatorRole, in.Action, in.TargetType, in.TargetID, string(payload), in.IP)
	if err != nil {
		return fmt.Errorf("write vendor audit: %w", err)
	}
	return nil
}

// WriteVendorAuditBatch 同事务批写（批量注册一次最多 200 条 device_secret_fetch + 1 条注册行）；
// 任一行失败整批回滚，调用方据此拒绝交付明文。
func (r *PGStore) WriteVendorAuditBatch(ctx context.Context, ins []VendorAuditInput) error {
	if len(ins) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin vendor audit batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, in := range ins {
		payload, err := json.Marshal(in.Detail)
		if err != nil {
			return fmt.Errorf("marshal vendor audit detail: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO audit_logs (operator_id, operator_role, action, target_type, target_id, detail, ip)
			 VALUES (NULLIF($1, ''), NULLIF($2, ''), $3, NULLIF($4, ''), NULLIF($5, ''), $6::jsonb, NULLIF($7, ''))`,
			in.OperatorID, in.OperatorRole, in.Action, in.TargetType, in.TargetID, string(payload), in.IP); err != nil {
			return fmt.Errorf("write vendor audit batch: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit vendor audit batch: %w", err)
	}
	return nil
}
