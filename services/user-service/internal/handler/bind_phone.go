package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/wechat"
)

// bindPhoneRequest bind-phone 请求体（phone_code / phone_token 二选一，同传 phone_token 优先）
type bindPhoneRequest struct {
	PhoneCode  string `json:"phone_code"`
	PhoneToken string `json:"phone_token"`
}

// bindPhone T085 患者微信绑定手机号（六步流程）。
// scope=bind 由 scopeGuard 前置保证；本 handler 实现：
//  1. 参数校验（phone_code / phone_token 至少一个）
//  2. phoneToken 优先：校验签名/purpose/openid/exp，通过则取 claims.phone_hash（零微信调用）
//     失败 → 10605；否则用 phone_code 调微信 GetPhoneNumber（业务错误 → 10604）
//  3. 按 phone_hash 查患者；无匹配或 status!=active → 10602 + phoneToken
//  4. 查患者 wx_openid：==当前 openid 幂等成功；!=空(他人) → 10603 + phoneToken；空 → 绑定
//  5. BindPatientOpenid（原子行锁；0 行 → 10603 并发抢占）
//  6. 签发正式 JWT（sub=patientID，8h）
//
// T156：步骤 2 拿到微信返回的 pure 后必须先 phone.NormalizeHash 再查库。直接 phone.Hash(pure)
// 在微信某些边界下（purePhoneNumber 带 +86/86 前缀、含不可见字符）会算出与 DB 不一致的 hash，
// 触发 10602 patient_not_found（误诊为档案不存在）。phoneToken 路径取 claims.phone_hash 不
// 走外部输入，保持原 phone.Hash 行为（claims 来源于服务端自身，安全可信）。
func (h *Handler) bindPhone(c *gin.Context) {
	openID, _ := c.Get("subject") // scopeGuard 已注入（scope=bind 时 sub=openid）
	currentOpenID, _ := openID.(string)

	var req bindPhoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if req.PhoneCode == "" && req.PhoneToken == "" {
		fail(c, model.ErrInvalidParam("phone_code or phone_token is required"))
		return
	}

	// 步骤 2：解析手机号 hash（phoneToken 优先，零微信调用）
	var phoneHash string
	var pureForLog string // T156：失败日志使用，便于定位是否 pure 带 +86/不可见字符
	if req.PhoneToken != "" {
		claims, err := verifyPhoneToken(h.phoneTokenSecret, req.PhoneToken, currentOpenID, time.Now())
		if err != nil {
			fail(c, model.ErrInvalidPhoneToken("invalid phone token: %v", err))
			return
		}
		// phoneToken 来源于服务端自身签发（含 Normalize 后的 hash），安全可信，无需再 Normalize
		phoneHash = claims.PhoneHash
	} else {
		if h.wxClient == nil {
			fail(c, model.ErrInternal("wechat client not configured"))
			return
		}
		pure, _, err := h.wxClient.GetPhoneNumber(c.Request.Context(), req.PhoneCode)
		if err != nil {
			var we *wechat.WechatError
			if errors.As(err, &we) {
				fail(c, model.ErrInvalidPhoneCode("wechat getPhoneNumber failed: errcode=%d", we.ErrCode))
				return
			}
			fail(c, model.NewWXServiceUnavailable("wechat service unavailable"))
			return
		}
		// T156：先 Normalize 再 Hash，防止微信返回带 +86/不可见字符导致与 DB hash 不一致
		pureForLog = pure
		phoneHash = phone.NormalizeHash(pure)
	}

	// 步骤 3：按 phone_hash 查患者
	row, err := h.store.GetPatientByPhoneHash(c.Request.Context(), phoneHash)
	if err != nil {
		ctxLogger(c).Error().Err(err).Str("phone_hash", phoneHash).Msg("bind-phone: query patient by phone hash failed")
		fail(c, model.ErrInternal("query patient by phone hash failed"))
		return
	}
	if row == nil || row.Status != "active" {
		// T156：失败分支打 Info 级结构化日志，包含原始 pure（若有）与最终 hash，
		// 便于将来复发 10602 时直接定位是微信侧返回格式异常 / DB 数据缺失 / 其它原因。
		ctxLogger(c).Info().
			Str("phone_hash", phoneHash).
			Str("pure_from_wechat", pureForLog).
			Str("openid", currentOpenID).
			Msg("bind-phone: patient not found or inactive (10602)")
		// 无匹配或非 active 同码 10602 防枚举；返回 phoneToken 供重试
		h.respondWithPhoneToken(c, model.CodePatientNotFound, "patient not found or inactive", phoneHash, currentOpenID)
		return
	}

	// 步骤 4：查患者当前 wx_openid
	boundOpenID, err := h.store.GetPatientWXOpenID(c.Request.Context(), row.PatientID)
	if err != nil {
		fail(c, model.ErrInternal("query patient wx_openid failed"))
		return
	}
	if boundOpenID == currentOpenID {
		// 幂等：已绑定到当前 openid → 直接签发正式 JWT
		h.respondLoginOK(c, row)
		return
	}
	if boundOpenID != "" {
		// 已绑定其他微信 → 10603 + phoneToken（禁止覆盖）
		h.respondWithPhoneToken(c, model.CodePhoneAlreadyBound, "phone already bound to another wechat", phoneHash, currentOpenID)
		return
	}

	// 步骤 5：原子绑定（行锁语义；并发下仅 1 个成功）
	if err := h.store.BindPatientOpenid(c.Request.Context(), row.PatientID, currentOpenID); err != nil {
		if errors.Is(err, repo.ErrAlreadyBound) {
			// 并发抢占：另一请求已绑定 → 10603 + phoneToken
			h.respondWithPhoneToken(c, model.CodePhoneAlreadyBound, "phone already bound to another wechat", phoneHash, currentOpenID)
			return
		}
		fail(c, model.ErrInternal("bind openid failed"))
		return
	}

	// 步骤 6：签发正式 JWT
	h.respondLoginOK(c, row)
}

// respondWithPhoneToken 失败分支统一响应：code + phoneToken（供客户端重试免二次微信调用）
func (h *Handler) respondWithPhoneToken(c *gin.Context, code int, msg, phoneHash, openID string) {
	pt := issuePhoneToken(h.phoneTokenSecret, phoneHash, openID, time.Now())
	c.JSON(http.StatusOK, jsonResp{
		Code:    code,
		Message: msg,
		Data:    gin.H{"phone_token": pt},
	})
}

// respondLoginOK 绑定成功响应：签发正式 JWT（sub=patientID，8h）
func (h *Handler) respondLoginOK(c *gin.Context, row *repo.PatientLoginRow) {
	if h.signer == nil {
		fail(c, model.ErrInternal("JWT_SECRET not configured"))
		return
	}
	tk, err := h.signer.SignWithTeam(row.PatientID, row.Name, "", "patient")
	if err != nil {
		fail(c, model.ErrInternal("sign token failed"))
		return
	}
	ok(c, model.LoginResultDTO{
		Token:  tk,
		Name:   row.Name,
		RoleID: "patient",
	})
}
