package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
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
//
// T159：scope=bind 时 JWT sub="openid_<raw>"（网关 scopeAuthz 据此前缀放行 bind-phone）；
// 本 handler 第一步用 stripScopeBindPrefix 还原成 raw openid 后与 DB / phoneToken.openid 对齐。
func (h *Handler) bindPhone(c *gin.Context) {
	openID, _ := c.Get("subject") // scopeGuard 已注入（scope=bind 时 sub="openid_<raw>"）
	bareSubject, _ := openID.(string)
	// T159：剥 scopeBindPrefix 还原 raw openid，与 DB patients.wx_openid / phoneToken.openid 字段对齐。
	currentOpenID := stripScopeBindPrefix(bareSubject)

	// T159-panic-502 排查：handler 入口 Info 日志；此前缺这条导致「user-service 没收到请求」
	// 还是「handler 入口就 panic 但 panic recover 屏蔽日志」无法判定。
	ctxLogger(c).Info().
		Str("path", c.Request.URL.Path).
		Str("method", c.Request.Method).
		Str("remote_addr", c.ClientIP()).
		Str("auth_header_prefix", func() string {
			h := c.GetHeader("Authorization")
			if len(h) > 20 {
				return h[:20] + "...(truncated)"
			}
			return h
		}()).
		Str("subject_raw", bareSubject).
		Str("openid_after_strip", currentOpenID).
		Str("wx_client_nil", func() string {
			if h.wxClient == nil {
				return "true"
			}
			return "false"
		}()).
		Bool("phone_token_secret_set", h.phoneTokenSecret != "").
		Msg("bind-phone: handler entered (T159-dbg)")

	var req bindPhoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctxLogger(c).Info().Err(err).Msg("bind-phone: ShouldBindJSON failed (T159-dbg)")
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	ctxLogger(c).Info().
		Bool("has_phone_code", req.PhoneCode != "").
		Bool("has_phone_token", req.PhoneToken != "").
		Int("phone_code_len", len(req.PhoneCode)).
		Int("phone_token_len", len(req.PhoneToken)).
		Msg("bind-phone: request parsed (T159-dbg)")
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
				ctxLogger(c).Info().
					Int("wx_errcode", we.ErrCode).
					Msg("bind-phone: wechat biz error → 10604 (T166-dbg)")
				fail(c, model.ErrInvalidPhoneCode("wechat getPhoneNumber failed: errcode=%d", we.ErrCode))
				return
			}
			// T166：本分支此前零日志，正是「handler 已 parsed 却无出口日志、外部只见 502」的盲区。
			// 必须记 wechatErrText(err) 而不是 err 本身——见该函数注释，原始 URL 里带 AppSecret。
			ctxLogger(c).Error().
				Str("wechat_err", wechatErrText(err)).
				Msg("bind-phone: wechat GetPhoneNumber failed → 10502 (T166-dbg)")
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
	ctxLogger(c).Info().
		Str("patient_id", row.PatientID).
		Str("patient_status", row.Status).
		Str("phone_hash", phoneHash).
		Msg("bind-phone: patient matched (T159-dbg)")

	// 步骤 4：查患者当前 wx_openid
	boundOpenID, err := h.store.GetPatientWXOpenID(c.Request.Context(), row.PatientID)
	if err != nil {
		ctxLogger(c).Info().Err(err).Str("patient_id", row.PatientID).Msg("bind-phone: query wx_openid failed (T159-dbg)")
		fail(c, model.ErrInternal("query patient wx_openid failed"))
		return
	}
	ctxLogger(c).Info().
		Str("patient_id", row.PatientID).
		Str("bound_openid", boundOpenID).
		Str("current_openid", currentOpenID).
		Bool("is_idempotent", boundOpenID == currentOpenID).
		Bool("is_already_bound_other", boundOpenID != "" && boundOpenID != currentOpenID).
		Msg("bind-phone: wx_openid check done (T159-dbg)")
	if boundOpenID == currentOpenID {
		// 幂等：已绑定到当前 openid → 直接签发正式 JWT
		ctxLogger(c).Info().Str("patient_id", row.PatientID).Msg("bind-phone: idempotent OK (T159-dbg)")
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
		ctxLogger(c).Info().Err(err).Str("patient_id", row.PatientID).Msg("bind-phone: BindPatientOpenid failed (T159-dbg)")
		fail(c, model.ErrInternal("bind openid failed"))
		return
	}

	// 步骤 6：签发正式 JWT
	ctxLogger(c).Info().Str("patient_id", row.PatientID).Msg("bind-phone: bind OK (T159-dbg)")
	h.respondLoginOK(c, row)
}

// wechatErrText 把微信上游 error 转成可安全落日志的文本。
//
// 不能直接 .Err(err)：Go 的 *url.Error.Error() 会内嵌完整请求 URL，而
// /cgi-bin/token 的 query 带 appid + AppSecret、手机号接口的 query 带 access_token
// （wechat.go 用 query string 传凭据）。原样写日志＝把 AppSecret 落进 staging 日志。
// 这里保留整条 wrap 链的可读文本（"http do token: Get ...: dial tcp ..." 这类前缀正是
// 定位所需），只把出现的 URL 换成去掉 query 的版本。
func wechatErrText(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	var ue *url.Error
	if errors.As(err, &ue) && ue.URL != "" {
		msg = strings.ReplaceAll(msg, ue.URL, strings.SplitN(ue.URL, "?", 2)[0])
	}
	return msg
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
