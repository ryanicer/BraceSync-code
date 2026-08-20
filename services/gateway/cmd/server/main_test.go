// Package main provides HTTP-layer tests for the gateway service.
// 对齐：docs/ §1 (HTTP 层测试)
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthzEndpoint(t *testing.T) {
	router := setupRouter()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status"`)
	assert.Contains(t, w.Body.String(), `"ok"`)
}

func TestHealthzMethodNotAllowed(t *testing.T) {
	router := setupRouter()

	// POST 到 /healthz 应返回 405（Gin 默认行为）或 404
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Gin 路由未匹配时返回 404；若将来严格限制方法则改 405
	assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed,
		"expected 404 or 405, got %d", w.Code)
}

func TestGatewayRouterNotNil(t *testing.T) {
	router := setupRouter()
	assert.NotNil(t, router, "gateway router should not be nil")
}
