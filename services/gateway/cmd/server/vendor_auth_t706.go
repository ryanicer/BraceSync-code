// T706 厂商 API key 鉴权（gateway）：独立路由组 + 自己的中间件。
//
// 为什么必须自成一组：JWT 组的 jwtAuth/scopeAuthz/roleAuthz 对厂商 key 没有语义
// （厂商 key 不产生 JWT、没有角色），挂成子组会先吃 401/403。设计稿 §4 末条 P-2
// 把「vendor 组自成一个 r.Group("/api/v1") + vendor-key 中间件」写成可测断言。
//
// 验签口径（设计稿 §2/§3）：
//   - key 形如 bsk_<VND000001>_<64hex>；库内只有 HMAC-SHA256(VENDOR_KEY_PEPPER, 全串)
//   - 每次调用现查 device-service 的候选行（internal 端点），不做进程内缓存 ⇒ 吊销即时生效
//   - 状态分支：revoked/expired/suspended 各回自己的 slug；未知串一律 invalid_key
//     （三者都不带厂商信息，防止从错误报文反推 key 归属）
//
// 越权回报：持有合法 key 却打非 /api/v1/vendor/* 的请求，在 jwtAuth 里先验签再 403
// vendor_scope_denied，并异步把一条 vendor_denied 回报给 device-service（审计 owner 在那边）。
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// 厂商面错误 slug（与 device-service 侧同名常量取值一致）
const (
	vendorSlugScopeDenied     = "vendor_scope_denied"
	vendorSlugInvalidKey      = "invalid_key"
	vendorSlugKeyExpired      = "key_expired"
	vendorSlugKeyRevoked      = "key_revoked"
	vendorSlugKeySuspended    = "key_suspended"
	vendorSlugVendorSuspended = "vendor_suspended"
	vendorSlugPepperMissing   = "vendor_pepper_missing"
	vendorSlugAuthUnavailable = "vendor_auth_unavailable"
)

// vendorKeyPattern 只认一种形状：bsk_ + 6 位厂商号 + 64 位小写 hex。
var vendorKeyPattern = regexp.MustCompile(`^bsk_(VND\d{6})_([0-9a-f]{64})$`)

// vendorIdentity 验签通过后的厂商上下文（注入 X-Vendor-* 三头给 device-service）
type vendorIdentity struct {
	VendorID  string
	KeyID     string
	KeyPrefix string
}

// vendorAuthError 验签失败（status + slug 即对外契约）
type vendorAuthError struct {
	Status int
	Slug   string
	Detail string
}

type vendorKeyCandidate struct {
	KeyID        string `json:"keyId"`
	VendorID     string `json:"vendorId"`
	KeyHash      string `json:"keyHash"`
	Status       string `json:"status"`
	ExpiresAt    string `json:"expiresAt"`
	VendorStatus string `json:"vendorStatus"`
}

// vendorAuth 鉴权依赖：pepper 只从环境变量来；候选行从 device-service internal 端点取。
type vendorAuth struct {
	pepper        string
	deviceBaseURL string
	client        *http.Client
	now           func() time.Time
}

func newVendorAuth() *vendorAuth {
	return &vendorAuth{
		pepper:        os.Getenv("VENDOR_KEY_PEPPER"),
		deviceBaseURL: envOrURL("DEVICE_SERVICE_URL", defaultDeviceServiceURL),
		client:        &http.Client{Timeout: 3 * time.Second},
		now:           time.Now,
	}
}

// bearerVendorKey 取出 bsk_ 开头的 Bearer；不是厂商 key 返回 ""。
func bearerVendorKey(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	token := strings.TrimPrefix(h, prefix)
	if strings.HasPrefix(token, "bsk_") {
		return token
	}
	return ""
}

// verify 比对完整 key 与候选摘要，并判定 key/厂商状态。
func (va *vendorAuth) verify(r *http.Request) (*vendorIdentity, *vendorAuthError) {
	token := bearerVendorKey(r)
	if token == "" {
		return nil, &vendorAuthError{http.StatusUnauthorized, vendorSlugInvalidKey, "missing vendor key"}
	}
	m := vendorKeyPattern.FindStringSubmatch(token)
	if m == nil {
		return nil, &vendorAuthError{http.StatusUnauthorized, vendorSlugInvalidKey, "malformed vendor key"}
	}
	if va.pepper == "" {
		// fail-closed：没有 pepper 就验不了任何 key，绝不降级放行
		return nil, &vendorAuthError{http.StatusInternalServerError, vendorSlugPepperMissing, "VENDOR_KEY_PEPPER not configured"}
	}
	prefix := "bsk_" + m[1] + "_"
	candidates, err := va.candidates(prefix)
	if err != nil {
		return nil, &vendorAuthError{http.StatusInternalServerError, vendorSlugAuthUnavailable, err.Error()}
	}

	mac := hmac.New(sha256.New, []byte(va.pepper))
	mac.Write([]byte(token))
	digest := hex.EncodeToString(mac.Sum(nil))

	for _, cand := range candidates {
		if !hmac.Equal([]byte(digest), []byte(cand.KeyHash)) {
			continue
		}
		now := va.now()
		switch cand.Status {
		case "revoked":
			return nil, &vendorAuthError{http.StatusUnauthorized, vendorSlugKeyRevoked, "key revoked"}
		case "suspended":
			return nil, &vendorAuthError{http.StatusUnauthorized, vendorSlugKeySuspended, "key suspended"}
		case "expired":
			return nil, &vendorAuthError{http.StatusUnauthorized, vendorSlugKeyExpired, "key expired"}
		}
		if exp, parseErr := time.Parse(time.RFC3339, cand.ExpiresAt); parseErr == nil && !now.Before(exp) {
			return nil, &vendorAuthError{http.StatusUnauthorized, vendorSlugKeyExpired, "key expired"}
		}
		if cand.VendorStatus != "active" {
			return nil, &vendorAuthError{http.StatusUnauthorized, vendorSlugVendorSuspended, "vendor " + cand.VendorStatus}
		}
		return &vendorIdentity{VendorID: cand.VendorID, KeyID: cand.KeyID, KeyPrefix: prefix}, nil
	}
	return nil, &vendorAuthError{http.StatusUnauthorized, vendorSlugInvalidKey, "unknown key"}
}

