// Package repo device-service 数据访问层（pgx 实现）
//
// 写归属（架构 §4.2 Shared Database + 表级写归属）：
// devices / device_bindings / install_records / baselines 写归 device-service；
// patients / technicians 仅存在性只读（owner: user-service）。
package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// ErrNotFound 行不存在（service 层映射为 20404/10404）
var ErrNotFound = errors.New("repo: row not found")

// ErrConflict 唯一约束/状态冲突（uk_bindings_active / uk_install_baseline / 基线已存在）
var ErrConflict = errors.New("repo: conflict")

// BindParams 绑定事务入参
type BindParams struct {
	DeviceID   string
	PatientID  string
	OperatorID string
}

// Store device-service 存储契约（单测可用内存实现替换）
type Store interface {
	// RegisterDevice 幂等注册：ON CONFLICT DO NOTHING；created=false 表示已存在
	RegisterDevice(ctx context.Context, d *model.Device) (created bool, err error)
	// GetDevice 查设备；不存在返回 ErrNotFound
	GetDevice(ctx context.Context, deviceID string) (*model.Device, error)
	// PatientExists / TechExists 用户域存在性只读
	PatientExists(ctx context.Context, patientID string) (bool, error)
	TechExists(ctx context.Context, techID string) (bool, error)
	// Bind 绑定/自动换绑事务：同患者幂等 nil；他患者 active binding 存在时自动换绑
	// （旧行写 unbind_at+reason=rebind，返回 prevActive）；新 binding + devices 更新同一事务
	Bind(ctx context.Context, p BindParams) (prevActive *model.Binding, err error)
	// Rebind 换绑事务：要求存在 active binding（否则 ErrConflict）；
	// 旧行写 unbind_at+reason=rebind → 新 binding → 更新 devices，历史可追溯
	Rebind(ctx context.Context, p BindParams) (prevActive *model.Binding, err error)
	// Unbind 解绑事务（幂等）：hadActive=false 表示本就无有效绑定
	Unbind(ctx context.Context, deviceID, operatorID string) (hadActive bool, err error)
	// Touch 上报/补传更新：last_report_at 单调推进，仅最新帧改状态，updated_at 行级刷新
	Touch(ctx context.Context, deviceID string, ts time.Time, status string) error
	// ListBindings 绑定历史（bind_at DESC）
	ListBindings(ctx context.Context, deviceID string) ([]model.Binding, error)
	// CreateInstall 新建安装记录，返回 install_id
	CreateInstall(ctx context.Context, rec *model.InstallRecord) (int64, error)
	// GetInstall 查安装记录；不存在返回 ErrNotFound
	GetInstall(ctx context.Context, installID int64) (*model.InstallRecord, error)
	// SaveBaseline 基线落库事务：插 baselines + 回填 install_records.baseline_id/notes/signature_url；
	// install 已有基线返回 ErrConflict
	SaveBaseline(ctx context.Context, installID int64, offsets []float32, calibratorID string) (int64, error)
	// UpdateInstallMeta 更新安装记录备注与签名（基线之外的一次性回填）
	UpdateInstallMeta(ctx context.Context, installID int64, notes, signatureURL *string) error
	// SetWifiSSID 维护 devices.wifi_ssid（架构 §2.3 配网状态）
	SetWifiSSID(ctx context.Context, deviceID, ssid string) error
}

// PGStore Store 的 pgxpool 实现
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore 创建 PGStore
func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

// RegisterDevice 幂等注册（重复注册返回既有记录，不覆盖密钥）
func (r *PGStore) RegisterDevice(ctx context.Context, d *model.Device) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO devices (device_id, model, device_secret_enc, secret_version, status)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (device_id) DO NOTHING`,
		d.DeviceID, d.Model, d.DeviceSecretEnc, d.SecretVersion, d.Status)
	if err != nil {
		return false, fmt.Errorf("register device: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// GetDevice 查设备全字段
func (r *PGStore) GetDevice(ctx context.Context, deviceID string) (*model.Device, error) {
	d := &model.Device{}
	err := r.pool.QueryRow(ctx,
		`SELECT device_id, model, COALESCE(firmware_version, ''), device_secret_enc, secret_version,
		        patient_id, wifi_ssid, bind_time, status, last_report_at, created_at, updated_at
		 FROM devices WHERE device_id = $1`, deviceID,
	).Scan(&d.DeviceID, &d.Model, &d.FirmwareVersion, &d.DeviceSecretEnc, &d.SecretVersion,
		&d.PatientID, &d.WifiSSID, &d.BindTime, &d.Status, &d.LastReportAt, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}
	return d, nil
}

func (r *PGStore) exists(ctx context.Context, table, column, id string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE %s = $1)`, table, column), id,
	).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("exists %s: %w", table, err)
	}
	return ok, nil
}

