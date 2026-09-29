// Package handler T487：后台账号自助改密（管理员与医护/客服均可，凭旧密码改自己的）。
//
// 为什么已有代改通道还要这条：管理员重置早就有（T314 医护 reset-password、T480 技师重置、T477 患者设密），
// 那些都是「别人替你生成一个」——口令由服务端产、一次性回显、用户没得选。
// 本卡要的是「本人知道自己现在的口令，想换成自己的」，判据也按这条写（旧密码错必拒、改完旧密码失效）。
//
// 🔴 身份只取网关注入的 X-User-Id，请求体不含任何账号标识字段：
//
//	jwtAuth 先删外部同名头再从 JWT claims 重签（services/gateway/cmd/server/middleware.go:80-81,110-111），
//	所以这个值是「签发时刻的登录者本人」。若从 body 收 adminId，任何人带上别人的 ID 就能改别人密码。
//
// 🔴 「旧密码错误」不能回 401/10401：admin-web 的会话失效判定是「HTTP 401 一律清令牌、整页回登录页」
//
//	（apps/admin-web/src/utils/sessionExpiry.ts:33-36 isAuthExpired，request.ts:69 命中即 expiredSession）。
//	改密弹窗里填错一次旧密码就被登出，比「改不了」更难懂。401 只留给「根本没有身份」那一格（未登录本就该重登）。
package handler

import (
	"fmt"
	"unicode"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// 新密码长度边界。用**字节**数不是字符数：bcrypt 只吃前 72 字节，
// 上限 64 留出余量，避免「用户以为设了 80 位密码、实际生效的是被截断的前 72 字节」这种静默降级。
const (
	adminPasswordMinBytes = 8
	adminPasswordMaxBytes = 64
)

// validNewAdminPassword 自助改密的新密码强度判定。
//
// 规则取「字母 + 数字 + 长度」三件，锚点是这条不变量（有单测锁）：
//
//	管理员代重置生成的口令必须能通过本校验 —— genDoctorPassword 产 16 位「Br+12 位字母数字+#7」，
//	医护拿到它登进去后想立刻自助改，校验若比生成规则更严，就等于「先发一个自己改不掉的密码」。
//
// 不额外要求符号位：那是给用户的填写负担，且生成侧的 #7 已满足本规则，加严只会让两侧更难对齐。
// 大小写/非 ASCII 字母按 unicode 计（中文口令里的汉字算字母位，不因非 ASCII 被拒）。
func validNewAdminPassword(pw string) bool {
	if len(pw) < adminPasswordMinBytes || len(pw) > adminPasswordMaxBytes {
		return false
	}
	var hasLetter, hasDigit bool
	for _, r := range pw {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

// changePassword POST /api/v1/auth/change-password —— 后台账号本人改密。
//
// 判定序：身份 → 参数 → 强度 → 存在性 → 状态 → 旧密码 → 新旧不同 → 写。
// 前四步都不触库（强度是纯字符串判定），未知身份/禁用账号在写之前就被截住，
// 不会留下「哈希已换但响应报错」的半成品。
func (h *Handler) changePassword(c *gin.Context) {
	adminID := c.GetHeader(headerUserID)
	if adminID == "" {
		// 未登录那一格：直连服务（绕过 gateway）或身份头被剥离时走这里，前端按会话失效处理。
		fail(c, model.ErrUnauthorized("missing identity"))
		return
	}
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		fail(c, model.ErrInvalidParam("old_password and new_password are required"))
		return
	}
	if !validNewAdminPassword(req.NewPassword) {
		fail(c, model.ErrInvalidParam(
			"weak new password: need %d-%d bytes with at least one letter and one digit",
			adminPasswordMinBytes, adminPasswordMaxBytes))
		return
	}

	admin, err := h.store.GetAdminByID(c.Request.Context(), adminID)
	if err != nil {
		fail(c, model.ErrInternal("query admin failed"))
		return
	}
	if admin == nil {
		// X-User-Id 指向的不是 admins 行：技师/患者令牌打这条后台端点落到这里（404 不是 500）。
		fail(c, model.ErrNotFound("admin account not found: %s", adminID))
		return
	}
	if admin.Status != accountStatusEnabled {
		fail(c, model.ErrForbidden("account disabled, cannot change password"))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.OldPassword)) != nil {
		fail(c, model.ErrInvalidParam("old password mismatch"))
		return
	}
	// 新旧同值必须在写之前拒：否则「改密成功」与「密码没变」两件事同时成立，
	// 且下面那次渐进式重哈希逻辑会被自己绕过。
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.NewPassword)) == nil {
		fail(c, model.ErrInvalidParam("new password must differ from the current one"))
		return
	}

	newHash, err := GenerateBcryptHash([]byte(req.NewPassword))
	if err != nil {
		fail(c, model.ErrInternal("hash password failed"))
		return
	}
	if err := h.store.UpdateAdminPasswordHash(c.Request.Context(), adminID, newHash); err != nil {
		fail(c, model.ErrInternal("update password failed"))
		return
	}

	// 🔴 留痕不带口令也不带哈希（同 T314/T477/T480 三条写口令端点口径）；
	// 本条不走 auditRoutes 表驱动：表内 target_id 只能取路径参数，而这条的目标是「本人」，
	// 只有 handler 手里的 X-User-Id 拿得到 ⇒ 就地埋点，一次请求仍只写一行。
	h.audit(c, repo.AuditInput{
		OperatorID:   adminID,
		OperatorRole: admin.RoleID,
		Action:       auditActionDataModify,
		TargetType:   "admin",
		TargetID:     adminID,
		Description:  fmt.Sprintf("自助修改登录密码：%s", admin.Username),
		Detail:       map[string]any{"changed": []string{"password_hash"}, "self": true},
	})

	ctxLogger(c).Info().
		Str("action", "change_password").
		Str("operator_id", adminID).
		Str("role", admin.RoleID).
		Msg("admin self-service password changed")

	ok(c, nil)
}
