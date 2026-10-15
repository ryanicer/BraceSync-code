// T690 微信客服进入事件推送（PRD §7A.13 通道一 · V3.48 Boss 终审方案 1 身份透传）。
//
// 机制：患者在统一咨询入口点 <button open-type="contact" session-from="患者ID|…"> 进入
// 微信客服会话时，微信把 user_enter_tempsession 事件（含 SessionFrom 与 FromUserName＝
// 患者 openid）推到本端点；服务端据此在用户进入后 1 分钟内（最多 2 条、无需患者先发消息）
// 经客服消息接口自动下发一条身份消息「患者档案：{姓名}（{患者ID}）· 设备 {设备号}」——
// 客服窗直接对上人，患者本人亦可见（内容为其本人档案信息）。不是修改微信昵称。
//
// 身份解析兜底（PRD 原文）：SessionFrom 异常/缺失时以事件 openid 反查 patients.wx_openid。
//
// 协议边界：
//   - 本端点免 JWT（gateway authWhitelisted 放行 GET/POST），信任锚 = 微信 URL 验签
//     （sha1(sort(token,timestamp,nonce))）；token 未配置时 fail-closed 整端点拒收。
//   - 只支持明文推送体（XML 或 JSON 按首字符自动识别）；安全模式加密体（Encrypt 字段）
//     不解密、不处理（实测接法另笔回写后补）。
//   - 响应按微信明文约定回 "success"；无法解析的事件回 success 防重试风暴，
//     下游发送失败回 500 让微信重试。
//
// 真机实测项（§7A.13 ⚠️）：小程序客服消息与 mpkf.weixin.qq.com 两套体系并存，
// 运营值班通道与消息流向必须真机确认；本实现按小程序客服消息体系（open-type=contact
// 的原生链路）兜底落地，实测结论另笔回写。
package handler

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// kfPushEnvToken 微信消息推送配置的 Token（小程序后台 开发→开发设置→消息推送）。
// 未配置 ⇒ GET 验签无法通过、POST 一律拒收（fail-closed）。
const kfPushEnvToken = "WX_KF_MSG_TOKEN"

// kfPushEvent 微信「用户进入客服会话」事件名
const kfPushEvent = "user_enter_tempsession"

// kfPushBodyLimit 推送体字节上限（事件报文远小于此值，防御性截断）
const kfPushBodyLimit = 8 << 10

// kfSenderI 客服消息下发最小接口（单测注入 fake；生产为 *wechat.Client.SendKfText）
type kfSenderI interface {
	SendKfText(ctx context.Context, toUser, content string) error
}

// wxKfPushXML 微信推送体（明文 XML 形；未出现的字段留空即可）
type wxKfPushXML struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Event        string   `xml:"Event"`
	SessionFrom  string   `xml:"SessionFrom"`
	Encrypt      string   `xml:"Encrypt"`
}

// wxKfPushJSON 微信推送体（明文 JSON 形，数据格式选 JSON 时）
type wxKfPushJSON struct {
	ToUserName   string `json:"ToUserName"`
	FromUserName string `json:"FromUserName"`
	MsgType      string `json:"MsgType"`
	Event        string `json:"Event"`
	SessionFrom  string `json:"SessionFrom"`
	Encrypt      string `json:"Encrypt"`
}

// wxKfPush GET = URL 验签挑战；POST = 事件推送（当前只处理 user_enter_tempsession）
func (h *Handler) wxKfPush(c *gin.Context) {
	token := strings.TrimSpace(os.Getenv(kfPushEnvToken))
	if token == "" {
		log.Error().Str("env", kfPushEnvToken).Msg("wechat kf push rejected: token env not configured (fail-closed)")
		c.String(http.StatusServiceUnavailable, "kf push token not configured")
		return
	}
	if !wxPushSignatureOK(token, c.Query("signature"), c.Query("timestamp"), c.Query("nonce")) {
		log.Warn().Str("ip", c.ClientIP()).Msg("wechat kf push rejected: signature mismatch")
		c.String(http.StatusForbidden, "forbidden")
		return
	}

	if c.Request.Method == http.MethodGet {
		// URL 验签挑战：验签通过原样回 echostr
		c.String(http.StatusOK, c.Query("echostr"))
		return
	}
	h.handleKfEnterEvent(c)
}

