// T378：文件域读侧/写侧归属判定的 handler 半边。
//
// 判定口径与 user-service、alert-service、device-service 同名 helper 一致
// （跨服务不共享代码，靠契约对齐）：只有 ROLE_DOCTOR 受团队收窄，
// ADMIN / CS / technician 等不受限角色的响应逐字不变（只收紧不放宽）。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/file-service/internal/repo"
)

// roleDoctor 受限角色：医护（团队成员的患者材料才可见）
const roleDoctor = "ROLE_DOCTOR"

// teamScope 医护调用者的数据范围。limited=false 表示不受限角色。
type teamScope struct {
	limited bool
	teamID  string
}

// resolveTeamScope 推导调用者团队范围。
// allowed=false 时已写好响应（401/500），调用方直接 return。
// 判定失败一律落 allowed=false 或空 teamID → 恒假收窄，绝不退化成「不过滤」。
func (h *FileHandler) resolveTeamScope(c *gin.Context) (teamScope, bool) {
	if c.GetHeader(headerRole) != roleDoctor {
		return teamScope{}, true
	}
	adminID := c.GetHeader(headerUserID)
	if adminID == "" {
		errorJSON(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "user identity missing")
		return teamScope{}, false
	}
	teamID, _, err := h.store.DoctorTeamByAdmin(c.Request.Context(), adminID)
	if err != nil {
		log.Error().Err(err).Str("admin_id", adminID).Msg("T378 resolve doctor team failed")
		errorJSON(c, http.StatusInternalServerError, ErrorCodeInternal, "error resolving data scope")
		return teamScope{}, false
	}
	return teamScope{limited: true, teamID: teamID}, true
}

// applyFileScope 把团队范围写进列表过滤条件（不受限角色保持零值=不过滤）。
func applyFileScope(f *repo.QueryFilter, scope teamScope) {
	if !scope.limited {
		return
	}
	f.TeamScoped = true
	f.TeamID = scope.teamID
}

// denyCrossTeam 跨团队访问统一响应：查无此文件与不属于本团队合一，
// fileID 存在性不作为探测面（与 device-service / user-service 同一形态）。
func denyCrossTeam(c *gin.Context, fileID string) {
	errorJSON(c, http.StatusForbidden, ErrorCodeForbidden,
		"not allowed to access file "+fileID)
}

// assertFileInView 单文件面（详情 / 下载 / 上传完成）归属门禁：探测排在任何读写之前。
// 返回 false 表示已写好响应，调用方直接 return。
func (h *FileHandler) assertFileInView(c *gin.Context, fileID string) bool {
	scope, allowed := h.resolveTeamScope(c)
	if !allowed {
		return false
	}
	if !scope.limited {
		return true
	}
	inTeam, err := h.store.FileOwnerInTeam(c.Request.Context(), fileID, scope.teamID)
	if err != nil {
		log.Error().Err(err).Str("file_id", fileID).Msg("T378 file scope probe failed")
		errorJSON(c, http.StatusInternalServerError, ErrorCodeInternal, "error resolving file scope")
		return false
	}
	if !inTeam {
		denyCrossTeam(c, fileID)
		return false
	}
	return true
}

// assertPresignOwnerAllowed presign 写侧归属门禁：医护只能为患者维度材料中
// 本团队的患者/告警开上传通道。判定在任何落库（CreateFile 登记 pending 行）之前，
// 拒绝路径下库内零变更。
func (h *FileHandler) assertPresignOwnerAllowed(c *gin.Context, ownerType, ownerID string) bool {
	scope, allowed := h.resolveTeamScope(c)
	if !allowed {
		return false
	}
	if !scope.limited {
		return true
	}
	inTeam, err := h.store.OwnerInTeam(c.Request.Context(), ownerType, ownerID, scope.teamID)
	if err != nil {
		log.Error().Err(err).Str("owner_type", ownerType).Str("owner_id", ownerID).
			Msg("T378 presign owner scope probe failed")
		errorJSON(c, http.StatusInternalServerError, ErrorCodeInternal, "error resolving upload scope")
		return false
	}
	if !inTeam {
		denyCrossTeam(c, ownerID)
		return false
	}
	return true
}
