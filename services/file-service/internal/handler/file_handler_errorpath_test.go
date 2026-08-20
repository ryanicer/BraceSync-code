// Package handler 故障路径补充测试（T022 返工，覆盖率 ≥90%：store 失败 → 500 分支）
package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
	"github.com/bracesync/bracesync/services/file-service/internal/repo"
	"github.com/bracesync/bracesync/services/file-service/internal/service"
	"github.com/bracesync/bracesync/services/file-service/internal/storage"
)

var errDBDown = errors.New("database down")

// failStore 故障注入仓储：全部操作返回 errDBDown（模拟 PG 不可用）
type failStore struct{}

func (failStore) CreateFile(context.Context, *model.FileMetadata) error { return errDBDown }
func (failStore) MarkUploaded(context.Context, string, string, int64) error {
	return errDBDown
}
func (failStore) GetFileByFileID(context.Context, string) (*model.FileMetadata, error) {
	return nil, errDBDown
}
func (failStore) QueryFiles(context.Context, repo.QueryFilter) ([]model.FileMetadata, error) {
	return nil, errDBDown
}
func (failStore) CountFiles(context.Context, repo.QueryFilter) (int64, error) {
	return 0, errDBDown
}

// countFailStore QueryFiles 正常、CountFiles 失败（隔离 query/count 两条 500 分支）
type countFailStore struct {
	memStore
}

func (s *countFailStore) CountFiles(context.Context, repo.QueryFilter) (int64, error) {
	return 0, errDBDown
}

func setupFailServer(store repo.Store) *httptest.Server {
	svc := service.NewPresigner(storage.NewMockCOSClient(), store, "test-bucket", "ap-guangzhou")
	h := NewFileHandler(svc, store)
	return httptest.NewServer(h.Router())
}

func TestPresign_StoreFailure_500(t *testing.T) {
	srv := setupFailServer(failStore{})
	defer srv.Close()

	// COS 预签名成功但落库失败 → 500 61003
	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "T0001", "technician", presignBody)
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, float64(ErrorCodePresignFailed), body["code"])
}

func TestUploadComplete_StoreFailure_500(t *testing.T) {
	srv := setupFailServer(failStore{})
	defer srv.Close()

	// GetFileByFileID 返回非 NotFound 错误 → 500 61002
	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", "T0001", "technician",
		`{"file_id": "file_x", "size": 1}`)
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, float64(ErrorCodeUploadFailed), body["code"])
}

func TestGetFileByID_StoreFailure_500(t *testing.T) {
	srv := setupFailServer(failStore{})
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/file_x", "T0001", "technician")
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, float64(ErrorCodeInternal), body["code"])
}

func TestQuery_QueryFailure_500(t *testing.T) {
	srv := setupFailServer(failStore{})
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/query", "T0001", "technician")
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, float64(ErrorCodeInternal), body["code"])
}

func TestQuery_CountFailure_500(t *testing.T) {
	store := &countFailStore{}
	srv := setupFailServer(store)
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/query", "T0001", "technician")
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, float64(ErrorCodeInternal), body["code"])
}