// handleKfEnterEvent 解析推送体并下发身份消息
func (h *Handler) handleKfEnterEvent(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, kfPushBodyLimit+1))
	if err != nil {
		log.Warn().Err(err).Msg("wechat kf push: read body failed")
		c.String(http.StatusBadRequest, "bad request")
		return
	}
	if len(raw) > kfPushBodyLimit {
		log.Warn().Msg("wechat kf push: body exceeds limit")
		c.String(http.StatusBadRequest, "bad request")
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		c.String(http.StatusOK, "success")
		return
	}

	msgType, event, sessionFrom, fromUser, ok := parseKfPushBody(raw)
	if !ok {
		// 无法解析/安全模式加密体：回 success 防无限重试（接法实测后另补）
		c.String(http.StatusOK, "success")
		return
	}
	if msgType != "event" || event != kfPushEvent {
		log.Debug().Str("msg_type", msgType).Str("event", event).Msg("wechat kf push: event ignored")
		c.String(http.StatusOK, "success")
		return
	}
	if strings.TrimSpace(fromUser) == "" {
		log.Warn().Msg("wechat kf push: event missing FromUserName")
		c.String(http.StatusOK, "success")
		return
	}

	patient := h.resolveKfPatient(c, sessionFrom, fromUser)
	if patient == nil {
		// 无从对上人：无可下发内容，回 success 不让微信重试
		log.Warn().Str("session_from", truncateKfText(sessionFrom)).Msg("wechat kf push: patient unresolved, identity message skipped")
		c.String(http.StatusOK, "success")
		return
	}
	if h.kfSender == nil {
		log.Error().Msg("wechat kf push: kf sender not configured (WX_APPID/WX_APP_SECRET missing?), identity message dropped")
		c.String(http.StatusOK, "success")
		return
	}

	text := kfIdentityText(patient)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	if err := h.kfSender.SendKfText(ctx, fromUser, text); err != nil {
		// 发送失败 → 500 让微信重试（进入事件在 1 分钟窗口内重试仍有效）
		log.Error().Err(err).Str("patient_id", patient.PatientID).Msg("wechat kf push: send identity message failed")
		c.String(http.StatusInternalServerError, "send failed")
		return
	}
	log.Info().Str("patient_id", patient.PatientID).Msg("wechat kf push: identity message sent")
	c.String(http.StatusOK, "success")
}

// parseKfPushBody 按首字符识别 XML/JSON 明文体；加密体或解析失败返回 ok=false
func parseKfPushBody(raw []byte) (msgType, event, sessionFrom, fromUser string, ok bool) {
	trimmed := strings.TrimLeft(string(raw), " \t\r\n")
	if strings.HasPrefix(trimmed, "{") {
		var j wxKfPushJSON
		if err := json.Unmarshal([]byte(trimmed), &j); err != nil {
			log.Warn().Err(err).Msg("wechat kf push: parse json body failed")
			return "", "", "", "", false
		}
		if strings.TrimSpace(j.Encrypt) != "" {
			log.Warn().Msg("wechat kf push: encrypted (安全模式) body unsupported, skipped")
			return "", "", "", "", false
		}
		return j.MsgType, j.Event, j.SessionFrom, j.FromUserName, true
	}
	var x wxKfPushXML
	if err := xml.Unmarshal([]byte(trimmed), &x); err != nil {
		log.Warn().Err(err).Msg("wechat kf push: parse xml body failed")
		return "", "", "", "", false
	}
	if strings.TrimSpace(x.Encrypt) != "" {
		log.Warn().Msg("wechat kf push: encrypted (安全模式) body unsupported, skipped")
		return "", "", "", "", false
	}
	return x.MsgType, x.Event, x.SessionFrom, x.FromUserName, true
}

// resolveKfPatient SessionFrom 首段（患者ID）→ 档案；异常时以 openid 反查 wx_openid 兜底（PRD 原文）。
func (h *Handler) resolveKfPatient(c *gin.Context, sessionFrom, openid string) *repo.PatientRow {
	ctx := c.Request.Context()
	if pid := firstSessionFromSegment(sessionFrom); pid != "" {
		p, err := h.store.GetPatient(ctx, pid)
		if err == nil && p != nil {
			return p
		}
		if err != nil && !errors.Is(err, repo.ErrPatientNotFound) {
			log.Warn().Err(err).Str("patient_id", pid).Msg("wechat kf push: GetPatient by sessionFrom failed, fallback to openid")
		}
	}
	login, err := h.store.GetPatientByWXOpenID(ctx, openid)
	if err != nil {
		log.Warn().Err(err).Msg("wechat kf push: GetPatientByWXOpenID failed")
		return nil
	}
	if login == nil {
		return nil
	}
	p, err := h.store.GetPatient(ctx, login.PatientID)
	if err != nil {
		log.Warn().Err(err).Str("patient_id", login.PatientID).Msg("wechat kf push: GetPatient by openid failed")
		return nil
	}
	return p
}

// firstSessionFromSegment session-from = 患者ID|姓名|设备号（前端拼装），首段即患者ID
func firstSessionFromSegment(sessionFrom string) string {
	s := strings.TrimSpace(sessionFrom)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "|｜"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// kfIdentityText 身份消息定稿文案（PRD §7A.13 示例：「患者档案：林小雨（P20260001）· 设备 BSYNC-701001」）
func kfIdentityText(p *repo.PatientRow) string {
	var b strings.Builder
	b.WriteString("患者档案：")
	b.WriteString(p.Name)
	b.WriteString("（")
	b.WriteString(p.PatientID)
	b.WriteString("）")
	if p.DeviceID != nil && strings.TrimSpace(*p.DeviceID) != "" {
		b.WriteString("· 设备 ")
		b.WriteString(strings.TrimSpace(*p.DeviceID))
	}
	return b.String()
}

// wxPushSignatureOK sha1(sort(token, timestamp, nonce)) == signature（微信 URL 验签）
func wxPushSignatureOK(token, signature, timestamp, nonce string) bool {
	if strings.TrimSpace(signature) == "" {
		return false
	}
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return strings.EqualFold(hex.EncodeToString(sum[:]), signature)
}

// truncateKfText 日志用限长（SessionFrom 可能被伪造注入超长串）
func truncateKfText(s string) string {
	const n = 64
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return string(s[:n]) + "..."
}
