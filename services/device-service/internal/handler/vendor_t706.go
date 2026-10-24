// T706 厂商凭据面的 HTTP 接入层。
//
// 分三块：
//   - /api/v1/vendor/*   厂商 key 面（网关验签后注入 X-Vendor-* 三头；本层只做业务与限流）
//   - /api/v1/admin/vendors*  运营管理员登记厂商 / 签发 / 吊销 key（assertAdminRole）
//   - /internal/vendor-* 网关内部用（候选 key 行、越权回报；nginx 不放 /internal/*）
//
// 响应形状：成功沿用统一信封 {code:0,...}; 失败为 {code:<http>,error:<slug>,message}。
// error 是机器可读判据（矩阵按 slug 断言），message 只作人读补充。
//
// 零明文：本文件的任何响应体/日志都不写 secret 或完整 key；厂商设备的 view 不带患者标识。
package handler

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/device-service/internal/repo"
	"github.com/bracesync/bracesync/services/device-service/internal/service"
)

// 网关注入的厂商身份头（jwtAuth 同族：外部伪造值必须先被网关剥离）
const (
	headerVendorID        = "X-Vendor-Id"
	headerVendorKeyPrefix = "X-Vendor-Key-Prefix"
	headerVendorKeyID     = "X-Vendor-Key-Id"
)

// 厂商面错误 slug（与网关侧同名常量取值一致；跨模块各持字面量，不互相 import）
const (
	vendorSlugScopeDenied   = "vendor_scope_denied"
	vendorSlugRateLimited   = "vendor_rate_limited"
	vendorSlugInvalidKey    = "invalid_key"
	vendorSlugKeyExpired    = "key_expired"
	vendorSlugKeyRevoked    = "key_revoked"
	vendorSlugKeySuspended  = "key_suspended"
	vendorSlugVendorStopped = "vendor_suspended"
)

func vendorOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": data})
}

func vendorFail(c *gin.Context, e *service.VendorError) {
	c.JSON(e.Status, gin.H{"code": e.Status, "error": e.Slug, "message": e.Message})
}

// vendorIdentity 取网关注入的厂商上下文（缺失=直连绕网关，一律 401）；
// 厂商状态每次现读（§3：不加缓存，停用下一次调用即失效）。
func (h *Handler) vendorIdentity(c *gin.Context) (*repo.Vendor, string, *service.VendorError) {
	vendorID := c.GetHeader(headerVendorID)
	keyPrefix := c.GetHeader(headerVendorKeyPrefix)
	if vendorID == "" || keyPrefix == "" {
		return nil, "", &service.VendorError{Status: http.StatusUnauthorized, Slug: vendorSlugInvalidKey,
			Message: "missing gateway vendor identity"}
	}
	vendor, appErr := h.svc.VendorByID(c.Request.Context(), vendorID)
	if appErr != nil {
		return nil, "", appErr
	}
	return vendor, keyPrefix, nil
}

// ─────────────────────────────────────────────────────────────
// 厂商面四条
// ─────────────────────────────────────────────────────────────

type vendorBatchRequest struct {
	Model   string                       `json:"model"`
	Devices []service.VendorRegisterItem `json:"devices"`
}

// vendorBatchRegister POST /api/v1/vendor/devices
func (h *Handler) vendorBatchRegister(c *gin.Context) {
	vendor, keyPrefix, appErr := h.vendorIdentity(c)
	if appErr != nil {
		vendorFail(c, appErr)
		return
	}
	var req vendorBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		vendorFail(c, &service.VendorError{Status: http.StatusBadRequest,
			Slug: service.VendorSlugInvalidParam, Message: "invalid request body"})
		return
	}
	// 请求体里的 vendor_id（若有）一律忽略：归属由 key 决定。
	result, vErr := h.svc.VendorBatchRegister(c.Request.Context(), vendor, keyPrefix, c.ClientIP(),
		req.Model, req.Devices)
	if vErr != nil {
		vendorFail(c, vErr)
		return
	}
	h.svc.TouchVendorKey(c.Request.Context(), c.GetHeader(headerVendorKeyID))
	vendorOK(c, result)
}

