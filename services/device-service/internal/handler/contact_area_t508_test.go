// T508 面积配置写端点：PUT /api/v1/devices/:deviceId/contact-area。
//
// 本文件守四件事（角色门禁本身由 write_scope_t387_test.go 的端点表覆盖，不重复）：
//  1. 整值覆盖 + 详情回显同源（配完立刻能被设备管理页读回，PRD §7A.2.1 五.1「下一次读数即生效」），
//     以及列表 DTO 不把该列投影丢掉；
//  2. 未配置回显是 null 而不是 0.64 —— 这一格是「默认值属后台表单项、不落库不兜底」的唯一现网证据；
//  3. 非法载荷（省略键 / 显式 null / 0 / 负 / 非 JSON）一律 400 + 20400，且库里原值不动：
//     本列没有回退成 NULL 的通路，「不表态」不能被静默读成清空；
//  4. 未注册设备 404 + 20404（判定序：门禁 > 体解析 > 参数 > 仓储，查无只在最后一步）。
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
	"github.com/bracesync/bracesync/services/device-service/internal/testutil"
)

// t508Detail 读设备详情，返回原始 data 与解析后的 DTO。
// 面积断言必须同时看原文：DTO 的 nil 与「字段根本没投影」在结构体里同形，原文里不一样。
func t508Detail(t *testing.T, env *testEnv, deviceID string) (json.RawMessage, model.DeviceDTO) {
	t.Helper()
	status, resp := env.do(t, http.MethodGet, "/api/v1/devices/"+deviceID, nil, nil)
	require.Equal(t, http.StatusOK, status, "详情读夹具失败: %s", resp.Message)
	require.NotEmpty(t, resp.Data, "详情 data 为空，无从判 shape")
	var dto model.DeviceDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto), "data=%s", string(resp.Data))
	return resp.Data, dto
}

func t508RegisterDev(t *testing.T, env *testEnv, deviceID string) {
	t.Helper()
	// 不给身份头：testEnv 的 do 对技师安装流程用例按技师代发（见 handler_http_test.go 头注）
	status, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": deviceID}, nil)
	require.Equal(t, http.StatusOK, status, "注册夹具失败: code=%d msg=%s", resp.Code, resp.Message)
}

func TestT508_ContactArea_WriteThenReadBackSameValue(t *testing.T) {
	env := newTestEnv(t)
	const dev = "DEV-T508-A"
	t508RegisterDev(t, env, dev)

	status, resp := env.do(t, http.MethodPut, "/api/v1/devices/"+dev+"/contact-area",
		map[string]float64{"contactAreaCm2": 0.64}, nil)
	require.Equal(t, http.StatusOK, status, "code=%d msg=%s", resp.Code, resp.Message)
	assert.Equal(t, model.CodeOK, resp.Code)

	raw, dto := t508Detail(t, env, dev)
	require.NotNil(t, dto.ContactAreaCm2, "配置后详情必须回显面积")
	assert.InDelta(t, 0.64, *dto.ContactAreaCm2, 1e-9)
	assert.Contains(t, string(raw), `"contactAreaCm2":0.64`)

	// 整值覆盖：再次配置是新值，不是与旧值合并/取平均
	_, _ = env.do(t, http.MethodPut, "/api/v1/devices/"+dev+"/contact-area",
		map[string]float64{"contactAreaCm2": 1.5}, nil)
	_, after := t508Detail(t, env, dev)
	require.NotNil(t, after.ContactAreaCm2)
	assert.InDelta(t, 1.5, *after.ContactAreaCm2, 1e-9, "PUT 是整写，第二次配置必须完全替换")

	// 写通路唯一：面积只经 SetContactArea 落库，注册与绑定等其他写路径不得顺带改它
	dev2 := "DEV-T508-A2"
	t508RegisterDev(t, env, dev2)
	_, d2 := t508Detail(t, env, dev2)
	assert.Nil(t, d2.ContactAreaCm2, "新注册设备必须是未配置态（注册 INSERT 不含该列）")
}

