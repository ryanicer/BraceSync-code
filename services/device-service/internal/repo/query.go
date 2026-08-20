// Package repo — T030：管理端只读查询（设备列表 + 安装记录列表）
//
// 独立 ListStore 接口（不扩展 Store，避免影响既有绑定/注册链路契约与测试基座）；
// PGStore 同时实现两个接口。patients/technicians 仅 LEFT JOIN 只读（owner: user-service）。
package repo

import (
	"context"
	"fmt"
	"time"
)

// DeviceListItem devices LEFT JOIN patients 投影（管理端设备列表，T030 #3）
type DeviceListItem struct {
	DeviceID        string
	Model           string
	FirmwareVersion string
	PatientID       *string
	PatientName     *string // patients.name join（未绑定为 nil）
	WifiSSID        *string
	BindTime        *time.Time
	Status          string
	LastReportAt    *time.Time
}

// InstallListItem install_records 双 join 投影（管理端安装记录列表）
type InstallListItem struct {
	InstallID     int64
	DeviceID      string
	PatientID     string
	PatientName   *string // patients.name join
	TechID        string
	TechName      *string // technicians.name join
	CalibrateTime time.Time
	BaselineID    *int64
	Notes         *string
	SignatureURL  *string
	WifiStatus    string
}

// ListStore 管理端查询契约（handler 注入点；单测 fake / 集成 PGStore）
type ListStore interface {
	// ListDevices 设备分页列表：keyword=设备ID/患者ID/患者姓名（ILIKE）
	ListDevices(ctx context.Context, keyword string, page, pageSize int) ([]DeviceListItem, int64, error)
	// ListInstallRecords 安装记录分页列表：keyword=设备ID/患者ID/患者姓名/技师姓名（ILIKE）
	ListInstallRecords(ctx context.Context, keyword string, page, pageSize int) ([]InstallListItem, int64, error)
}

// likeArg keyword → ILIKE 参数（%% 转义无业务必要，keyword 走占位符不构成注入）
func likeArg(keyword string) string { return "%" + keyword + "%" }

// ListDevices 设备分页列表（patients 姓名 join；走 idx_devices_patient 反查小表可接受）
func (r *PGStore) ListDevices(ctx context.Context, keyword string, page, pageSize int) ([]DeviceListItem, int64, error) {
	base := `FROM devices d LEFT JOIN patients p ON p.patient_id = d.patient_id`
	var args []any
	if keyword != "" {
		args = append(args, likeArg(keyword))
		base += fmt.Sprintf(` WHERE (d.device_id ILIKE $%[1]d OR d.patient_id ILIKE $%[1]d OR p.name ILIKE $%[1]d)`, 1)
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count devices: %w", err)
	}

	offset := (page - 1) * pageSize
	listArgs := append(append([]any{}, args...), pageSize, offset)
	query := `SELECT d.device_id, d.model, COALESCE(d.firmware_version, ''), d.patient_id, p.name,
	                 d.wifi_ssid, d.bind_time, d.status, d.last_report_at ` + base +
		fmt.Sprintf(` ORDER BY d.created_at DESC, d.device_id LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)

	rows, err := r.pool.Query(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	list := make([]DeviceListItem, 0, pageSize)
	for rows.Next() {
		var d DeviceListItem
		if scanErr := rows.Scan(&d.DeviceID, &d.Model, &d.FirmwareVersion, &d.PatientID, &d.PatientName,
			&d.WifiSSID, &d.BindTime, &d.Status, &d.LastReportAt); scanErr != nil {
			return nil, 0, fmt.Errorf("scan device item: %w", scanErr)
		}
		list = append(list, d)
	}
	return list, total, rows.Err()
}

// ListInstallRecords 安装记录分页列表（患者/技师姓名 join，install_id 倒序）
func (r *PGStore) ListInstallRecords(ctx context.Context, keyword string, page, pageSize int) ([]InstallListItem, int64, error) {
	base := `FROM install_records i
	         LEFT JOIN patients p ON p.patient_id = i.patient_id
	         LEFT JOIN technicians t ON t.tech_id = i.tech_id`
	var args []any
	if keyword != "" {
		args = append(args, likeArg(keyword))
		base += fmt.Sprintf(
			` WHERE (i.device_id ILIKE $%[1]d OR i.patient_id ILIKE $%[1]d OR p.name ILIKE $%[1]d OR t.name ILIKE $%[1]d)`, 1)
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count install records: %w", err)
	}

	offset := (page - 1) * pageSize
	listArgs := append(append([]any{}, args...), pageSize, offset)
	query := `SELECT i.install_id, i.device_id, i.patient_id, p.name, i.tech_id, t.name,
	                 i.calibrate_time, i.baseline_id, i.notes, i.signature_url, i.wifi_status ` + base +
		fmt.Sprintf(` ORDER BY i.install_id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)

	rows, err := r.pool.Query(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list install records: %w", err)
	}
	defer rows.Close()

	list := make([]InstallListItem, 0, pageSize)
	for rows.Next() {
		var i InstallListItem
		if scanErr := rows.Scan(&i.InstallID, &i.DeviceID, &i.PatientID, &i.PatientName, &i.TechID, &i.TechName,
			&i.CalibrateTime, &i.BaselineID, &i.Notes, &i.SignatureURL, &i.WifiStatus); scanErr != nil {
			return nil, 0, fmt.Errorf("scan install item: %w", scanErr)
		}
		list = append(list, i)
	}
	return list, total, rows.Err()
}

// compile-time interface satisfaction
var _ ListStore = (*PGStore)(nil)
