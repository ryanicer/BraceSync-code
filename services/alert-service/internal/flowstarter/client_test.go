// T653 flowstarter.HTTPClient 契约测试：请求体三态（started/unbound/exists）与失败静默。
//
// 关键安全语义：任何下游异常（拒连/非 200/坏 JSON/业务码非 0/超时）都不得外抛、
// 不得阻塞调用方（调用点在告警落库热路径上）。
package flowstarter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type t653Stub struct {
	mu       sync.Mutex
	bodies   []autoStartRequest
	status   int
	resp     string
	delay    time.Duration
	hitCount int
}

func (s *t653Stub) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body autoStartRequest
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.hitCount++
		st, payload, delay := s.status, s.resp, s.delay
		s.mu.Unlock()

		if r.URL.Path != "/internal/flow/auto-start" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		if st != 0 {
			w.WriteHeader(st)
		}
		_, _ = io.WriteString(w, payload)
	}
}

func (s *t653Stub) snapshot() ([]autoStartRequest, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]autoStartRequest{}, s.bodies...), s.hitCount
}

func TestT653_HTTPClient_PostsStartedAndUnboundAndExists(t *testing.T) {
	cases := []struct {
		name   string
		status int
		resp   string
	}{
		{"已绑定自动建实例", http.StatusOK, `{"code":0,"data":{"started":true,"reason":"started","instanceId":"FLOW_IABC"}}`},
		{"未绑定静默跳过", http.StatusOK, `{"code":0,"data":{"started":false,"reason":"unbound"}}`},
		{"已有实例幂等", http.StatusConflict, `{"code":10409,"data":{"started":false,"reason":"exists","instanceId":"FLOW_IOLD"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &t653Stub{status: tc.status, resp: tc.resp}
			srv := httptest.NewServer(stub.handler())
			defer srv.Close()

			c := New(srv.URL, 0, zerolog.Nop())
			c.AutoStart(context.Background(), "9527", "pressure_high") // 三态都不得 panic / 阻塞

			bodies, hits := stub.snapshot()
			require.Equal(t, 1, hits)
			assert.Equal(t, autoStartRequest{AlertID: "9527", AlertType: "pressure_high"}, bodies[0])
		})
	}
}

func TestT653_HTTPClient_FailuresAreSwallowed(t *testing.T) {
	t.Run("非200坏JSON", func(t *testing.T) {
		stub := &t653Stub{status: http.StatusInternalServerError, resp: `oops`}
		srv := httptest.NewServer(stub.handler())
		defer srv.Close()

		c := New(srv.URL, time.Second, zerolog.Nop())
		assert.NotPanics(t, func() { c.AutoStart(context.Background(), "1", "wear_interrupt") })
	})

	t.Run("下游超时仅熔断不抛错", func(t *testing.T) {
		stub := &t653Stub{status: http.StatusOK, resp: `{"code":0,"data":{"started":true}}`, delay: 300 * time.Millisecond}
		srv := httptest.NewServer(stub.handler())
		defer srv.Close()

		c := New(srv.URL, 50*time.Millisecond, zerolog.Nop())
		done := make(chan struct{})
		go func() { c.AutoStart(context.Background(), "1", "wear_interrupt"); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("AutoStart 被下游拖死，1s 熔断语义失效")
		}
	})

	t.Run("拒连静默", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close() // 立即关闭 → 连接被拒
		c := New(srv.URL, time.Second, zerolog.Nop())
		assert.NotPanics(t, func() { c.AutoStart(context.Background(), "1", "sensor_drift") })
	})
}

func TestT653_HTTPClient_Guards(t *testing.T) {
	stub := &t653Stub{status: http.StatusOK, resp: `{"code":0,"data":{"started":true}}`}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	c := New(srv.URL, 0, zerolog.Nop())
	c.AutoStart(context.Background(), "", "pressure_high")
	c.AutoStart(context.Background(), "1", "")

	c2 := New("", 0, zerolog.Nop())
	c2.AutoStart(context.Background(), "1", "pressure_high")

	(*HTTPClient)(nil).AutoStart(context.Background(), "1", "pressure_high")

	_, hits := stub.snapshot()
	assert.Equal(t, 0, hits, "空 alertID/alertType/baseURL/nil client 均不得发请求")
}
