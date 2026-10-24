// T706 厂商面 handler 测试（H 层）：拿已鉴权厂商上下文（网关注入的三头），只测业务分支。
// 覆盖 M01-M05 + 批量三态 + 零明文 + 跨厂商 404 同形。
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
	"github.com/bracesync/bracesync/services/device-service/internal/service"
	"github.com/bracesync/bracesync/services/device-service/internal/testutil"
)

const (
	vendorAID = "VND000001"
	vendorBID = "VND000002"
	devA1     = "PRS-ML05-RC-20261024001"
	devB1     = "PRS-ML05-RB-20261024001"
	devA2     = "PRS-ML05-RC-20261024002"
)

type fakeVendorStore struct {
	vendors   map[string]*repo.Vendor
	keys      map[string]*repo.VendorAPIKey
	devices   map[string]*repo.VendorDevice // key = vendorID+"/"+deviceID
	audits    []repo.VendorAuditInput
	configs   map[string]int
	nextSeq   int
	suspended []string
}

func newFakeVendorStore() *fakeVendorStore {
	return &fakeVendorStore{
		vendors: map[string]*repo.Vendor{
			vendorAID: {VendorID: vendorAID, Name: "厂商A", Models: []string{"PRS-ML05-RC"}, Status: "active"},
			vendorBID: {VendorID: vendorBID, Name: "厂商B", Models: []string{"PRS-ML05-RB"}, Status: "active"},
		},
		keys:    map[string]*repo.VendorAPIKey{},
		devices: map[string]*repo.VendorDevice{},
		configs: map[string]int{},
	}
}