// vendorListDevices GET /api/v1/vendor/devices?status=&page=&pageSize=
func (h *Handler) vendorListDevices(c *gin.Context) {
	vendor, _, appErr := h.vendorIdentity(c)
	if appErr != nil {
		vendorFail(c, appErr)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	views, total, vErr := h.svc.VendorListDevices(c.Request.Context(), vendor,
		c.Query("status"), page, pageSize)
	if vErr != nil {
		vendorFail(c, vErr)
		return
	}
	vendorOK(c, gin.H{
		"list":     views,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// vendorDeviceMeta GET /api/v1/vendor/devices/:deviceId
func (h *Handler) vendorDeviceMeta(c *gin.Context) {
	vendor, _, appErr := h.vendorIdentity(c)
	if appErr != nil {
		vendorFail(c, appErr)
		return
	}
	view, vErr := h.svc.VendorDeviceMeta(c.Request.Context(), vendor, c.Param("deviceId"))
	if vErr != nil {
		vendorFail(c, vErr)
		return
	}
	vendorOK(c, view)
}

// vendorFetchSecret GET /api/v1/vendor/devices/:deviceId/secret?reason=
func (h *Handler) vendorFetchSecret(c *gin.Context) {
	vendor, keyPrefix, appErr := h.vendorIdentity(c)
	if appErr != nil {
		vendorFail(c, appErr)
		return
	}
	secret, vErr := h.svc.VendorFetchSecret(c.Request.Context(), vendor, keyPrefix,
		c.Param("deviceId"), c.Query("reason"), c.ClientIP())
	if vErr != nil {
		vendorFail(c, vErr)
		return
	}
	h.svc.TouchVendorKey(c.Request.Context(), c.GetHeader(headerVendorKeyID))
	vendorOK(c, gin.H{"deviceId": c.Param("deviceId"), "secret": secret})
}

// ─────────────────────────────────────────────────────────────
// 限流（QPS + 自然日；单实例内存态，键=key_id）
// ─────────────────────────────────────────────────────────────

type vendorRateWindow struct {
	second int64
	qps    int
	day    string
	calls  int
}

// vendorRateLimiter 进程内计数：QPS 按秒窗、日限按北京时间自然日。
// device-service 单实例部署（与 T091 配送窗口防护同口径）；重启清零只放松不收紧。
type vendorRateLimiter struct {
	mu sync.Mutex
	m  map[string]*vendorRateWindow
}

func newVendorRateLimiter() *vendorRateLimiter {
	return &vendorRateLimiter{m: make(map[string]*vendorRateWindow)}
}

func (l *vendorRateLimiter) allow(keyID string, now time.Time, qpsLimit, dailyLimit int) (ok bool, which string, current, threshold int) {
	cst := time.FixedZone("CST", 8*3600)
	t := now.In(cst)
	sec := now.Unix()
	day := t.Format("2006-01-02")

	l.mu.Lock()
	defer l.mu.Unlock()
	w, exists := l.m[keyID]
	if !exists {
		w = &vendorRateWindow{}
		l.m[keyID] = w
	}
	if w.day != day {
		w.day, w.calls = day, 0
	}
	if w.second != sec {
		w.second, w.qps = sec, 0
	}
	if w.qps >= qpsLimit {
		return false, "vendor_key_qps_limit", w.qps, qpsLimit
	}
	if w.calls >= dailyLimit {
		return false, "vendor_key_daily_calls", w.calls, dailyLimit
	}
	w.qps++
	w.calls++
	return true, "", 0, 0
}

// vendorRateLimit 厂商组前置中间件：超限 429 + Retry-After + 一条 vendor_rate_hit 审计。
func (h *Handler) vendorRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		keyID := c.GetHeader(headerVendorKeyID)
		if keyID == "" {
			c.Next() // 身份缺失交给各 handler 出 401
			return
		}
		qpsLimit, dailyLimit := h.svc.VendorRateLimits(c.Request.Context())
		ok, which, current, threshold := h.limiter.allow(keyID, time.Now(), qpsLimit, dailyLimit)
		if ok {
			c.Next()
			return
		}
		_ = h.svc.RecordVendorRateHit(c.Request.Context(), c.GetHeader(headerVendorID), keyID, which, current, threshold)
		c.Header("Retry-After", "1")
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"code": http.StatusTooManyRequests, "error": vendorSlugRateLimited,
			"message": "rate limit exceeded: " + which,
		})
	}
}

// ─────────────────────────────────────────────────────────────
// 管理面：厂商登记 / key 签发 / 吊销 / 列表
// ─────────────────────────────────────────────────────────────

type createVendorRequest struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
	Note   string   `json:"note"`
}

// createVendor POST /api/v1/admin/vendors
func (h *Handler) createVendor(c *gin.Context) {
	if !h.assertAdminRole(c, "register a vendor") {
		return
	}
	var req createVendorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		vendorFail(c, &service.VendorError{Status: http.StatusBadRequest,
			Slug: service.VendorSlugInvalidParam, Message: "invalid request body"})
		return
	}
	v, vErr := h.svc.CreateVendor(c.Request.Context(), operatorID(c), req.Name, req.Models, req.Note)
	if vErr != nil {
		vendorFail(c, vErr)
		return
	}
	vendorOK(c, gin.H{"vendorId": v.VendorID, "name": v.Name, "models": v.Models, "status": v.Status})
}

