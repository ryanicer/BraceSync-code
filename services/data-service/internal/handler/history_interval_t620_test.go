package handler

// T620：records 端点的 interval 参数面（handler 只做透传与放行，档位合法性在 service 层判）。

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

func TestGetHistory_IntervalPassthrough(t *testing.T) {
	srv := newTestServer(nil)
	staffHdr := map[string]string{headerRole: roleAdmin}

	// interval 缺席：走原明细分页读路，桶读路一次都不碰
	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/records?date=2026-08-08&period=day", "", staffHdr)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 20, srv.records.lastPageSize)
	assert.Equal(t, 0, srv.records.lastBucketSeconds)

	// interval=30m → 桶宽 1800 秒落到 repo 契约
	w = doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/records?date=2026-08-08&period=day&interval=30m", "", staffHdr)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1800, srv.records.lastBucketSeconds)
	code, data := decodeBody(t, w)
	assert.Equal(t, 0, code)
	assert.NotNil(t, data["list"])
}

func TestGetHistory_IntervalRejected(t *testing.T) {
	srv := newTestServer(nil)
	staffHdr := map[string]string{headerRole: roleAdmin}

	// 未知档
	w := doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/records?date=2026-08-08&period=day&interval=5m", "", staffHdr)
	require.Equal(t, http.StatusBadRequest, w.Code)
	code, _ := decodeBody(t, w)
	assert.Equal(t, model.CodeQueryParam, code)
	assert.Equal(t, 0, srv.records.lastBucketSeconds)

	// 合法档 + 月窗 = 桶数超上限（档位判据在 service，handler 只透传，这里证 400 出得来）
	w = doReq(t, srv, http.MethodGet, "/api/v1/patients/"+hPatient+"/records?date=2026-08-08&period=month&interval=30m", "", staffHdr)
	require.Equal(t, http.StatusBadRequest, w.Code)
	code, _ = decodeBody(t, w)
	assert.Equal(t, model.CodeQueryParam, code)
}
