// T378：设备域读侧归属判定的 repo 半边（devices / install_records）。
//
// 与 user-service、alert-service 同名 helper 同一口径（跨服务不共享代码，靠契约对齐）：
// 团队唯一事实源是 doctors.team_id，登录身份是 admins.admin_id，故必须走 doctors.admin_id 这一跳。
// device-service 对 doctors / patients 只读（写归 user-service）。
package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ListScope 管理端列表读侧团队范围（T378）。
//
// TeamScoped=false：不受限角色（运营 / 客服），SQL 不加团队谓词，响应逐字不变。
// TeamScoped=true 且 TeamID 为空：无团队归属的医护，落恒假谓词 → 空集 + total 0，
// 绝不退化成「不过滤」。
type ListScope struct {
	TeamID     string
	TeamScoped bool
}

// DoctorTeamByAdmin admin_id → 医护所属团队 team_id。
// 无 doctor 行 / team_id 为 NULL 或空串都返回 ok=false，teamID 为空串；
// 调用方（handler）据此落空集，绝不退化成「不按团队过滤」。
func (r *PGStore) DoctorTeamByAdmin(ctx context.Context, adminID string) (string, bool, error) {
	var teamID string
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(team_id, '') FROM doctors WHERE admin_id = $1`, adminID).Scan(&teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return teamID, teamID != "", nil
}

// DeviceInTeam 设备是否归属该团队的患者（单资源读侧的只读探测，T378）。
//
// 四格合一律 false，不区分「设备不存在 / 患者在他团队 / 患者未分配团队 / 设备未绑定患者」——
// 否则 deviceId 存在性可被当作探测 oracle。teamID 为空时不下库直接 false（调用者无归属）。
func (r *PGStore) DeviceInTeam(ctx context.Context, deviceID, teamID string) (bool, error) {
	if teamID == "" {
		return false, nil
	}
	var one int
	err := r.pool.QueryRow(ctx, `
SELECT 1 FROM devices d
 JOIN patients p ON p.patient_id = d.patient_id
WHERE d.device_id = $1 AND p.team_id = $2`, deviceID, teamID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InstallInTeam 安装记录是否归属该团队的患者（口径同 DeviceInTeam，T378）。
func (r *PGStore) InstallInTeam(ctx context.Context, installID int64, teamID string) (bool, error) {
	if teamID == "" {
		return false, nil
	}
	var one int
	err := r.pool.QueryRow(ctx, `
SELECT 1 FROM install_records i
 JOIN patients p ON p.patient_id = i.patient_id
WHERE i.install_id = $1 AND p.team_id = $2`, installID, teamID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
