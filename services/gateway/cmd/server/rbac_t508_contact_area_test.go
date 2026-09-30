// Package main — T508 新增写端点 PUT /api/v1/devices/:deviceId/contact-area 的网关侧验证。
//
// 为什么需要本文件（口径逐字承自 T407）：check-routes.sh 只比对 user-service 的路由表，
// device-service 的新路由没有任何 CI 门禁会发现「服务已实现、网关漏转发」或
// 「网关已转发、RBAC 漏登记」。后一种尤其阴：T260 之后未登记路径一律默认 403，
// 症状是「四角色全都配不了面积」，而 device-service 的单测仍然全绿。
//
// 登记面口径（为什么落 staffOnlyPatterns 而不是 techAdminOnlyPatterns）：
// 本端点与 T387 那族八条写端点共用同一把服务层门禁（assertDeviceWriteRole，技师+管理员 allow-list）。
// 医护/客服打到本族任何一条，拿到的都应是设备域那一个 20403 + 同一句中文短句；
// 若这一条改在网关 403，同族越权在网关侧与服务侧就分裂成两种报文。
// 整族是否上收到 techAdminOnly 是 T387 N-c 已登记的待裁项，不在本卡单方面变更。
package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	t508ContactAreaPath     = "/api/v1/devices/DEV-GW-T508/contact-area"
	t508UnregisteredSibling = "/api/v1/devices/DEV-GW-T508/contact-area-history"
)

// ① 登记面：命中 staffOnly 矩阵（不登记即 T260 默认 403）；不是 admin-only；
// 也不得落进 publicPatterns（那等于把设备配置写面开放给患者令牌）。
func TestRBAC_T508_ContactAreaPatternRegistered(t *testing.T) {
	assert.True(t, matchStaffOnlyPattern(http.MethodPut, t508ContactAreaPath),
		"PUT %s 必须登记进 staffOnlyPatterns，否则 roleAuthz 默认 403", t508ContactAreaPath)
	assert.False(t, matchRBACPattern(http.MethodPut, t508ContactAreaPath),
		"面积配置不是 admin 专属（技师侧同样是正当调用方）")
	assert.False(t, matchPublicPattern(http.MethodPut, t508ContactAreaPath),
		"不得对患者开放（口径同 POST /devices/:deviceId/wifi）")
}

// ② 反代 + 角色判定：staff 四角色的请求真实打到 device-service 且路径原样保留
// （收口本身由 device-service 判定，本层只挡患者）；患者 403 不触达后端。
func TestRBAC_T508_ContactAreaStaffPassPatientDenied(t *testing.T) {
	device, gotDevice := captureBackend(t)
	other, gotOther := captureBackend(t)
	gw := startFullGateway(t, other.URL, device.URL, other.URL, other.URL, other.URL, testJWTSecretMain)

	for _, role := range []string{roleAdmin, roleDoctor, roleCS, roleTech} {
		code, body := httpDoFull(t, http.MethodPut, gw.URL+t508ContactAreaPath,
			`{"contactAreaCm2":0.8}`, rbacToken(t, role))
		require.Equal(t, http.StatusOK, code, "role=%s 应放行到后端，body=%s", role, body)
	}
	require.Len(t, *gotDevice, 4, "4 个 staff 角色均须打到 device-service（代理未注册则为 0）")
	for _, got := range *gotDevice {
		assert.Contains(t, got, "PUT "+t508ContactAreaPath+" ",
			"转发路径必须原样保留（deviceId 段被吞即改到别的设备），实收 %q", got)
	}
	assert.Empty(t, *gotOther, "设备域写端点不得落到其它服务")

	before := len(*gotDevice)
	code, body := httpDoFull(t, http.MethodPut, gw.URL+t508ContactAreaPath,
		`{"contactAreaCm2":0.8}`, rbacToken(t, rolePatient))
	assert.Equal(t, http.StatusForbidden, code, "患者不得配置设备受压面积，body=%s", body)
	assert.Len(t, *gotDevice, before, "患者请求不得触达后端")
}

// ③ 反证「代理未注册」这一格是可见的：同前缀但未登记进 deviceManageRoutes 的兄弟路径
// 一律 404。没有这条对照，②里的「代理未注册则为 0」可能与「网关本就不转发这一族」混为一谈。
func TestRBAC_T508_UnregisteredSiblingRouteIs404Not403(t *testing.T) {
	assert.False(t, matchStaffOnlyPattern(http.MethodPut, t508UnregisteredSibling))

	device, gotDevice := captureBackend(t)
	other, _ := captureBackend(t)
	gw := startFullGateway(t, other.URL, device.URL, other.URL, other.URL, other.URL, testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodPut, gw.URL+t508UnregisteredSibling,
		`{"contactAreaCm2":0.8}`, rbacToken(t, roleAdmin))
	assert.Equal(t, http.StatusNotFound, code, "未注册路由应 404，body=%s", body)
	assert.Empty(t, *gotDevice, "404 不得穿透到后端")
}