func (f *fakeVendorStore) NextVendorID(context.Context) (string, error) {
	f.nextSeq++
	return fmt.Sprintf("VND%06d", 100+f.nextSeq), nil
}
func (f *fakeVendorStore) CreateVendor(_ context.Context, v *repo.Vendor) error {
	f.vendors[v.VendorID] = v
	return nil
}
func (f *fakeVendorStore) GetVendor(_ context.Context, id string) (*repo.Vendor, error) {
	v, ok := f.vendors[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return v, nil
}
func (f *fakeVendorStore) ListVendors(context.Context) ([]repo.Vendor, error) {
	out := make([]repo.Vendor, 0, len(f.vendors))
	for _, v := range f.vendors {
		out = append(out, *v)
	}
	return out, nil
}
func (f *fakeVendorStore) CreateVendorAPIKey(_ context.Context, k *repo.VendorAPIKey) error {
	f.keys[k.KeyID] = k
	return nil
}
func (f *fakeVendorStore) GetVendorAPIKey(_ context.Context, id string) (*repo.VendorAPIKey, error) {
	k, ok := f.keys[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return k, nil
}
func (f *fakeVendorStore) ListVendorAPIKeys(_ context.Context, vendorID string) ([]repo.VendorAPIKey, error) {
	out := []repo.VendorAPIKey{}
	for _, k := range f.keys {
		if k.VendorID == vendorID {
			out = append(out, *k)
		}
	}
	return out, nil
}
func (f *fakeVendorStore) CountActiveVendorKeys(_ context.Context, vendorID string, now time.Time) (int, error) {
	n := 0
	for _, k := range f.keys {
		if k.VendorID == vendorID && k.Status == "active" && now.Before(k.ExpiresAt) {
			n++
		}
	}
	return n, nil
}
func (f *fakeVendorStore) RevokeVendorAPIKey(_ context.Context, keyID, reason string, now time.Time) (string, bool, error) {
	k, ok := f.keys[keyID]
	if !ok {
		return "", false, repo.ErrNotFound
	}
	if k.Status == "revoked" {
		return k.VendorID, false, nil
	}
	k.Status = "revoked"
	k.RevokedAt = &now
	k.RevokeReason = &reason
	return k.VendorID, true, nil
}
func (f *fakeVendorStore) VendorKeyCandidates(_ context.Context, prefix string) ([]repo.VendorKeyCandidate, error) {
	out := []repo.VendorKeyCandidate{}
	for _, k := range f.keys {
		if strings.HasPrefix(k.KeyPrefix, prefix) {
			out = append(out, repo.VendorKeyCandidate{
				KeyID: k.KeyID, VendorID: k.VendorID, KeyHash: k.KeyHash, Status: k.Status,
				ExpiresAt: k.ExpiresAt, VendorStatus: f.vendors[k.VendorID].Status,
			})
		}
	}
	return out, nil
}
func (f *fakeVendorStore) TouchVendorKey(context.Context, string, time.Time) error { return nil }
func (f *fakeVendorStore) RegisterVendorDevice(_ context.Context, d *repo.VendorDevice, vendorID string) (bool, error) {
	key := vendorID + "/" + d.DeviceID
	if _, exists := f.devices[key]; exists {
		return false, nil
	}
	for otherKey := range f.devices {
		if strings.HasSuffix(otherKey, "/"+d.DeviceID) {
			return false, nil // 他厂商名下：冲突，服务层再读本厂商判定
		}
	}
	d.CreatedAt = time.Now()
	f.devices[key] = d
	return true, nil
}
func (f *fakeVendorStore) GetVendorDevice(_ context.Context, vendorID, deviceID string) (*repo.VendorDevice, error) {
	d, ok := f.devices[vendorID+"/"+deviceID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return d, nil
}
func (f *fakeVendorStore) ListVendorDevices(_ context.Context, vendorID, status string, page, pageSize int) ([]repo.VendorDevice, int64, error) {
	all := []repo.VendorDevice{}
	for key, d := range f.devices {
		if !strings.HasPrefix(key, vendorID+"/") {
			continue
		}
		if status != "" && d.Status != status {
			continue
		}
		all = append(all, *d)
	}
	total := int64(len(all))
	start := (page - 1) * pageSize
	if start >= len(all) {
		return []repo.VendorDevice{}, total, nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}
func (f *fakeVendorStore) CountVendorSecretFetchesSince(_ context.Context, vendorID string, since time.Time) (int64, error) {
	var n int64
	for _, a := range f.audits {
		if a.Action == service.VendorActionSecretRead && a.OperatorID == vendorID {
			if ch, _ := a.Detail["channel"].(string); ch == "by-request" {
				n++
			}
		}
	}
	return n, nil
}
func (f *fakeVendorStore) CountVendorDenialsSince(_ context.Context, keyID string, since time.Time) (int64, error) {
	var n int64
	for _, a := range f.audits {
		if a.Action == "vendor_denied" && a.Detail["key_id"] == keyID {
			n++
		}
	}
	return n, nil
}
func (f *fakeVendorStore) SuspendVendorKey(_ context.Context, keyID string) error {
	if k, ok := f.keys[keyID]; ok {
		k.Status = "suspended"
	}
	f.suspended = append(f.suspended, keyID)
	return nil
}
func (f *fakeVendorStore) SysConfigInt(_ context.Context, key string, def int) int {
	if v, ok := f.configs[key]; ok {
		return v
	}
	return def
}
func (f *fakeVendorStore) WriteVendorAudit(_ context.Context, in repo.VendorAuditInput) error {
	f.audits = append(f.audits, in)
	return nil
}
func (f *fakeVendorStore) WriteVendorAuditBatch(_ context.Context, ins []repo.VendorAuditInput) error {
	f.audits = append(f.audits, ins...)
	return nil
}

func newVendorEnv(t *testing.T) (*gin.Engine, *fakeVendorStore, *crypto.Encryptor) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	fs := testutil.NewFakeStore()
	svc := service.NewDeviceService(fs, enc)
	vs := newFakeVendorStore()
	svc.SetVendorStore(vs, "t706-test-pepper")

	// A 名下已注册设备（密文与线上同一条 AES-GCM 通路），并绑定一位患者用于 bound 断言
	encSecret, err := enc.Encrypt([]byte(strings.Repeat("a1", 32)))
	require.NoError(t, err)
	patient := "P001"
	vs.devices[vendorAID+"/"+devA1] = &repo.VendorDevice{
		DeviceID: devA1, Model: "PRS-ML05-RC", FirmwareVersion: "v1.2.0",
		Status: "unbound", PatientID: &patient, SecretEnc: encSecret, CreatedAt: time.Now(),
	}
	encB, err := enc.Encrypt([]byte(strings.Repeat("b2", 32)))
	require.NoError(t, err)
	vs.devices[vendorBID+"/"+devB1] = &repo.VendorDevice{
		DeviceID: devB1, Model: "PRS-ML05-RB", FirmwareVersion: "v1.2.0",
		Status: "unbound", SecretEnc: encB, CreatedAt: time.Now(),
	}
	return New(svc).Router(), vs, enc
}

func vendorHeaders(vendorID string) map[string]string {
	return map[string]string{
		"X-Vendor-Id":         vendorID,
		"X-Vendor-Key-Prefix": "bsk_" + vendorID + "_",
		"X-Vendor-Key-Id":     "VK" + vendorID + "0001",
	}
}

func doVendor(t *testing.T, r *gin.Engine, method, path, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// M01：批量三态 + 服务端强制归属（请求体 vendor_id 被忽略）。
func TestVendor_T706_M01_BatchRegister(t *testing.T) {
	r, vs, _ := newVendorEnv(t)
	body := `{"vendor_id":"` + vendorBID + `","model":"PRS-ML05-RC","devices":[` +
		`{"deviceId":"` + devA2 + `","firmware":"v1.2.0"},` +
		`{"deviceId":"` + devA1 + `","firmware":"v1.2.0"},` +
		`{"deviceId":"BAD-ID","firmware":"v1.2.0"},` +
		`{"deviceId":"PRS-ML05-RC-20261024003","firmware":"v1.2.0"},` +
		`{"deviceId":"PRS-ML05-RC-20261024003","firmware":"v1.2.0"}]}`

	code, raw := doVendor(t, r, http.MethodPost, "/api/v1/vendor/devices", body, vendorHeaders(vendorAID))
	require.Equal(t, http.StatusOK, code, raw)

	var resp struct {
		Code int                       `json:"code"`
		Data service.VendorBatchResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	assert.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Results, 5)
	assert.Equal(t, service.VendorItemCreated, resp.Data.Results[0].Status)
	assert.Regexp(t, `^[0-9a-f]{64}$`, resp.Data.Results[0].Secret)
	assert.Equal(t, service.VendorItemExisted, resp.Data.Results[1].Status)
	assert.Empty(t, resp.Data.Results[1].Secret, "existed 不得重发密钥")
	assert.Equal(t, service.VendorItemFailed, resp.Data.Results[2].Status)
	assert.Equal(t, service.VendorSlugDeviceIDInvalid, resp.Data.Results[2].Code)
	assert.Equal(t, service.VendorItemCreated, resp.Data.Results[3].Status)
	assert.Equal(t, service.VendorItemExisted, resp.Data.Results[4].Status, "同批重复按 existed，明文面不扩大")
	assert.Empty(t, resp.Data.Results[4].Secret)
	assert.Equal(t, 2, resp.Data.Summary.Created)
	assert.Equal(t, 2, resp.Data.Summary.Existed)
	assert.Equal(t, 1, resp.Data.Summary.Failed)

	// 强制归属：新设备只落在 A 名下，B 名下无此设备
	_, err := vs.GetVendorDevice(context.Background(), vendorBID, devA2)
	assert.ErrorIs(t, err, repo.ErrNotFound)
	_, err = vs.GetVendorDevice(context.Background(), vendorAID, devA2)
	assert.NoError(t, err)

	// 审计：一条注册行（三态计数）+ 每条 created 一条 on-register fetch
	var register, onRegister int
	for _, a := range vs.audits {
		switch a.Action {
		case service.VendorActionRegister:
			register++
			assert.Equal(t, 5, a.Detail["requested"])
			assert.Equal(t, 2, a.Detail["created"])
		case service.VendorActionSecretRead:
			if a.Detail["channel"] == "on-register" {
				onRegister++
			}
		}
		assert.NotContains(t, fmt.Sprint(a.Detail), "a1a1a1", "审计不得出现 secret 明文")
	}
	assert.Equal(t, 1, register)
	assert.Equal(t, 2, onRegister)
}

func TestVendor_T706_M01_ModelNotAuthorized(t *testing.T) {
	r, _, _ := newVendorEnv(t)
	body := `{"model":"PRS-ML05-RB","devices":[{"deviceId":"PRS-ML05-RB-20261024001","firmware":"v1"}]}`
	code, raw := doVendor(t, r, http.MethodPost, "/api/v1/vendor/devices", body, vendorHeaders(vendorAID))
	require.Equal(t, http.StatusOK, code, raw)
	var resp struct {
		Data service.VendorBatchResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	assert.Equal(t, service.VendorItemFailed, resp.Data.Results[0].Status)
	assert.Equal(t, service.VendorSlugModelNotAuthorized, resp.Data.Results[0].Code)
}

func TestVendor_T706_BatchTooLarge(t *testing.T) {
	r, vs, _ := newVendorEnv(t)
	vs.configs["vendor_batch_max_devices"] = 2
	body := `{"model":"PRS-ML05-RC","devices":[{"deviceId":"PRS-ML05-RC-20261024001"},{"deviceId":"PRS-ML05-RC-20261024002"},{"deviceId":"PRS-ML05-RC-20261024003"}]}`
	code, raw := doVendor(t, r, http.MethodPost, "/api/v1/vendor/devices", body, vendorHeaders(vendorAID))
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, raw, service.VendorSlugBatchTooLarge)
}

// M02：补取 secret 必带 reason，每次成功落审计。
func TestVendor_T706_M02_SecretRefetch(t *testing.T) {
	r, vs, _ := newVendorEnv(t)

	code, raw := doVendor(t, r, http.MethodGet,
		"/api/v1/vendor/devices/"+devA1+"/secret?reason=", "", vendorHeaders(vendorAID))
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, raw, service.VendorSlugReasonRequired)

	code, raw = doVendor(t, r, http.MethodGet,
		"/api/v1/vendor/devices/"+devA1+"/secret?reason=%E4%BA%A7%E7%BA%BF%E9%87%8D%E7%83%A7", "",
		vendorHeaders(vendorAID))
	require.Equal(t, http.StatusOK, code, raw)
	var resp struct {
		Data struct {
			DeviceID string `json:"deviceId"`
			Secret   string `json:"secret"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	assert.Equal(t, devA1, resp.Data.DeviceID)
	assert.Equal(t, strings.Repeat("a1", 32), resp.Data.Secret)

	var n int
	for _, a := range vs.audits {
		if a.Action == service.VendorActionSecretRead && a.Detail["channel"] == "by-request" {
			n++
			assert.Equal(t, devA1, a.TargetID)
			assert.Equal(t, "VENDOR", a.OperatorRole)
			assert.NotContains(t, fmt.Sprint(a.Detail), resp.Data.Secret)
		}
	}
	assert.Equal(t, 1, n, "每次成功补取都必须落一条 by-request 审计")
}

// M03：元数据字段面固定，不含患者标识；bound 是 bool。
func TestVendor_T706_M03_Metadata(t *testing.T) {
	r, _, _ := newVendorEnv(t)
	code, raw := doVendor(t, r, http.MethodGet, "/api/v1/vendor/devices/"+devA1, "", vendorHeaders(vendorAID))
	require.Equal(t, http.StatusOK, code, raw)
	var resp struct {
		Data service.VendorDeviceView `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	assert.Equal(t, devA1, resp.Data.DeviceID)
	assert.Equal(t, "PRS-ML05-RC", resp.Data.Model)
	assert.Equal(t, "v1.2.0", resp.Data.FirmwareVersion)
	assert.True(t, resp.Data.Bound, "绑定状态以 bool 暴露")

	assert.NotContains(t, strings.ToLower(raw), "patient")
	assert.NotContains(t, raw, "P001")
	assert.NotRegexp(t, regexp.MustCompile(`1[3-9]\d{9}`), raw, "不得出现手机号")
	assert.NotRegexp(t, regexp.MustCompile(`\d{17}[\dXx]`), raw, "不得出现 18 位身份证")
}

// M04：列表恒被 vendor_id 收窄，B 的设备不出现。
func TestVendor_T706_M04_ListScoped(t *testing.T) {
	r, _, _ := newVendorEnv(t)
	code, raw := doVendor(t, r, http.MethodGet, "/api/v1/vendor/devices?status=&page=1&pageSize=20", "",
		vendorHeaders(vendorAID))
	require.Equal(t, http.StatusOK, code, raw)
	var resp struct {
		Data struct {
			List  []service.VendorDeviceView `json:"list"`
			Total int64                      `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	require.Len(t, resp.Data.List, 1)
	assert.Equal(t, int64(1), resp.Data.Total)
	assert.Equal(t, devA1, resp.Data.List[0].DeviceID)
	assert.NotContains(t, raw, devB1)
}

// M05 / P-1：跨厂商与不存在的设备逐字同形（同码同句）。
func TestVendor_T706_M05_CrossVendorSameShape(t *testing.T) {
	r, _, _ := newVendorEnv(t)
	hdr := vendorHeaders(vendorAID)

	for _, path := range []string{
		"/api/v1/vendor/devices/" + devB1,
		"/api/v1/vendor/devices/PRS-ML05-RC-20261024999",
	} {
		code, raw := doVendor(t, r, http.MethodGet, path, "", hdr)
		assert.Equal(t, http.StatusNotFound, code, path)
		assert.Contains(t, raw, service.VendorSlugDeviceNotFound, path)
		if path == "/api/v1/vendor/devices/"+devB1 {
			for _, other := range []string{
				"/api/v1/vendor/devices/" + devB1 + "/secret?reason=x",
				"/api/v1/vendor/devices/PRS-ML05-RC-20261024999/secret?reason=x",
			} {
				c2, r2 := doVendor(t, r, http.MethodGet, other, "", hdr)
				assert.Equal(t, http.StatusNotFound, c2)
				assert.Contains(t, r2, service.VendorSlugDeviceNotFound)
			}
		}
	}

	// 同一句失败报文：跨厂商 vs 不存在，body 去掉 trace 后逐字相等
	_, cross := doVendor(t, r, http.MethodGet, "/api/v1/vendor/devices/"+devB1, "", hdr)
	_, missing := doVendor(t, r, http.MethodGet, "/api/v1/vendor/devices/PRS-ML05-RC-20261024999", "", hdr)
	assert.Equal(t, missing, cross, "跨厂商与不存在必须同形，不给枚举增量")
}

func TestVendor_T706_MissingGatewayIdentity_401(t *testing.T) {
	r, _, _ := newVendorEnv(t)
	code, raw := doVendor(t, r, http.MethodGet, "/api/v1/vendor/devices", "", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Contains(t, raw, "invalid_key")
}
