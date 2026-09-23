package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PatientStore 患者档案存在性查询契约（patients 表只读，写归 user-service）
type PatientStore interface {
	// PatientExists 返回 patients 表里有没有这个 patient_id。
	// 只判存在、不看 status：pending 档案同样可被运营查询，档案生命周期不等于数据可见性（T340）。
	PatientExists(ctx context.Context, patientID string) (bool, error)
	// PatientInAdminTeam T350：该患者是否落在这名医护（admins.admin_id）的所属团队内。
	// 假 = 越权或档案不存在，由 handler 统一按 403 处理（fail-closed）。
	PatientInAdminTeam(ctx context.Context, patientID, adminID string) (bool, error)
	// DoctorTeamByAdmin T350：admin_id → 医护所属团队 team_id（Dashboard 聚合范围推导用）。
	// 无 doctor 行或 team_id 为 NULL/空串一律 ok=false，调用方按空集处理。
	DoctorTeamByAdmin(ctx context.Context, adminID string) (teamID string, ok bool, err error)
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

// PatientInAdminTeam T350：医护（admins.admin_id）与患者的团队归属是否对得上。
//
// 一条 SQL 同时覆盖三种「不可见」，故返回值不区分原因，调用方一律 403：
//   - patients 无此行（不给无权方泄露「这个患者存不存在」，与 T340 同口径）
//   - admins.admin_id 在 doctors 里无行（技师 / 客服 / 无医护档案的账号）
//   - doctors.team_id 为 NULL 或 patients.team_id 为 NULL（未分配团队 = 不属于任何团队）
//
// 团队的事实源只有 doctors.team_id；X-User-Id 由 gateway 从 JWT 注入，客户端伪造不到。
func (r *PatientRepo) PatientInAdminTeam(ctx context.Context, patientID, adminID string) (bool, error) {
	var allowed bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		    FROM patients p
		    JOIN doctors d ON d.admin_id = $2
		   WHERE p.patient_id = $1
		     AND d.team_id IS NOT NULL
		     AND p.team_id = d.team_id)`, patientID, adminID).Scan(&allowed)
	if err != nil {
		return false, err
	}
	return allowed, nil
}

// DoctorTeamByAdmin T350：admin_id → 所属团队（Dashboard 聚合按团队收口时的过滤值）。
// ok=false（无 doctor 行 / team_id 为 NULL 或空串）时 teamID 为空串，由调用方落空集。
func (r *PatientRepo) DoctorTeamByAdmin(ctx context.Context, adminID string) (string, bool, error) {
	var teamID string
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(team_id, '') FROM doctors WHERE admin_id = $1`, adminID).Scan(&teamID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return teamID, teamID != "", nil
}
