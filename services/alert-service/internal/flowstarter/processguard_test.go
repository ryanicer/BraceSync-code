package flowstarter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessGuard_Decisions(t *testing.T) {
	cases := []struct {
		name     string
		data     map[string]any
		enforced bool
		allowed  bool
	}{
		{"无实例不执法", map[string]any{"enforced": false, "allowed": true}, false, true},
		{"绑定且角色匹配", map[string]any{"enforced": true, "allowed": true, "currentNodeId": "N2"}, true, true},
		{"绑定但角色不匹配", map[string]any{"enforced": true, "allowed": false, "reason": "当前账号角色无权处理该节点（要求处理角色：主治医生）"}, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got processGuardRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/internal/flow/process-guard", r.URL.Path)
				require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				writeJSON2(w, map[string]any{"code": 0, "data": c.data})
			}))
			defer srv.Close()

			g := NewProcessGuard(srv.URL, time.Second)
			d, err := g.CheckProcess(context.Background(), 9527, "D0001", "ROLE_DOCTOR")
			require.NoError(t, err)
			assert.Equal(t, int64(9527), got.AlertID)
			assert.Equal(t, "D0001", got.OperatorID)
			assert.Equal(t, "ROLE_DOCTOR", got.Role)
			assert.Equal(t, c.enforced, d.Enforced)
			assert.Equal(t, c.allowed, d.Allowed)
			if !c.allowed {
				assert.Contains(t, d.Reason, "主治医生")
			}
		})
	}
}

func TestProcessGuard_FailClosedOnErrors(t *testing.T) {
	t.Run("500 非200 → error（调用方回503，不旁路）", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		_, err := NewProcessGuard(srv.URL, time.Second).CheckProcess(context.Background(), 1, "X", "ROLE_CS")
		assert.Error(t, err)
	})
	t.Run("坏 JSON → error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("{not-json"))
		}))
		defer srv.Close()
		_, err := NewProcessGuard(srv.URL, time.Second).CheckProcess(context.Background(), 1, "X", "ROLE_CS")
		assert.Error(t, err)
	})
	t.Run("服务不可达 → error", func(t *testing.T) {
		_, err := NewProcessGuard("http://127.0.0.1:1", 200*time.Millisecond).CheckProcess(context.Background(), 1, "X", "ROLE_CS")
		assert.Error(t, err)
	})
	t.Run("code 非 0 → error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON2(w, map[string]any{"code": 500, "message": "boom"})
		}))
		defer srv.Close()
		_, err := NewProcessGuard(srv.URL, time.Second).CheckProcess(context.Background(), 1, "X", "ROLE_CS")
		assert.Error(t, err)
	})
}

func TestNoopGuard_AlwaysAllowed(t *testing.T) {
	d, err := NoopGuard{}.CheckProcess(context.Background(), 1, "X", "whatever")
	require.NoError(t, err)
	assert.False(t, d.Enforced)
	assert.True(t, d.Allowed)
}

func writeJSON2(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
