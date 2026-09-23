package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PatientStore 患者档案存在性查询契约（patients 表只读，写归 user-service）
type PatientStore interface {
	// PatientExists 返回 patients 表里有没有这个 patient_id。
	// 只判存在、不看 status：pending 档案同样可被运营查询，档案生命周期不等于数据可见性（T340）。
	PatientExists(ctx context.Context, patientID string) (bool, error)
}

// PatientRepo PatientStore 的 pgx 实现
type PatientRepo struct {
	pool *pgxpool.Pool
}

// NewPatientRepo 创建 PatientRepo
func NewPatientRepo(pool *pgxpool.Pool) *PatientRepo { return &PatientRepo{pool: pool} }

// PatientExists 查患者档案是否存在
func (r *PatientRepo) PatientExists(ctx context.Context, patientID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM patients WHERE patient_id = $1)`, patientID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
