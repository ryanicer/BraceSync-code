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
	if req.PhoneToken != "" {
		claims, err := verifyPhoneToken(h.phoneTokenSecret, req.PhoneToken, currentOpenID, time.Now())
		if err != nil {
			fail(c, model.ErrInvalidPhoneToken("invalid phone token: %v", err))
			return
		}
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
		phoneHash = phone.Hash(pure)
	}

	// 步骤 3：按 phone_hash 查患者
	row, err := h.store.GetPatientByPhoneHash(c.Request.Context(), phoneHash)
	if err != nil {
		fail(c, model.ErrInternal("query patient by phone hash failed"))
		return
	}
	if row == nil || row.Status != "active" {
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