// 列表端点必须把面积列透到 wire（前端设备管理页列表列由此取数）。
//
// 列表走 repo.ListStore（另一条 PG 读路，见 repo/query.go:127 的 scan），与写路的 FakeStore
// 不是同一个数据源，所以本用例只能验「repo 行 → 列表 DTO」这一段是否漏投影；
// 「列表 SQL 真选了这一列」由 write_deny_integration_t389_test.go 的 contactArea 快照腿覆盖。
func TestT508_ContactArea_ListProjectionNotDropped(t *testing.T) {
	area := 0.8
	store := &fakeListStore{
		devices: []repo.DeviceListItem{{
			DeviceID: "DEV-T508-LIST", ContactAreaCm2: &area, Status: "online",
		}},
		deviceTotal: 1,
	}
	h := newQueryEnv(t, store)

	w := doGet(t, h, "/api/v1/devices?keyword=DEV-T508-LIST")
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, model.CodeOK, resp.Code)

	// 键名必须与详情 DeviceDTO 完全一致（T356 门禁的「详情键 ⊆ 列表键」由此成立）
	assert.Contains(t, string(resp.Data), `"contactAreaCm2":0.8`, "列表投影没带面积值：%s", string(resp.Data))

	var page struct {
		List []struct {
			DeviceID       string   `json:"deviceId"`
			ContactAreaCm2 *float64 `json:"contactAreaCm2"`
		} `json:"list"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &page), "data=%s", string(resp.Data))
	require.Len(t, page.List, 1)
	require.NotNil(t, page.List[0].ContactAreaCm2, "列表 DTO 把面积洗成 null")
	assert.InDelta(t, 0.8, *page.List[0].ContactAreaCm2, 1e-9)

	// 未配置行同样要出 null 键，不能省键（前端据此区分「未配置」与「字段没投影」）
	store.devices = []repo.DeviceListItem{{DeviceID: "DEV-T508-LIST", Status: "online"}}
	w2 := doGet(t, h, "/api/v1/devices?keyword=DEV-T508-LIST")
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), `"contactAreaCm2":null`, "未配置必须序列化成 null 而非省键")
	assert.NotContains(t, w2.Body.String(), "0.64", "列表侧也不得用后台默认值补位")
}

// 未配置 = null，不是 0.64：这条是「默认值不落库、不作读侧兜底」（PRD §7A.2.1 五.3）的现网证据。
func TestT508_ContactArea_UnconfiguredIsNullNotDefault(t *testing.T) {
	env := newTestEnv(t)
	const dev = "DEV-T508-NULL"
	t508RegisterDev(t, env, dev)

	raw, dto := t508Detail(t, env, dev)
	assert.Nil(t, dto.ContactAreaCm2, "未配置必须回 null")
	assert.Contains(t, string(raw), `"contactAreaCm2":null`, "null 要真的序列化出来，不是省键")
	assert.NotContains(t, string(raw), "0.64", "服务端不得用后台默认值补位")
}

// 非法载荷一律 400 + 20400，并且库里原值一动不动。
// 「省略键」与「显式 null」都要拒：整值覆盖路由最容易被静默读成清空。
func TestT508_ContactArea_RejectsInvalidPayload(t *testing.T) {
	env := newTestEnv(t)
	const dev = "DEV-T508-BAD"
	t508RegisterDev(t, env, dev)

	// 先配一个合法值，好让「拒绝路径不写库」这一格有可对照的原值
	st, rp := env.do(t, http.MethodPut, "/api/v1/devices/"+dev+"/contact-area",
		map[string]float64{"contactAreaCm2": 0.64}, nil)
	require.Equal(t, http.StatusOK, st, rp.Message)

	before := t508AreaOf(t, env, dev)
	require.NotNil(t, before)
	assert.InDelta(t, 0.64, *before, 1e-9)

	cases := []struct {
		name string
		body any
	}{
		{"省略键", map[string]any{}},
		{"显式null", map[string]any{"contactAreaCm2": nil}},
		{"零", map[string]float64{"contactAreaCm2": 0}},
		{"负数", map[string]float64{"contactAreaCm2": -0.64}},
		{"非数字", map[string]string{"contactAreaCm2": "0.64"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, resp := env.do(t, http.MethodPut, "/api/v1/devices/"+dev+"/contact-area", c.body, nil)
			assert.Equal(t, http.StatusBadRequest, status, "%s 必须 400", c.name)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			got := t508AreaOf(t, env, dev)
			require.NotNil(t, got, "%s 被拒后原值不得被洗成未配置", c.name)
			assert.InDelta(t, *before, *got, 1e-9, "%s 被拒后库里原值不得变化", c.name)
		})
	}
}

// t508AreaOf 读仓储里的面积现值（FakeStore 直读，绕开 DTO 层）
func t508AreaOf(t *testing.T, env *testEnv, deviceID string) *float64 {
	t.Helper()
	dev, err := env.store.GetDevice(context.Background(), deviceID)
	require.NoError(t, err)
	require.NotNil(t, dev, "设备应在库里")
	return dev.ContactAreaCm2
}

// 非 JSON 请求体也要 400（不该 panic 成 500）
func TestT508_ContactArea_MalformedJSONRejected(t *testing.T) {
	env := newTestEnv(t)
	const dev = "DEV-T508-MAL"
	t508RegisterDev(t, env, dev)

	httpStatus, code := env.doRaw(t, http.MethodPut, "/api/v1/devices/"+dev+"/contact-area", "not-json")
	assert.Equal(t, http.StatusBadRequest, httpStatus)
	assert.Equal(t, model.CodeInvalidParam, code)
	assert.Nil(t, t508AreaOf(t, env, dev))
}

// 未注册设备：门禁与体解析都放行后才判查无 → 404 + 20404
func TestT508_ContactArea_UnknownDevice404(t *testing.T) {
	env := newTestEnv(t)
	status, resp := env.do(t, http.MethodPut, "/api/v1/devices/DEV-T508-NOT-EXIST/contact-area",
		map[string]float64{"contactAreaCm2": 0.64}, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeNotFound, resp.Code)
}

var _ = testutil.TestEncKey // 夹具密钥常量在本文件的新 env 构造路径里经 newTestEnv 间接使用
