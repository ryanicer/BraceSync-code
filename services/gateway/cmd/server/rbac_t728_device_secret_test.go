// Package main — T728 设备验签密钥三条端点的网关侧验证。
//
// 为什么必须有本文件（口径承自 T407/T508，本卡尤其致命）：device-service 的新路由没有任何
// CI 门禁会发现「服务已实现、网关漏转发」或「网关已转发、RBAC 漏登记」。前者的症状是
// 管理员点下载永远 404；后者的症状更阴——T260 起未登记路径默认 403，于是四条角色全被挡，
// 而 device-service 的单测仍然全绿。本卡交出去的是能伪造设备上报的明文，最不能出现的
// 就是「网关没挡、后端挡了，但没人知道」。
//
// 五层判据：
//
//	① 登记面：三条都在 adminOnlyPatterns，且不得漂进 public/staffOnly/techAdmin（那都是放宽）；
//	② 现网形状：只有 admin 真打到 device-service，转发路径逐字原样（deviceId 段被吞=能取别的设备）；
//	   医护/客服/患者/技师/未知/无身份全部在网关 403 且零触达后端；
//	③ 反证可见：同前缀但未登记的兄弟路径是 404，不是 403（否则②的 0 条无法归因）；
//	④ 钩子形状：只有重生成那一条路由的 2xx 会作废缓存，别的 200 与所有失败都不许动；
//	⑤ 端到端：真 setupRouter 下，旧 secret 在重生成前验得过、重生成后立刻验不过、新值立刻可用
//	   ——卡面红线 6「旧值即时失效」的一半责任在网关这份缓存里，SQL 覆盖不到它。
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/gateway/internal/auth"
)

const (
	t728Device         = "DEV-GW-T728"
	t728DownloadPath   = "/api/v1/admin/devices/" + t728Device + "/secret-file"
	t728RegeneratePath = "/api/v1/admin/devices/" + t728Device + "/secret-regenerate"
	t728ExportPath     = "/api/v1/admin/device-secrets/export"
	t728SiblingPath    = "/api/v1/admin/devices/" + t728Device + "/secret-file-preview"
)

var t728Routes = []struct{ method, path string }{
	{http.MethodGet, t728DownloadPath},
	{http.MethodPost, t728ExportPath},
	{http.MethodPost, t728RegeneratePath},
}

// ── ① 登记面 ────────────────────────────────────────────────────────

func TestRBAC_T728_SecretRoutesAreAdminOnly(t *testing.T) {
	for _, rt := range t728Routes {
		assert.True(t, matchRBACPattern(rt.method, rt.path),
			"%s %s 必须登记进 adminOnlyPatterns；未登记即 T260 默认 403（四角色全配不了密钥）", rt.method, rt.path)
		assert.False(t, matchPublicPattern(rt.method, rt.path),
			"%s %s 不得对患者/无身份开放", rt.method, rt.path)
	}
	// 下载那条是 GET，最容易被顺手归进「后台读端点」那一族（staffOnly）。
	// 那一族的放行面含 doctor/cs/tech —— 对他们而言密钥明文是越权可得物，必须留在 admin-only。
	assert.False(t, matchStaffOnlyPattern(http.MethodGet, t728DownloadPath),
		"GET %s 不得落 staffOnly：那是把验签密钥开放给医护/客服/技师", t728DownloadPath)
	assert.False(t, matchTechAdminPattern(http.MethodPost, t728RegeneratePath),
		"重生成不得落 techAdmin：技师在本卡红线里被点名拒绝")
	assert.False(t, matchDoctorAdminPattern(http.MethodPost, t728ExportPath),
		"批量导出不得落 doctorAdmin：医生不需要给设备烧录")
}

// ── ② 现网形状：只放 admin，且路径原样转发 ──────────────────────────

