package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func evalReq() *AlertEvalRequest {
	return &AlertEvalRequest{
		DeviceID:   "DEV1",
		PatientID:  "P1",
		Timestamp:  time.Now().UTC(),
		Points:     make([]float64, 20),
		UploadTime: time.Now().UTC(),
	}
}

func TestHTTPAlertClient_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/evaluate", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"shouldAlert":true,"alertType":"pressure_high","sensorPoint":"P03"}}`))
	}))
	defer srv.Close()

	client := NewHTTPAlertClient(srv.URL, time.Second)
	res, err := client.Evaluate(context.Background(), evalReq())
	require.NoError(t, err)
	assert.True(t, res.ShouldAlert)
	assert.Equal(t, "pressure_high", res.AlertType)
	assert.Equal(t, "P03", res.SensorPoint)
}

func TestHTTPAlertClient_SuccessNilData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"message":"success"}`))
	}))
	defer srv.Close()

	client := NewHTTPAlertClient(srv.URL, time.Second)
	res, err := client.Evaluate(context.Background(), evalReq())
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.False(t, res.ShouldAlert)
}

func TestHTTPAlertClient_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewHTTPAlertClient(srv.URL, time.Second)
	_, err := client.Evaluate(context.Background(), evalReq())
	require.Error(t, err)
}

func TestHTTPAlertClient_BusinessCodeNonZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":40001,"message":"rule config missing"}`))
	}))
	defer srv.Close()

	client := NewHTTPAlertClient(srv.URL, time.Second)
	_, err := client.Evaluate(context.Background(), evalReq())
	require.Error(t, err)
}

func TestHTTPAlertClient_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	client := NewHTTPAlertClient(srv.URL, time.Second)
	_, err := client.Evaluate(context.Background(), evalReq())
	require.Error(t, err)
}

func TestHTTPAlertClient_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	// 100ms 熔断超时（架构 §3.4）
	client := NewHTTPAlertClient(srv.URL, 100*time.Millisecond)
	start := time.Now()
	_, err := client.Evaluate(context.Background(), evalReq())
	require.Error(t, err)
	assert.Less(t, time.Since(start), 200*time.Millisecond, "timeout must trip before server responds")
}

func TestHTTPAlertClient_ConnectionRefused(t *testing.T) {
	client := NewHTTPAlertClient("http://127.0.0.1:1", 200*time.Millisecond)
	_, err := client.Evaluate(context.Background(), evalReq())
	require.Error(t, err)
}

func TestNoopAlertClient(t *testing.T) {
	_, err := NoopAlertClient{}.Evaluate(context.Background(), evalReq())
	require.Error(t, err, "Noop client must always fail to trigger degradation")
}
