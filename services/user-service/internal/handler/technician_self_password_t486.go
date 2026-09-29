// Package handler T486：技师本人自助修改登录口令。
//
// 为什么要这条通道：technicians.password_hash 只有两个写点——后台新建（T480 起带口令）
// 与管理员重置（T480）。技师登录后没有任何地方能改自己的口令，只能找管理员重置，
// 而重置会把口令打成随机串、再发一次明文，比自助改更麻烦。
//
// 与 T480 的分工（卡面口径）：重置=管理员（admin 通道 + requireAdminRole），
// 自助=本人（tech JWT + 只认 X-User-Id 这一行，body 里没有 techId 可传，故无从改他人）。
package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
)

// roleTechnician 技师 JWT 的角色值（signer.SignWithTeam 签发口径，与技术端小程序一致）
const roleTechnician = "technician"

// 新口令强度窗口。下限 6 不是随手取的：技术端小程序登录页自身校验的就是 6-16 位，
// 自助改密可接受的集合必须是它的子集，否则技师能设出一个登录页拒绝提交的口令、把自己锁在门外。
// 上限 16 对齐管理员重置通道生成的口令长度（T314/T480 发号器：前缀 + 12 位 + 后缀）。
const (
	techPasswordMinLen = 6
	techPasswordMaxLen = 16
)

// techChangePasswordRequest POST /api/v1/tech/change-password 请求体（camelCase，全仓 HTTP 入参口径）
type techChangePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// validTechPassword 长度落在窗口内、同时含字母与数字，且只含可打印 ASCII。
//
// 与管理员重置通道发出的口令相容（发号器必出字母+数字，且全 ASCII），
// 所以 T480 重置后的技师第一次自助改密不会被自己的现有口令卡住。
//
// ASCII 那一格不是洁癖：前端按「字符数」校验、Go 的 len() 是「字节数」，
// 一个汉字是 1 字符但 3 字节。不限定字符集时两侧能算出不同长度，
// 技师就可能设出一个登录页判「不足 6 位」的口令把自己锁在门外（口令写成功、登录被前端拦）。
// 限定可打印 ASCII 后「字符数 == 字节数」恒等，前后端同一条规则不会再分叉。
func validTechPassword(password string) bool {
	if len(password) < techPasswordMinLen || len(password) > techPasswordMaxLen {
		return false
	}
	var hasLetter, hasDigit bool
	for i := 0; i < len(password); i++ {
		ch := password[i]
		if ch < 0x21 || ch > 0x7e { // 非 ASCII、空格、控制符一并拒
			return false
		}
		switch {
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z'):
			hasLetter = true
		case ch >= '0' && ch <= '9':
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

// techChangePassword POST /api/v1/tech/change-password —— 技师改自己的登录口令。
//
// 判定序：角色门禁（fail-closed，兜住绕过网关直连服务的请求）→ 身份头 → 参数 →
// 取行 → 旧口令 bcrypt → 新口令强度 → 新旧不同 → 写。
// 写点排在全部校验之后 ⇒ 任一条被拒都零写，不会留下「口令换了一半」的行。
func (h *Handler) techChangePassword(c *gin.Context) {
	if c.GetHeader(headerRole) != roleTechnician {
		fail(c, model.ErrForbidden("only technician can change own password"))
		return
	}
	techID := c.GetHeader(headerUserID)
	if techID == "" {
		fail(c, model.ErrUnauthorized("missing technician identity"))
		return
	}
	var req techChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}

	tech, err := h.store.GetTechByTechID(c.Request.Context(), techID)
	if err != nil {
		fail(c, model.ErrInternal("query technician failed"))
		return
	}
	// 行不存在 / 从未设过口令（password_hash 为 NULL，读侧抹平成空串）都走同一条 401：
	// 不给「该技师号是否存在」留可分辨的差异（同 techLogin 的防枚举口径），
	// 且未设口令的账号本来就该由管理员走 T480 首次发号，不在自助通道里补。
	if tech == nil || tech.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(tech.PasswordHash), []byte(req.OldPassword)) != nil {
		fail(c, model.ErrInvalidCredentials("invalid old password"))
		return
	}
	if !validTechPassword(req.NewPassword) {
		fail(c, model.ErrInvalidParam("new password must be %d-%d printable ASCII chars with both letters and digits",
			techPasswordMinLen, techPasswordMaxLen))
		return
	}
	if req.NewPassword == req.OldPassword {
		fail(c, model.ErrInvalidParam("new password must differ from old password"))
		return
	}

	hash, err := GenerateBcryptHash([]byte(req.NewPassword))
	if err != nil {
		fail(c, model.ErrInternal("hash password failed"))
		return
	}
	// 复用 T480 那条写：只换 password_hash，启停/认证状态/团队归属都不被改密顺带改掉。
	if err := h.store.SetTechnicianPassword(c.Request.Context(), techID, hash); err != nil {
		fail(c, model.ErrInternal("change password failed"))
		return
	}

	// 🔴 审计与运行日志都不带口令（也不带哈希）：与管理员重置那条同口径。
	setAuditTrace(c, fmt.Sprintf("技师 %s 自助修改登录口令", techID), map[string]any{
		"changed": []string{"password_hash"},
	})
	ctxLogger(c).Info().
		Str("action", "change_technician_password").
		Str("operator_id", techID).
		Str("tech_id", techID).
		Msg("technician changed own password")

	ok(c, gin.H{"techId": techID})
}