func TestRBAC_T728_OnlyAdminReachesDeviceService(t *testing.T) {
	device, gotDevice := captureBackend(t)
	other, gotOther := captureBackend(t)
	gw := startFullGateway(t, other.URL, device.URL, other.URL, other.URL, other.URL, testJWTSecretMain)

	for _, rt := range t728Routes {
		code, body := httpDoFull(t, rt.method, gw.URL+rt.path, t728Body(rt.method), rbacToken(t, roleAdmin))
		require.Equal(t, http.StatusOK, code, "admin 应放行到后端：%s %s body=%s", rt.method, rt.path, body)
	}
	require.Len(t, *gotDevice, 3, "三条端点均须打到 device-service（代理未注册则为 0）")
	// 逐字对照：后端实收的 URI 必须与前端发出的一模一样（:deviceId 段被吞即变成「取别的设备」）。
	gotPaths := make([]string, 0, len(*gotDevice))
	for _, got := range *gotDevice {
		gotPaths = append(gotPaths, gotMethodPath(t, got).path)
	}
	for _, rt := range t728Routes {
		assert.Contains(t, gotPaths, rt.path, "后端实收路径须逐字含 %s，实收=%v", rt.path, gotPaths)
	}
	assert.Empty(t, *gotOther, "密钥端点属设备域，不得落到其它服务")

	// 医护/客服/患者/技师 + 未知/无身份：网关这一层就拒，一个字节都不往后端送。
	// 技师这一格是本卡与 T387 那一族唯一的分叉点（那一族放 admin+tech，这里只放 admin），
	// 所以必须在这一层显式钉住，而不是等后端判。
	for _, role := range []string{roleDoctor, roleCS, rolePatient, roleTech, "ROLE_GHOST", ""} {
		before := len(*gotDevice)
		for _, rt := range t728Routes {
			code, body := httpDoFull(t, rt.method, gw.URL+rt.path, t728Body(rt.method), rbacToken(t, role))
			assert.Equal(t, http.StatusForbidden, code,
				"role=%q %s %s 应 403，body=%s", role, rt.method, rt.path, body)
		}
		assert.Len(t, *gotDevice, before, "role=%q 的越权请求不得触达后端", role)
	}
}

func t728Body(method string) string {
	if method == http.MethodPost {
		return `{"deviceIds":["` + t728Device + `"]}`
	}
	return ""
}

type gotRequest struct {
	method, path string
}

// gotMethodPath 拆 captureBackend 记录的前两段（"METHOD /uri?query 其余"）
func gotMethodPath(t *testing.T, got string) gotRequest {
	t.Helper()
	parts := strings.Fields(got)
	require.GreaterOrEqual(t, len(parts), 2, "反代记录应为「METHOD /path ...」，实收 %q", got)
	return gotRequest{method: parts[0], path: parts[1]}
}

// ── ③ 反证：未注册的兄弟路径是 404，不是 403 ─────────────────────────

func TestRBAC_T728_UnregisteredSiblingRouteIs404Not403(t *testing.T) {
	assert.False(t, matchRBACPattern(http.MethodGet, t728SiblingPath))

	device, gotDevice := captureBackend(t)
	other, _ := captureBackend(t)
	gw := startFullGateway(t, other.URL, device.URL, other.URL, other.URL, other.URL, testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodGet, gw.URL+t728SiblingPath, "", rbacToken(t, roleAdmin))
	assert.Equal(t, http.StatusNotFound, code, "未注册路由应 404，body=%s", body)
	assert.Empty(t, *gotDevice, "404 不得穿透到后端")
}

// ── ④ 缓存作废钩子的形状 ────────────────────────────────────────────

// countingSecrets 既计数回源、又实现可选作废能力的桩（真实现同形）。
type countingSecrets struct {
	secret string
	hits   int32
	busts  int32
}

func (s *countingSecrets) GetDeviceSecret(context.Context, string) (string, error) {
	atomic.AddInt32(&s.hits, 1)
	return s.secret, nil
}

func (s *countingSecrets) InvalidateDeviceSecret(string) { atomic.AddInt32(&s.busts, 1) }

var (
	_ SecretProvider         = (*countingSecrets)(nil)
	_ secretCacheInvalidator = (*countingSecrets)(nil)
	_ secretCacheInvalidator = (*deviceServiceSecretProvider)(nil) // 漂了会让上一条断言的静默失败显形
)

