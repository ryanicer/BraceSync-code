// Package handler 登录并发性能基准与存量哈希兼容回归（T040）
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
	"github.com/bracesync/bracesync/services/user-service/internal/token"
)

func init() { gin.SetMode(gin.TestMode) }

const testJWTSecret = "T040-test-secret-for-keygen-only-do-not-use-in-prod"

const itAdminPassword = "admin123"
const itAdminHashCost10 = "$2a$10$HpFYM9TY7cv8ABe.UZDx/OWi/HpdFcPSBf4rbvyJtlgLBmSo2/Snm"

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	signer, err := token.NewSigner(testJWTSecret, time.Hour)
	require.NoError(t, err)
	store := &fakeStore{}
	return &testEnv{t: t, store: store, signer: signer, h: New(store, signer, nil)}
}

// TestLoginBenchSingleCost10 bcrypt cost10 校验耗时基准（T040）
// 采用同进程内相对判据（cost10 中位耗时 vs cost9），与机器绝对性能 / 共享 CI runner 的
// CPU 争用时基解耦，避免 T141 记录的 49ms < 50ms 这类 flaky；同时保留安全语义：
// bcrypt cost 每 +1 迭代量约 ×2，cost10 应显著慢于 cost9（理论比值 ≈2.0）。
// 若硬编码常量被降级为 cost8/cost9 哈希，其与 cost9 的比值会坍缩到 ≤1.0，被判失败。
func TestLoginBenchSingleCost10(t *testing.T) {
	t.Parallel()

	pwd := []byte(itAdminPassword)

	// 动态生成 cost8/cost9 哈希作为机器无关的参照标尺
	hash8, err := bcrypt.GenerateFromPassword(pwd, 8)
	require.NoError(t, err)
	hash9, err := bcrypt.GenerateFromPassword(pwd, 9)
	require.NoError(t, err)

	const samples = 15
	median := func(hash []byte) time.Duration {
		durs := make([]time.Duration, 0, samples)
		for i := 0; i < samples; i++ {
			start := time.Now()
			require.NoError(t, bcrypt.CompareHashAndPassword(hash, pwd))
			durs = append(durs, time.Since(start))
		}
		sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
		return durs[len(durs)/2]
	}

	med8 := median(hash8)
	med9 := median(hash9)
	med10 := median([]byte(itAdminHashCost10))

	ratio := float64(med10) / float64(med9)
	t.Logf("cost8=%dms cost9=%dms cost10=%dms ratio=%0.2fx (samples=%d)",
		med8.Milliseconds(), med9.Milliseconds(), med10.Milliseconds(), ratio, samples)

	if ratio < 1.5 {
		t.Errorf("cost10 中位耗时仅 %.2fx cost9（应≥1.5x），疑似哈希成本被降级", ratio)
	}
}

// TestLoginCompatCost10Hash 存量 $2a$10$ 哈希兼容回归
func TestLoginCompatCost10Hash(t *testing.T) {
	t.Parallel()

	e := newTestEnv(t)
	e.store.admin = &repo.AdminRow{
		AdminID: "A0001", Username: "ops_admin", Name: "运营小张",
		PasswordHash: itAdminHashCost10, RoleID: "ROLE_ADMIN", Status: "enabled",
	}
	e.store.scope = "all"

	w, resp := e.do("POST", "/api/v1/auth/login", map[string]string{
		"username": "ops_admin", "password": itAdminPassword,
	}, nil)

	assert.Equal(t, 200, w.Code, "状态码应为 200")
	assert.Equal(t, 0, resp.Code, "业务码应为 0")

	var dto model.LoginResultDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "A0001", dto.AdminID)
	assert.Equal(t, "ops_admin", dto.Username)
	assert.Equal(t, "ROLE_ADMIN", dto.RoleID)
	assert.Equal(t, "all", dto.Scope)

	claims, err := e.signer.Verify(dto.Token)
	require.NoError(t, err)
	assert.Equal(t, "A0001", claims.Subject)
	assert.Equal(t, "ops_admin", claims.Username)
	assert.Equal(t, "ROLE_ADMIN", claims.RoleID)
}

// TestLoginCompatMixedCosts 多成本混合兼容测试（cost8/cost9/cost10）
func TestLoginCompatMixedCosts(t *testing.T) {
	t.Parallel()

	const testPwd = "TestPassword1!"

	testCases := []struct {
		cost int
		name string
	}{
		{8, "cost8"},
		{9, "cost9"},
		{10, "cost10"},
	}

	for _, tc := range testCases {
		tc := tc // capture for closure
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hash, err := bcrypt.GenerateFromPassword([]byte(testPwd), tc.cost)
			require.NoError(t, err)

			signer, err := token.NewSigner(testJWTSecret, time.Hour)
			require.NoError(t, err)
			store := &fakeStore{
				admin: &repo.AdminRow{
					AdminID: "A0002", Username: "test_admin", Name: "测试账号",
					PasswordHash: string(hash), RoleID: "ROLE_ADMIN", Status: "enabled",
				},
				scope: "all",
			}
			h := New(store, signer, nil)

			w := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"test_admin","password":"`+testPwd+`"}`))
			w.Header = http.Header{"Content-Type": []string{"application/json"}}
			rec := httptest.NewRecorder()
			h.Router().ServeHTTP(rec, w)

			assert.Equal(t, 200, rec.Code, "%s 应登录成功", tc.name)
			if rec.Code != 200 {
				t.Logf("response body: %s", rec.Body.String())
			}
		})
	}
}
