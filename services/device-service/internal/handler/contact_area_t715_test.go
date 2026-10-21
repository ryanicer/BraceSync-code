// T715 设备面积注册默认值：POST /api/v1/devices 注册即落 Boss 标称 0.64 cm²。
//
// 本文件守 PM 排的三条验收判据（TAPD 1133005753001000501）：
//  1. 新注册设备面积非空——kPa 换算的分母不再靠人工补（患者端「未配置面积」整片失效的根因）；
//  2. 现有设备回归——幂等注册（ON CONFLICT DO NOTHING）既不洗已配面积，也不覆盖既有密钥；
//  3. 编辑不受伤——默认值只是初值，PUT /contact-area 的整值覆盖与详情回显照常生效。
//
// T508 的「读侧不得用常量补位」口径一字未动（见 contact_area_t508_test.go 的 NULL 态用例）：
// 本卡只改注册面写侧，NULL 仍原样读出、仍 fail-closed。
package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// 判据一：注册出来的设备面积非空，且库里、wire 上都是 0.64。
func TestT715_RegisterWritesBossNominalArea(t *testing.T) {
	env := newTestEnv(t)
	const dev = "DEV-T715-A"

	status, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": dev}, nil)
	require.Equal(t, http.StatusOK, status, "注册应成功：%d %s", resp.Code, resp.Message)

	raw, dto := t508Detail(t, env, dev)
	require.NotNil(t, dto.ContactAreaCm2, "判据一：新注册设备面积不得为 null")
	assert.InDelta(t, model.DefaultContactAreaCm2, *dto.ContactAreaCm2, 1e-9,
		"默认值必须是 Boss 标称 0.64（单点 0.8cm×0.8cm）")
	assert.Contains(t, string(raw), `"contactAreaCm2":0.64`, "wire 上就是数值，不是省键也不是 null")

	// 库里那一行同样非空（详情 DTO 与仓储实体同形才说明不是读侧补出来的）
	stored, err := env.store.GetDevice(context.Background(), dev)
	require.NoError(t, err)
	require.NotNil(t, stored.ContactAreaCm2, "仓储实体面积为 null ⇒ 说明是读侧兜底，T715 禁止这种实现")
	assert.InDelta(t, model.DefaultContactAreaCm2, *stored.ContactAreaCm2, 1e-9)
}

// 判据二：已配过面积的设备再走一次注册，配置与密钥都不动。
func TestT715_ReRegisterKeepsAreaAndSecret(t *testing.T) {
	env := newTestEnv(t)
	const dev = "DEV-T715-B"
	t508RegisterDev(t, env, dev)

	status, resp := env.do(t, http.MethodPut, "/api/v1/devices/"+dev+"/contact-area",
		map[string]float64{"contactAreaCm2": 1.25}, nil)
	require.Equal(t, http.StatusOK, status, "预置合法面积失败：%s", resp.Message)

	before, err := env.store.GetDevice(context.Background(), dev)
	require.NoError(t, err)
	require.NotNil(t, before.ContactAreaCm2)

	// 同设备重复注册（幂等）：DO NOTHING 这一支必须整体不动既有行
	status2, resp2 := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": dev}, nil)
	require.Equal(t, http.StatusOK, status2, "重复注册应幂等返回既有记录：%s", resp2.Message)

	after, err := env.store.GetDevice(context.Background(), dev)
	require.NoError(t, err)
	require.NotNil(t, after.ContactAreaCm2, "幂等注册把面积洗成了 null")
	assert.InDelta(t, 1.25, *after.ContactAreaCm2, 1e-9, "判据二：幂等注册不得覆盖已配面积")
	assert.Equal(t, before.DeviceSecretEnc, after.DeviceSecretEnc, "判据二：幂等注册不得覆盖既有密钥")
	assert.Equal(t, before.SecretVersion, after.SecretVersion)
}

// 判据三：默认值不锁死编辑面，PUT 仍是整值覆盖且详情同源。
func TestT715_EditAfterDefaultStillWorks(t *testing.T) {
	env := newTestEnv(t)
	const dev = "DEV-T715-C"
	t508RegisterDev(t, env, dev)
	require.NotNil(t, t508AreaOf(t, env, dev), "前置：注册默认值应已在库里")

	status, resp := env.do(t, http.MethodPut, "/api/v1/devices/"+dev+"/contact-area",
		map[string]float64{"contactAreaCm2": 0.5}, nil)
	require.Equal(t, http.StatusOK, status, "判据三：默认值之上的编辑必须照常受理：%s", resp.Message)

	got := t508AreaOf(t, env, dev)
	require.NotNil(t, got)
	assert.InDelta(t, 0.5, *got, 1e-9, "PUT 是整写，不是与默认值取平均/保留其一")

	_, dto := t508Detail(t, env, dev)
	require.NotNil(t, dto.ContactAreaCm2)
	assert.InDelta(t, 0.5, *dto.ContactAreaCm2, 1e-9, "编辑后下一次读数即生效（PRD §7A.2.1 五.1）")
}

// 反面对照：非法面积值在默认值之上仍然被拒，且不落库（默认值不是「什么都接受」的借口）。
func TestT715_DefaultDoesNotLoosenValidation(t *testing.T) {
	env := newTestEnv(t)
	const dev = "DEV-T715-D"
	t508RegisterDev(t, env, dev)

	for _, bad := range []float64{0, -1} {
		status, resp := env.do(t, http.MethodPut, "/api/v1/devices/"+dev+"/contact-area",
			map[string]float64{"contactAreaCm2": bad}, nil)
		assert.Equal(t, http.StatusBadRequest, status, "面积 %v 应被拒", bad)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	}
	// 库里仍是注册默认值，两次非法写一个字节都没落地
	kept := t508AreaOf(t, env, dev)
	require.NotNil(t, kept)
	assert.InDelta(t, model.DefaultContactAreaCm2, *kept, 1e-9)
}