// t728HookRouter 只挂钩子与三条同形路由（代理本体不参与：这里判的是钩子认不认得这条路由）。
func t728HookRouter(status int, agt *gatewayAuth) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(bustSecretCacheOnRegenerate(agt))
	api.POST("/admin/devices/:deviceId/secret-regenerate", func(c *gin.Context) {
		c.String(status, "upstream")
	})
	api.POST("/admin/device-secrets/export", func(c *gin.Context) { c.String(http.StatusOK, "zip") })
	api.GET("/admin/devices/:deviceId/secret-file", func(c *gin.Context) { c.String(http.StatusOK, "env") })
	api.POST("/other/things", func(c *gin.Context) { c.String(http.StatusOK, "other") })
	return r
}

func TestT728_HookBustsCacheOnlyOnSuccessfulRegenerate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		upstream  int
		wantBusts int32
	}{
		{"200 后旧明文必须作废", http.StatusOK, 1},
		{"403 不动缓存（库里那版没变）", http.StatusForbidden, 0},
		{"500 不动缓存", http.StatusInternalServerError, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secrets := &countingSecrets{secret: "old-plain"}
			r := t728HookRouter(tc.upstream, newGatewayAuth("s", secrets))

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, t728RegeneratePath, nil))

			require.Equal(t, tc.upstream, w.Code)
			assert.Equal(t, tc.wantBusts, atomic.LoadInt32(&secrets.busts),
				"作废次数应为 %d：多清=白打 device-service，漏清=旧值在 TTL 内还能签成功", tc.wantBusts)
		})
	}
}

// TestT728_HookIgnoresOtherRoutes 钩子按 gin 的 FullPath 认路由：批量导出与下载同样是
// /admin/ 前缀、同样可能 200，但它们没换掉任何密钥 ⇒ 不许动缓存（动了就是每次下载白丢一次命中）。
func TestT728_HookIgnoresOtherRoutes(t *testing.T) {
	secrets := &countingSecrets{secret: "still-valid"}
	r := t728HookRouter(http.StatusOK, newGatewayAuth("s", secrets))

	for _, rt := range []struct{ method, path string }{
		{http.MethodPost, t728ExportPath},
		{http.MethodGet, t728DownloadPath},
		{http.MethodPost, "/api/v1/other/things"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(rt.method, rt.path, nil))
		require.Equal(t, http.StatusOK, w.Code, rt.path)
	}
	assert.Zero(t, atomic.LoadInt32(&secrets.busts), "非重生成的 200 不得作废缓存")
}