// PatientExists patients 存在性（owner: user-service，只读）
func (r *PGStore) PatientExists(ctx context.Context, patientID string) (bool, error) {
	return r.exists(ctx, "patients", "patient_id", patientID)
}

// TechExists technicians 存在性（owner: user-service，只读）
func (r *PGStore) TechExists(ctx context.Context, techID string) (bool, error) {
	return r.exists(ctx, "technicians", "tech_id", techID)
}

// Bind 绑定/自动换绑事务（同设备仅一个 active binding，uk_bindings_active 兜底）：
// 同患者重复绑定幂等；他患者 active binding 存在时自动换绑（旧行关闭 reason=rebind）。
func (r *PGStore) Bind(ctx context.Context, p BindParams) (*model.Binding, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("bind: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 锁设备行，防并发绑定
	var devExists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM devices WHERE device_id = $1 FOR UPDATE)`, p.DeviceID,
	).Scan(&devExists)
	if err != nil {
		return nil, fmt.Errorf("bind: lock device: %w", err)
	}
	if !devExists {
		return nil, ErrNotFound
	}

	// 查当前 active binding（行锁）
	prev := &model.Binding{}
	err = tx.QueryRow(ctx,
		`SELECT binding_id, device_id, patient_id, bind_at
		 FROM device_bindings WHERE device_id = $1 AND unbind_at IS NULL FOR UPDATE`, p.DeviceID,
	).Scan(&prev.BindingID, &prev.DeviceID, &prev.PatientID, &prev.BindAt)
	hasActive := true
	if errors.Is(err, pgx.ErrNoRows) {
		hasActive = false
	} else if err != nil {
		return nil, fmt.Errorf("bind: query active: %w", err)
	}

	var prevActive *model.Binding
	reason := model.ReasonInstall // 首绑
	if hasActive {
		if prev.PatientID == p.PatientID {
			return nil, nil // 幂等：同患者重复绑定，无变更
		}
		// 自动换绑：关闭旧绑定（reason=rebind），历史可追溯
		if _, err = tx.Exec(ctx,
			`UPDATE device_bindings SET unbind_at = now(), reason = $2, operator_id = $3
			 WHERE binding_id = $1`, prev.BindingID, model.ReasonRebind, p.OperatorID,
		); err != nil {
			return nil, fmt.Errorf("bind: close previous: %w", err)
		}
		prevActive = prev
		reason = model.ReasonRebind
	}

	if _, err = tx.Exec(ctx,
		`INSERT INTO device_bindings (device_id, patient_id, reason, operator_id)
		 VALUES ($1, $2, $3, $4)`, p.DeviceID, p.PatientID, reason, p.OperatorID,
	); err != nil {
		return nil, fmt.Errorf("bind: insert binding: %w", err)
	}

	// 同事务更新 devices 冗余：patient_id / bind_time / 状态（unbound→offline）/ updated_at
	if _, err = tx.Exec(ctx,
		`UPDATE devices
		 SET patient_id = $2, bind_time = now(),
		     status = CASE WHEN status = $3 THEN $4 ELSE status END,
		     updated_at = now()
		 WHERE device_id = $1`, p.DeviceID, p.PatientID, model.StatusUnbound, model.StatusOffline,
	); err != nil {
		return nil, fmt.Errorf("bind: update device: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("bind: commit: %w", err)
	}
	return prevActive, nil
}

// Rebind 换绑事务：旧 binding 写 unbind_at+reason=rebind+operator → 新 binding → 更新 devices（历史可追溯）。
// 无 active binding 时返回 ErrConflict（应先走 Bind）。
func (r *PGStore) Rebind(ctx context.Context, p BindParams) (*model.Binding, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebind: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var devExists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM devices WHERE device_id = $1 FOR UPDATE)`, p.DeviceID,
	).Scan(&devExists)
	if err != nil {
		return nil, fmt.Errorf("rebind: lock device: %w", err)
	}
	if !devExists {
		return nil, ErrNotFound
	}

	prev := &model.Binding{}
	err = tx.QueryRow(ctx,
		`SELECT binding_id, device_id, patient_id, bind_at
		 FROM device_bindings WHERE device_id = $1 AND unbind_at IS NULL FOR UPDATE`, p.DeviceID,
	).Scan(&prev.BindingID, &prev.DeviceID, &prev.PatientID, &prev.BindAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConflict // 无 active binding，应先走 Bind
	}
	if err != nil {
		return nil, fmt.Errorf("rebind: query active: %w", err)
	}
	if prev.PatientID == p.PatientID {
		return nil, nil // 幂等：换绑到同一患者，无变更
	}

	// 关闭旧绑定（reason=rebind）
	if _, err = tx.Exec(ctx,
		`UPDATE device_bindings SET unbind_at = now(), reason = $2, operator_id = $3
		 WHERE binding_id = $1`, prev.BindingID, model.ReasonRebind, p.OperatorID,
	); err != nil {
		return nil, fmt.Errorf("rebind: close previous: %w", err)
	}

	if _, err = tx.Exec(ctx,
		`INSERT INTO device_bindings (device_id, patient_id, reason, operator_id)
		 VALUES ($1, $2, $3, $4)`, p.DeviceID, p.PatientID, model.ReasonRebind, p.OperatorID,
	); err != nil {
		return nil, fmt.Errorf("rebind: insert binding: %w", err)
	}

	// 换绑不清空在线态，仅更新归属与时间戳
	if _, err = tx.Exec(ctx,
		`UPDATE devices SET patient_id = $2, bind_time = now(), updated_at = now()
		 WHERE device_id = $1`, p.DeviceID, p.PatientID,
	); err != nil {
		return nil, fmt.Errorf("rebind: update device: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("rebind: commit: %w", err)
	}
	return prev, nil
}

