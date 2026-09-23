// T350：告警读链路的医护团队归属查询。
//
// 团队只挂在 doctors 行上，而登录身份（JWT sub / gateway 注入的 X-User-Id）是
// admins.admin_id，故必须走 doctors.admin_id 这一跳（与 user-service 的
// DoctorTeamByAdmin 同一条 SQL 口径）。alert-service 对 doctors 表只读（写归 user-service）。
package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// DoctorTeamByAdmin admin_id → 医护所属团队 team_id。
// 无 doctor 行 / team_id 为 NULL 或空串都返回 ok=false，teamID 为空串；
// 调用方（handler）据此落空集，绝不退化成「不按团队过滤」。
func (r *PGAlertRepo) DoctorTeamByAdmin(ctx context.Context, adminID string) (string, bool, error) {
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