// TestT728_ProviderInvalidateForcesRefetch 真实现那一格：作废后下一次取密钥必须回源，
// 且拿到后端此刻的值——锁住「清了确实会重查」，而不是「清了但压根没这个条目」。
func TestT728_ProviderInvalidateForcesRefetch(t *testing.T) {
	var mu sync.Mutex
	current := "v1"
	var hits int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		mu.Lock()
		secret := current
		mu.Unlock()
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"secret":"` + secret + `"}}`))
	}))
	t.Cleanup(backend.Close)

	p := newDeviceServiceSecretProvider(backend.URL)
	ctx := context.Background()

	got, err := p.GetDeviceSecret(ctx, t728Device)
	require.NoError(t, err)
	assert.Equal(t, "v1", got)

	got, err = p.GetDeviceSecret(ctx, t728Device)
	require.NoError(t, err)
	assert.Equal(t, "v1", got)
	require.EqualValues(t, 1, atomic.LoadInt32(&hits), "第二次必须走缓存")

	mu.Lock()
	current = "v2"
	mu.Unlock()
	got, err = p.GetDeviceSecret(ctx, t728Device)
	require.NoError(t, err)
	assert.Equal(t, "v1", got, "TTL 内不作废就是旧值续命（红线 6 的反面）")

	p.InvalidateDeviceSecret(t728Device)
	got, err = p.GetDeviceSecret(ctx, t728Device)
	require.NoError(t, err)
	assert.Equal(t, "v2", got, "作废后的下一次必须拿到新值")
	assert.EqualValues(t, 2, atomic.LoadInt32(&hits), "作废前后各回源一次，中间三次调用全走缓存")
}

// ── ⑤ 端到端：旧值即时失效 ──────────────────────────────────────────

// t728StubDeviceService 一台桩同时扮演 device-service 的两个面：
// internal 密钥查询（验签回源用）与 admin 重生成（把现值换掉）。
func t728StubDeviceService(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var mu sync.Mutex
	current := t728OldPlain
	var regenHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.HasPrefix(r.URL.Path, "/internal/devices/"):
			mu.Lock()
			secret := current
			mu.Unlock()
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"secret":"` + secret + `"}}`))
		case strings.HasSuffix(r.URL.Path, "/secret-regenerate"):
			atomic.AddInt32(&regenHits, 1)
			mu.Lock()
			current = t728NewPlain
			mu.Unlock()
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"deviceId":"` + t728Device +
				`","secret":"` + t728NewPlain + `","secretVersion":2}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":null}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &regenHits
}

const (
	t728OldPlain = "old-plain-secret"
	t728NewPlain = "new-plain-secret"
)

func t728ReportHeaders(secret, nonce, body string) map[string]string {
	const reportPath = "/api/v1/device/records"
	ts := time.Now()
	return map[string]string{
		"X-Device-Id": t728Device,
		"X-Timestamp": strconv.FormatInt(ts.Unix(), 10),
		"X-Nonce":     nonce,
		"X-Signature": auth.HMACSHA256(secret, auth.BuildSignString(
			http.MethodPost, reportPath, t728Device, nonce, body, ts)),
	}
}

func TestT728_RealGatewayRefusesOldSecretAfterRegenerate(t *testing.T) {
	device, regenHits := t728StubDeviceService(t)
	data, gotData := captureBackend(t)
	other, _ := captureBackend(t)
	gw := startFullGateway(t, other.URL, device.URL, data.URL, other.URL, other.URL, testJWTSecretMain)

	const body = `{"pressures":[10,20]}`

	// ① 旧值验得过，同时把旧明文灌进网关那份缓存（这一枪就是「没作废就一直续命」的那一颗种子）
	code, msg := httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body,
		t728ReportHeaders(t728OldPlain, t728Nonce(1), body))
	require.Equal(t, http.StatusOK, code, "重生成前旧 secret 应验得过，body=%s", msg)
	require.Len(t, *gotData, 1)

	// ② 管理员重生成（走真 RBAC + 真代理 + 钩子）
	code, msg = httpDoFull(t, http.MethodPost, gw.URL+t728RegeneratePath, "", rbacToken(t, roleAdmin))
	require.Equal(t, http.StatusOK, code, "admin 重生成应放行，body=%s", msg)
	require.EqualValues(t, 1, atomic.LoadInt32(regenHits), "重生成必须打到 device-service")

	// ③ 旧值立刻签不过：缓存没作废的话，这一枪会在 TTL 内继续放行 ⇒ 红线 6 破防
	code, msg = httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body,
		t728ReportHeaders(t728OldPlain, t728Nonce(2), body))
	assert.Equal(t, http.StatusUnauthorized, code,
		"重生成后旧 secret 必须立刻失效（网关缓存未作废则 5 分钟内仍验得过），body=%s", msg)
	assert.Len(t, *gotData, 1, "验签不过的上报不得转发到 data-service")

	// ④ 新值立刻可用（防「清成永久拒绝」这种反向错法）
	code, msg = httpDoFull(t, http.MethodPost, gw.URL+"/api/v1/device/records", body,
		t728ReportHeaders(t728NewPlain, t728Nonce(3), body))
	assert.Equal(t, http.StatusOK, code, "重生成后新 secret 应立刻可用，body=%s", msg)
	assert.Len(t, *gotData, 2, "新值那一枪应转发到 data-service")
}

// t728Nonce 每次上报换一枚 32 位 hex（硬件清单 §2.2 的 nonce 形状；T606 去重键含 nonce，
// 复用同一枚会被重放防护干扰 ⇒ 三枪必须各用一枚）
func t728Nonce(i int) string {
	return fmt.Sprintf("%032x", i)
}
