// T378：设备域读侧归属判定的 repo 半边（devices / install_records）。
//
// 与 user-service、alert-service 同名 helper 同一口径（跨服务不共享代码，靠契约对齐）：
// 团队唯一事实源是 doctors.team_id，登录身份是 admins.admin_id，故必须走 doctors.admin_id 这一跳。
// device-service 对 doctors / patients 只读（写归 user-service）。
package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// 团队归属谓词的唯一出处：设备族（ListDevices + DeviceInTeam）与安装记录族
// （ListInstallRecords + InstallInTeam）各一段文本，四处在同一处定义 ——
// 列表与单资源探测拿到的是同一段字符，「列表可见 ⇒ 详情可读」由构造保证而非两条 SQL 恰好同形。
//
// 两段文本都以 `%[1]d` 收占位符序号（由 listPredicates 按参数追加顺序推导，不得写死；
// 探测侧固定 $2），且**别名固定为 d（devices）/ p（该族 JOIN 出来的患者行：设备族是现绑患者、
// 安装记录族是记录自己的患者）**：两条列表与两条探测的 FROM 用的就是这两个别名，
// 改别名必须同步这里，否则是运行时报错而不是静默放行。
const (
	// deviceTeamCondFmt 设备族：现绑患者属本团队 OR 该设备被本团队患者的安装记录引用过。
	//
	// 后半支是 T403 乙案（Boss 09-27 17:31 拍「列表可见 = 详情可读」口径归一）加进来的
	// 历史锚点。install_records.patient_id 是安装当时的患者、写侧从不回填（见 InstallInTeam
	// 注释），设备转绑/解绑后它与 devices.patient_id 分叉 —— 只按现绑过滤就会出现
	// 「本团队安装记录列得出来、点它引用的设备 403」（T386 观察项二）。
	// 列表与探测共用这一段 ⇒ 两口径不可能再各自成立。
	//
	// 代价（Boss 裁定时已列明，评论 1816 第三节乙案）：现绑患者属他团队的那台设备，
	// 其 patientId / patientName / wifi_ssid / bindTime / lastReportAt 会交给本团队医护，
	// 共用同一探测的 GET /devices/:id/bindings 还会交出该设备历次绑定患者号。
	deviceTeamCondFmt = `(p.team_id = $%[1]d OR EXISTS (
	         SELECT 1 FROM install_records ir
	           JOIN patients ip ON ip.patient_id = ir.patient_id
	          WHERE ir.device_id = d.device_id AND ip.team_id = $%[1]d))`

	// installTeamCondFmt 安装记录族：只按记录自己的患者定团队（T403 甲案钉死的历史锚点，
	// 乙案不牵连它 —— 反向放宽会把别团队的安装备注/签名图链接拉进本团队列表）。
	installTeamCondFmt = `p.team_id = $%[1]d`
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

// DeviceInTeam 设备是否对该团队可读（单资源读侧的只读探测，T378）。
//
// T403 乙案（Boss 09-27 17:31 拍「列表可见 = 详情可读」）：条件改用 deviceTeamCondFmt，
// 与 ListDevices 的列表谓词是同一段文本 ⇒ 不再出现「列表里有这台、点详情 403」。
// 不区分「设备不存在 / 现绑患者在他团队且从无本团队安装记录 / 患者未分配团队」——
// 否则 deviceId 存在性可被当作探测 oracle。teamID 为空时不下库直接 false（调用者无归属）。
func (r *PGStore) DeviceInTeam(ctx context.Context, deviceID, teamID string) (bool, error) {
	if teamID == "" {
		return false, nil
	}
	var one int
	err := r.pool.QueryRow(ctx, `
SELECT 1 FROM devices d
 LEFT JOIN patients p ON p.patient_id = d.patient_id
WHERE d.device_id = $1 AND `+fmt.Sprintf(deviceTeamCondFmt, 2), deviceID, teamID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InstallInTeam 安装记录是否归属该团队的患者（单资源读侧只读探测，T378）。
//
// 锚点是 install_records.patient_id（安装当时的历史患者），不是 devices.patient_id。
// 写侧从不回填这一列（repo.go 只 UPDATE devices.patient_id），故设备转绑后两条锚点会分叉。
//
// 🔴 与 DeviceInTeam 的口径**不再相同**（T403 乙案起设备族是两段 union，本族仍是单段历史锚点
// installTeamCondFmt）：本族不跟随放宽，否则别团队的安装备注 / 签名图链接会被拉进本团队列表。
// 结论是「安装记录的可见集 ⊆ 设备的可见集」，反向不成立 —— 门禁见
// team_scope_t403_integration_test.go（同锚一致性 + 跨资源非对称两格都在那里锁）。
// teamID 为空时不下库直接 false（调用者无归属），口径同 DeviceInTeam。
func (r *PGStore) InstallInTeam(ctx context.Context, installID int64, teamID string) (bool, error) {
	if teamID == "" {
		return false, nil
	}
	var one int
	err := r.pool.QueryRow(ctx, `
SELECT 1 FROM install_records i
 JOIN patients p ON p.patient_id = i.patient_id
WHERE i.install_id = $1 AND `+fmt.Sprintf(installTeamCondFmt, 2), installID, teamID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
