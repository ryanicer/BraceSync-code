// T378：设备域读侧归属判定在服务端落地（devices / install_records）。
//
// 这一族既不在 #205（T350）的枚举里，也不在 #208（T373）的三处里：任意医护令牌能
// ① 拉全院设备列表与全院安装记录（含患者姓名、绑定关系、安装备注与签名图链接），
// ② 按 deviceId / installId 逐条读他团队患者的详情 —— 后者还兼作存在性探测 oracle。
//
// 本文件不重新推导范围：身份 → 团队的推导复用与 #205 同口径的 resolveTeamScope
// （device-service 侧独立一份代码，跨服务不共享，靠契约对齐 —— 同 alert-service 的
// team_scope_t350.go）。列表与单资源两条路只是把同一个 teamScope 换算成各自入参。
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

// roleDoctor 医护角色键（与 presetRoles、网关 rbac.go 字面量一致，枚举键不改）
const roleDoctor = "ROLE_DOCTOR"

// teamScope 调用者的团队数据范围（口径同 user-service / alert-service 的同名类型）。
type teamScope struct {
	// limited 为 false 表示该角色不受团队限制（运营 / 客服 / 技师 / 患者），维持原有语义。
	limited bool
	// teamID 所属团队。limited 为真且 teamID 为空 = 该医护账号无团队归属，
	// 按 fail-closed 处理：列表返回空集、单资源返回 403，绝不退化成「不过滤」。
	teamID string
}

// resolveTeamScope 从身份头推导团队范围。
// ok 为 false 表示响应已写出（403 身份缺失 / 500 解析失败），调用方必须直接 return。
func (h *Handler) resolveTeamScope(c *gin.Context) (teamScope, bool) {
	if c.GetHeader(headerRole) != roleDoctor {
		return teamScope{}, true
	}
	adminID := c.GetHeader(headerUserID)
	if adminID == "" {
		fail(c, model.ErrForbidden("missing user identity"))
		return teamScope{}, false
	}
	if h.list == nil {
		// 数据源缺失时无法判定归属，受限身份一律拒（不退化为放行）。
		fail(c, model.ErrInternal("scope store not configured"))
		return teamScope{}, false
	}
	teamID, _, err := h.list.DoctorTeamByAdmin(c.Request.Context(), adminID)
	if err != nil {
		fail(c, model.ErrInternal("resolve doctor team failed"))
		return teamScope{}, false
	}
	return teamScope{limited: true, teamID: teamID}, true
}

// listReadScope 把 teamScope 换算成设备/安装记录列表的读侧范围。
//
// 只收紧不放宽：不受限角色得到 TeamScoped=false，repo 侧不加团队谓词，响应逐字不变。
func listReadScope(scope teamScope) repo.ListScope {
	if !scope.limited {
		return repo.ListScope{}
	}
	return repo.ListScope{TeamScoped: true, TeamID: scope.teamID}
}

// denyCrossTeam 受限身份下的单资源越界一律 403，且「资源不存在 / 患者在他团队 /
// 患者未分配团队 / 设备未绑定患者」四格合一 ⇒ deviceId、installId 存在性不可辨
// （口径同 #205 的 getPatient 与 #208 的 A1）。不受限角色不进这条路径，仍吃原 404。
func denyCrossTeam(c *gin.Context, kind, id string) {
	fail(c, model.ErrForbidden("%s %s is out of your data scope", kind, id))
}

// assertDeviceInScope 设备详情 / 绑定历史（GET /devices/:deviceId[/bindings]）的读侧门禁。
func (h *Handler) assertDeviceInScope(c *gin.Context, deviceID string) bool {
	scope, allowed := h.resolveTeamScope(c)
	if !allowed {
		return false
	}
	if !scope.limited {
		return true
	}
	inTeam, err := h.list.DeviceInTeam(c.Request.Context(), deviceID, scope.teamID)
	if err != nil {
		fail(c, model.ErrInternal("resolve device scope failed"))
		return false
	}
	if !inTeam {
		denyCrossTeam(c, "device", deviceID)
		return false
	}
	return true
}

// assertInstallInScope 安装记录详情（GET /install-records/:id）的读侧门禁。
// id 已由调用方解析为正整数；非法 id 在门禁之前就已被原校验拒掉。
func (h *Handler) assertInstallInScope(c *gin.Context, installID int64) bool {
	scope, allowed := h.resolveTeamScope(c)
	if !allowed {
		return false
	}
	if !scope.limited {
		return true
	}
	inTeam, err := h.list.InstallInTeam(c.Request.Context(), installID, scope.teamID)
	if err != nil {
		fail(c, model.ErrInternal("resolve install scope failed"))
		return false
	}
	if !inTeam {
		denyCrossTeam(c, "install record", strconv.FormatInt(installID, 10))
		return false
	}
	return true
}
