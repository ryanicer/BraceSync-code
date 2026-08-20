// Package handler 登录并发性能基准与存量哈希兼容回归（T040）
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestLoginBenchSingleCost10 bcrypt cost10 单次校验耗时基准
func TestLoginBenchSingleCost10(t *testing.T) {
	t.Parallel()

	const targetMinMs, targetMaxMs = 50, 150
	pwd := []byte(itAdminPassword)
	hash := []byte(itAdminHashCost10)

	var times []time.Duration
	for i := 0; i < 10; i++ {
		start := time.Now()
		err := bcrypt.CompareHashAndPassword(hash, pwd)
		require.NoError(t, err)
		dur := time.Since(start)
		times = append(times, dur)
	}

	total := 0 * time.Microsecond
	for _, d := range times {
		total += d
	}
	avgDur := total / time.Duration(len(times))
	avgMs := avgDur.Milliseconds()

	t.Logf("bcrypt cost10 平均耗时：%d ms（样本数=10）", avgMs)

	if avgMs < targetMinMs {
		t.Errorf("cost10 平均耗时%dms < 最低要求%dms", avgMs, targetMinMs)
	}
	if avgMs > targetMaxMs {
		t.Errorf("cost10 平均耗时%dms > 最高允许%dms", avgMs, targetMaxMs)
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
