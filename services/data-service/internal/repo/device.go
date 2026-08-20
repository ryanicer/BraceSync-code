package repo

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeviceStore 设备归属查询契约（devices 表只读，写归 device-service）
type DeviceStore interface {
	// GetBinding 返回设备当前绑定患者与状态；exists=false 表示 device_id 未注册
	GetBinding(ctx context.Context, deviceID string) (patientID, status string, exists bool, err error)
	// GetDeviceByPatient 返回患者当前绑定设备（devices.patient_id 冗余为当前绑定）
	GetDeviceByPatient(ctx context.Context, patientID string) (deviceID, status string, exists bool, err error)
}

// DeviceRepo DeviceStore 的 pgx 实现
type DeviceRepo struct {
	pool *pgxpool.Pool
}

// NewDeviceRepo 创建 DeviceRepo
func NewDeviceRepo(pool *pgxpool.Pool) *DeviceRepo { return &DeviceRepo{pool: pool} }

// GetBinding 查设备绑定（patient_id 可能为 NULL → 空串表示未绑定）
func (r *DeviceRepo) GetBinding(ctx context.Context, deviceID string) (string, string, bool, error) {
	var patientID, status string
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(patient_id, ''), status FROM devices WHERE device_id = $1`, deviceID,
	).Scan(&patientID, &status)
	if err == pgx.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return patientID, status, true, nil
}

// GetDeviceByPatient 反查患者当前绑定设备
func (r *DeviceRepo) GetDeviceByPatient(ctx context.Context, patientID string) (string, string, bool, error) {
	var deviceID, status string
	err := r.pool.QueryRow(ctx,
		`SELECT device_id, status FROM devices WHERE patient_id = $1 ORDER BY updated_at DESC LIMIT 1`, patientID,
	).Scan(&deviceID, &status)
	if err == pgx.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return deviceID, status, true, nil
}