// listVendors GET /api/v1/admin/vendors
func (h *Handler) listVendors(c *gin.Context) {
	if !h.assertAdminRole(c, "list vendors") {
		return
	}
	vendors, vErr := h.svc.ListVendors(c.Request.Context())
	if vErr != nil {
		vendorFail(c, vErr)
		return
	}
	out := make([]gin.H, 0, len(vendors))
	for _, v := range vendors {
		out = append(out, gin.H{
			"vendorId": v.VendorID, "name": v.Name, "models": v.Models,
			"status": v.Status, "createdAt": v.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	vendorOK(c, gin.H{"list": out, "total": len(out)})
}

// issueVendorKey POST /api/v1/admin/vendors/:vendorId/keys
func (h *Handler) issueVendorKey(c *gin.Context) {
	if !h.assertAdminRole(c, "issue a vendor key") {
		return
	}
	issued, vErr := h.svc.IssueVendorKey(c.Request.Context(), operatorID(c), c.Param("vendorId"))
	if vErr != nil {
		vendorFail(c, vErr)
		return
	}
	vendorOK(c, issued)
}

// listVendorKeys GET /api/v1/admin/vendors/:vendorId/keys
func (h *Handler) listVendorKeys(c *gin.Context) {
	if !h.assertAdminRole(c, "list vendor keys") {
		return
	}
	keys, vErr := h.svc.ListVendorKeys(c.Request.Context(), c.Param("vendorId"))
	if vErr != nil {
		vendorFail(c, vErr)
		return
	}
	vendorOK(c, gin.H{"list": keys, "total": len(keys)})
}

type revokeVendorKeyRequest struct {
	Reason string `json:"reason"`
}

// revokeVendorKey POST /api/v1/admin/vendor-keys/:keyId/revoke
func (h *Handler) revokeVendorKey(c *gin.Context) {
	if !h.assertAdminRole(c, "revoke a vendor key") {
		return
	}
	var req revokeVendorKeyRequest
	_ = c.ShouldBindJSON(&req) // 空体允许（reason 非必填）
	vendorID, vErr := h.svc.RevokeVendorKey(c.Request.Context(), operatorID(c), c.Param("keyId"), req.Reason)
	if vErr != nil {
		vendorFail(c, vErr)
		return
	}
	vendorOK(c, gin.H{"keyId": c.Param("keyId"), "vendorId": vendorID, "status": "revoked"})
}

// ─────────────────────────────────────────────────────────────
// 内部面（网关专用；nginx 不放 /internal/*）
// ─────────────────────────────────────────────────────────────

// vendorKeyCandidates GET /internal/vendor-keys?prefix=bsk_VND000001_
func (h *Handler) vendorKeyCandidates(c *gin.Context) {
	prefix := c.Query("prefix")
	if prefix == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": service.VendorSlugInvalidParam})
		return
	}
	list, vErr := h.svc.VendorKeyCandidatesForPrefix(c.Request.Context(), prefix)
	if vErr != nil {
		c.JSON(vErr.Status, gin.H{"error": vErr.Slug})
		return
	}
	candidates := make([]gin.H, 0, len(list))
	for _, k := range list {
		candidates = append(candidates, gin.H{
			"keyId":        k.KeyID,
			"vendorId":     k.VendorID,
			"keyHash":      k.KeyHash,
			"status":       k.Status,
			"expiresAt":    k.ExpiresAt.UTC().Format(time.RFC3339),
			"vendorStatus": k.VendorStatus,
		})
	}
	c.JSON(http.StatusOK, gin.H{"keys": candidates})
}

type vendorDeniedRequest struct {
	VendorID string `json:"vendorId"`
	KeyID    string `json:"keyId"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Status   int    `json:"status"`
	IP       string `json:"ip"`
}

// vendorDenied POST /internal/vendor-denied —— 网关挡下越权后异步回报，落 vendor_denied。
func (h *Handler) vendorDenied(c *gin.Context) {
	var req vendorDeniedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": service.VendorSlugInvalidParam})
		return
	}
	if err := h.svc.RecordVendorDenied(c.Request.Context(), service.VendorDeniedInput{
		VendorID: req.VendorID,
		KeyID:    req.KeyID,
		Method:   req.Method,
		Path:     req.Path,
		Status:   req.Status,
		IP:       req.IP,
	}); err != nil {
		log.Warn().Err(err).Str("vendor_id", req.VendorID).Msg("record vendor denied audit")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "audit_write_failed"})
		return
	}
	c.Status(http.StatusNoContent)
}