// Unbind 解绑事务（幂等：无 active binding 时 hadActive=false，不报错）
func (r *PGStore) Unbind(ctx context.Context, deviceID, operatorID string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("unbind: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var devExists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM devices WHERE device_id = $1 FOR UPDATE)`, deviceID,
	).Scan(&devExists)
	if err != nil {
		return false, fmt.Errorf("unbind: lock device: %w", err)
	}
	if !devExists {
		return false, ErrNotFound
	}

	tag, err := tx.Exec(ctx,
		`UPDATE device_bindings SET unbind_at = now(), reason = $2, operator_id = $3
		 WHERE device_id = $1 AND unbind_at IS NULL`,
		deviceID, model.ReasonUnbind, operatorID)
	if err != nil {
		return false, fmt.Errorf("unbind: close binding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// 幂等：本就未绑定；事务仍提交（无副作用）
		if err = tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("unbind: commit: %w", err)
		}
		return false, nil
	}

	if _, err = tx.Exec(ctx,
		`UPDATE devices SET patient_id = NULL, bind_time = NULL, status = $2, updated_at = now()
		 WHERE device_id = $1`, deviceID, model.StatusUnbound,
	); err != nil {
		return false, fmt.Errorf("unbind: update device: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("unbind: commit: %w", err)
	}
	return true, nil
}

// Touch 上报/补传状态更新：
//   - last_report_at 单调推进（GREATEST，防乱序补传回退，避免状态误判）
//   - 仅当本帧为最新帧（ts ≥ 现 last_report_at）时才改状态：陈旧补传帧不得把已恢复设备打回 abnormal
//   - updated_at 行级刷新
func (r *PGStore) Touch(ctx context.Context, deviceID string, ts time.Time, status string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE devices SET
		   last_report_at = GREATEST(COALESCE(last_report_at, $2), $2),
		   status = CASE WHEN $2 >= COALESCE(last_report_at, '-infinity'::timestamptz) THEN $3 ELSE status END,
		   updated_at = now()
		 WHERE device_id = $1`, deviceID, ts.UTC(), status)
	if err != nil {
		return fmt.Errorf("touch device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListBindings 绑定历史（最新在前）
func (r *PGStore) ListBindings(ctx context.Context, deviceID string) ([]model.Binding, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT binding_id, device_id, patient_id, bind_at, unbind_at, reason, operator_id
		 FROM device_bindings WHERE device_id = $1 ORDER BY bind_at DESC, binding_id DESC`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("list bindings: %w", err)
	}
	defer rows.Close()

	out := []model.Binding{}
	for rows.Next() {
		var b model.Binding
		if err = rows.Scan(&b.BindingID, &b.DeviceID, &b.PatientID, &b.BindAt, &b.UnbindAt, &b.Reason, &b.OperatorID); err != nil {
			return nil, fmt.Errorf("list bindings scan: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CreateInstall 新建安装记录（wifi_status 默认 unconfigured）
func (r *PGStore) CreateInstall(ctx context.Context, rec *model.InstallRecord) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO install_records (device_id, patient_id, tech_id, calibrate_time, notes, signature_url)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING install_id`,
		rec.DeviceID, rec.PatientID, rec.TechID, rec.CalibrateTime, rec.Notes, rec.SignatureURL,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create install: %w", err)
	}
	return id, nil
}

// GetInstall 查安装记录
func (r *PGStore) GetInstall(ctx context.Context, installID int64) (*model.InstallRecord, error) {
	rec := &model.InstallRecord{}
	err := r.pool.QueryRow(ctx,
		`SELECT install_id, device_id, patient_id, tech_id, calibrate_time,
		        baseline_id, notes, signature_url, wifi_status, created_at
		 FROM install_records WHERE install_id = $1`, installID,
	).Scan(&rec.InstallID, &rec.DeviceID, &rec.PatientID, &rec.TechID, &rec.CalibrateTime,
		&rec.BaselineID, &rec.Notes, &rec.SignatureURL, &rec.WifiStatus, &rec.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get install: %w", err)
	}
	return rec, nil
}

// SaveBaseline 基线落库事务（对齐 P0-3 流程约定：install 先行，校准后回填）
func (r *PGStore) SaveBaseline(ctx context.Context, installID int64, offsets []float32, calibratorID string) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("save baseline: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 锁 install 行，校验存在且尚未回填基线
	var deviceID string
	var baselineID *int64
	err = tx.QueryRow(ctx,
		`SELECT device_id, baseline_id FROM install_records WHERE install_id = $1 FOR UPDATE`, installID,
	).Scan(&deviceID, &baselineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("save baseline: lock install: %w", err)
	}
	if baselineID != nil {
		return 0, ErrConflict // 一次安装至多一条基线（uk_install_baseline）
	}

	var newID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO baselines (install_id, device_id, offset_values, calibrator_id)
		 VALUES ($1, $2, $3, $4) RETURNING baseline_id`,
		installID, deviceID, offsets, calibratorID,
	).Scan(&newID)
	if err != nil {
		return 0, fmt.Errorf("save baseline: insert: %w", err)
	}

	if _, err = tx.Exec(ctx,
		`UPDATE install_records SET baseline_id = $2, calibrate_time = now() WHERE install_id = $1`,
		installID, newID,
	); err != nil {
		return 0, fmt.Errorf("save baseline: backfill: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("save baseline: commit: %w", err)
	}
	return newID, nil
}

// UpdateInstallMeta 回填 notes / signature_url
func (r *PGStore) UpdateInstallMeta(ctx context.Context, installID int64, notes, signatureURL *string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE install_records
		 SET notes = COALESCE($2, notes), signature_url = COALESCE($3, signature_url)
		 WHERE install_id = $1`, installID, notes, signatureURL)
	if err != nil {
		return fmt.Errorf("update install meta: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetWifiSSID 配网成功后维护 devices.wifi_ssid + 最近安装记录 wifi_status
func (r *PGStore) SetWifiSSID(ctx context.Context, deviceID, ssid string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE devices SET wifi_ssid = $2, updated_at = now() WHERE device_id = $1`, deviceID, ssid)
	if err != nil {
		return fmt.Errorf("set wifi ssid: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	// 最近一次安装记录置 connected（无安装记录时不报错：配网可先于安装）
	if _, err = r.pool.Exec(ctx,
		`UPDATE install_records SET wifi_status = 'connected'
		 WHERE install_id = (SELECT install_id FROM install_records WHERE device_id = $1 ORDER BY install_id DESC LIMIT 1)`,
		deviceID); err != nil {
		return fmt.Errorf("set wifi status: %w", err)
	}
	return nil
}