func (va *vendorAuth) candidates(prefix string) ([]vendorKeyCandidate, error) {
	endpoint := strings.TrimRight(va.deviceBaseURL, "/") + "/internal/vendor-keys?prefix=" + url.QueryEscape(prefix)
	resp, err := va.client.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("query vendor key candidates: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vendor key candidates status %d", resp.StatusCode)
	}
	var body struct {
		Keys []vendorKeyCandidate `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode vendor key candidates: %w", err)
	}
	return body.Keys, nil
}

// abortVendor 厂商面错误响应：code + 机器可读 error + 人读 message；技术文本只进日志。
func abortVendor(c *gin.Context, e *vendorAuthError) {
	requestID := requestIDOf(c)
	logTechnical(c, e.Status, e.Status, e.Detail, requestID)
	c.AbortWithStatusJSON(e.Status, gin.H{
		"code":    e.Status,
		"error":   e.Slug,
		"message": e.Slug,
	})
}

// vendorKeyAuth 厂商组中间件：先剥离外部伪造的 X-Vendor-* 三头，再验签注入。
func vendorKeyAuth(va *vendorAuth) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Header.Del("X-Vendor-Id")
		c.Request.Header.Del("X-Vendor-Key-Prefix")
		c.Request.Header.Del("X-Vendor-Key-Id")

		id, authErr := va.verify(c.Request)
		if authErr != nil {
			abortVendor(c, authErr)
			return
		}
		c.Request.Header.Set("X-Vendor-Id", id.VendorID)
		c.Request.Header.Set("X-Vendor-Key-Prefix", id.KeyPrefix)
		c.Request.Header.Set("X-Vendor-Key-Id", id.KeyID)
		c.Next()
	}
}

// reportVendorDenied 异步把越权尝试回报给 device-service（审计 owner 在那边）。
// best-effort：回报失败只落日志，绝不影响已经在写的 403。
func (va *vendorAuth) reportVendorDenied(r *http.Request, id *vendorIdentity, status int) {
	payload, err := json.Marshal(map[string]any{
		"vendorId": id.VendorID,
		"keyId":    id.KeyID,
		"method":   r.Method,
		"path":     r.URL.Path,
		"status":   status,
		"ip":       r.RemoteAddr,
	})
	if err != nil {
		return
	}
	endpoint := strings.TrimRight(va.deviceBaseURL, "/") + "/internal/vendor-denied"
	go func() {
		resp, err := va.client.Post(endpoint, "application/json", bytes.NewReader(payload))
		if err != nil {
			log.Warn().Err(err).Str("vendor_id", id.VendorID).Msg("report vendor denied failed")
			return
		}
		_ = resp.Body.Close()
	}()
}

// denyJWTAccess 对合法厂商 key 的非本域访问回 403 vendor_scope_denied。
// 返回 true 表示已写出响应，调用方应停止后续处理。
func (va *vendorAuth) denyJWTAccess(c *gin.Context) bool {
	id, authErr := va.verify(c.Request)
	if authErr != nil {
		abortVendor(c, authErr)
		return true
	}
	va.reportVendorDenied(c.Request, id, http.StatusForbidden)
	abortVendor(c, &vendorAuthError{
		Status: http.StatusForbidden, Slug: vendorSlugScopeDenied,
		Detail: "vendor key may not access " + c.Request.URL.Path,
	})
	return true
}

// vendorScopeGate 引擎级前置门：厂商 key 只允许打 /api/v1/vendor/*。
//
// 挂全局而不是 jwtAuth 里，是为了同时覆盖三类落点：
//   - 已注册的 JWT 端点（/patients、/teams、/admin/*、/devices/:id/bind…）→ 403；
//   - 完全未注册的路径（/admin/configs 这类）→ NoRoute 也会先走全局中间件，仍是 403
//     而不是裸 404（矩阵 M08 要求全族 403）；
//   - 登录白名单 → 403（白名单只免 JWT，不免厂商域）。
//
// 两个放行口：/api/v1/vendor/* 交给 vendorKeyAuth 自己验；/api/v1/device/* 交给设备验签组，
// 让厂商 key 在那里吃 401（设备族只认 HMAC 四头，矩阵 M10 的判据就是 401）。
func vendorScopeGate(va *vendorAuth) gin.HandlerFunc {
	return func(c *gin.Context) {
		if bearerVendorKey(c.Request) == "" {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/v1/vendor/") || strings.HasPrefix(path, "/api/v1/device/") {
			c.Next()
			return
		}
		va.denyJWTAccess(c)
	}
}
