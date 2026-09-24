// T378：文件域归属判定的 repo 半边。
//
// 与 user-service、alert-service、device-service 同名 helper 同一口径
// （跨服务不共享代码，靠契约对齐）：团队唯一事实源是 doctors.team_id，
// 登录身份是 admins.admin_id，故必须走 doctors.admin_id 这一跳。
// file-service 对 patients / alerts / doctors 只读（写归各自 owner 服务）。
//
// 文件本身不带团队列，归属靠 owner 反查：owner_type 为 'patient'（患者维度材料，
// 复查报告）或 'alert'（告警附件，alerts.patient_id 二级反查）时才受团队约束；
// 其余 owner_type（'ReviewTemplate' 等配置类材料）不是患者数据，不受团队范围限制。
package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// patientScopedTypes 需要按患者团队收窄的 owner_type（其余如 ReviewTemplate 不是患者数据）。
const patientScopedTypes = `'patient','alert'`

// teamScopedCond 生成「(ownerTypeCol, ownerIDCol) 这对 owner 列指向的患者属于 teamID」子句。
// ownerTypeCol / ownerIDCol 传裸列名（外层 files 未起别名）或参数占位符。
//
// 非患者材料整条放行；患者行缺失 / team_id 为 NULL 都不成立 → 收窄生效时一并排除（fail-closed）。
// 列表（QueryFiles/CountFiles）与单文件探测（FileOwnerInTeam）共用本子句，
// 避免「列表看得见、详情读不到」这类两口径。
func teamScopedCond(ownerTypeCol, ownerIDCol string, teamParam int) string {
	return fmt.Sprintf(`(%[1]s NOT IN (`+patientScopedTypes+`)
	   OR EXISTS (SELECT 1 FROM patients p
	               WHERE p.patient_id = %[2]s AND p.team_id = $%[3]d)
	   OR EXISTS (SELECT 1 FROM alerts a
	                 JOIN patients p2 ON p2.patient_id = a.patient_id
	                WHERE a.alert_id::text = %[2]s AND p2.team_id = $%[3]d))`,
		ownerTypeCol, ownerIDCol, teamParam)
}

// DoctorTeamByAdmin admin_id → 医护所属团队 team_id。
// 无 doctor 行 / team_id 为 NULL 或空串都返回 ok=false，teamID 为空串；
// 调用方（handler）据此落空集，绝不退化成「不按团队过滤」。
func (s *PGStore) DoctorTeamByAdmin(ctx context.Context, adminID string) (string, bool, error) {
	var teamID string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(team_id, '') FROM doctors WHERE admin_id = $1`, adminID).Scan(&teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return teamID, teamID != "", nil
}

// FileOwnerInTeam file_id → 该文件的 owner 是否落在 teamID 团队内。
// 查无此文件与跨团队合一返回 false（存在性不可作为探测面）。
//
// 刻意不对 teamID 做空串短路：无团队医护仍应看得到非患者材料（模板），
// 短路会与列表 teamScopedCond 的「NOT IN 患者材料即放行」分叉成两套口径。
func (s *PGStore) FileOwnerInTeam(ctx context.Context, fileID, teamID string) (bool, error) {
	query := `SELECT 1 FROM files WHERE file_id = $1 AND ` +
		teamScopedCond("owner_type", "owner_id", 2)
	var one int
	err := s.pool.QueryRow(ctx, query, fileID, teamID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// PatientInTeam patient_id 是否属于 teamID（presign 写侧归属探测）。
// 无患者行 / team_id 为 NULL / teamID 为空都返回 false。
func (s *PGStore) PatientInTeam(ctx context.Context, patientID, teamID string) (bool, error) {
	if teamID == "" {
		return false, nil
	}
	var one int
	err := s.pool.QueryRow(ctx, `
SELECT 1 FROM patients WHERE patient_id = $1 AND team_id = $2`, patientID, teamID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// AlertInTeam alert_id（文本形态）所指告警的患者是否属于 teamID。
func (s *PGStore) AlertInTeam(ctx context.Context, alertID, teamID string) (bool, error) {
	if teamID == "" {
		return false, nil
	}
	var one int
	err := s.pool.QueryRow(ctx, `
SELECT 1 FROM alerts a
 JOIN patients p ON p.patient_id = a.patient_id
WHERE a.alert_id::text = $1 AND p.team_id = $2`, alertID, teamID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// OwnerInTeam 按 owner_type 分派到对应归属探测（presign 写侧）。
// 非患者材料（ReviewTemplate 等）恒 true：模板上传不是患者数据。
func (s *PGStore) OwnerInTeam(ctx context.Context, ownerType, ownerID, teamID string) (bool, error) {
	switch ownerType {
	case "patient":
		return s.PatientInTeam(ctx, ownerID, teamID)
	case "alert":
		return s.AlertInTeam(ctx, ownerID, teamID)
	default:
		return true, nil
	}
}
