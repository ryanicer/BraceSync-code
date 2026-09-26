// Package main — T407 新增读端点 GET /api/v1/devices/:deviceId/baseline 的网关侧验证。
//
// 为什么需要本文件：check-routes.sh 只比对 user-service 的路由表，device-service 的新路由
// 没有任何 CI 门禁会发现「服务已实现、网关漏转发」或「网关已转发、RBAC 漏登记」。
// 后一种尤其阴：T260 之后未登记路径一律默认 403，症状是「四角色全都打不开基线」，
// 而 device-service 的单测仍然全绿。
//
// 口径：与 POST /baselines 及同域设备读端点一致，落在 staffOnlyPatterns（admin/doctor/cs/tech），
// 不对患者开放——患者端无基线读需求，设备详情族本来就是 staff-only。
package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t407BaselinePath = "/api/v1/devices/DEV-GW-T407/baseline"

// ① 登记面：命中 staffOnly 矩阵（不登记即 T260 默认 403）；且既不是 admin-only，
// 也没被误放进 publicPatterns（那等于把校准偏移开放给患者令牌）。
func TestRBAC_T407_DeviceBaselinePatternRegistered(t *testing.T) {
	assert.True(t, matchStaffOnlyPattern(http.MethodGet, t407BaselinePath),
		"GET %s 必须登记进 staffOnlyPatterns，否则 roleAuthz 默认 403", t407BaselinePath)
	assert.False(t, matchRBACPattern(http.MethodGet, t407BaselinePath),
		"基线读不是 admin 专属（医生/客服/技师都要能复算）")
	assert.False(t, matchPublicPattern(http.MethodGet, t407BaselinePath),
		"不得对患者开放（口径同 GET /devices/:deviceId）")
}

// ② 反代 + 角色判定：staff 四角色真实打到 device-service 且路径原样保留；患者 403 不触达后端。
func TestRBAC_T407_DeviceBaselineStaffPassPatientDenied(t *testing.T) {
	device, gotDevice := captureBackend(t)
	other, gotOther := captureBackend(t)
	gw := startFullGateway(t, other.URL, device.URL, other.URL, other.URL, other.URL, testJWTSecretMain)

	for _, role := range []string{roleAdmin, roleDoctor, roleCS, roleTech} {
		code, body := httpDoFull(t, http.MethodGet, gw.URL+t407BaselinePath, "", rbacToken(t, role))
		require.Equal(t, http.StatusOK, code, "role=%s 应放行，body=%s", role, body)
	}
	require.Len(t, *gotDevice, 4, "4 个 staff 角色均须打到 device-service（代理未注册则为 0）")
	for _, got := range *gotDevice {
		assert.Contains(t, got, "GET "+t407BaselinePath+" ",
			"转发路径必须原样保留（deviceId 段被吞即读到别的设备），实收 %q", got)
	}
	assert.Empty(t, *gotOther, "设备域读端点不得落到其它服务")

	before := len(*gotDevice)
	code, body := httpDoFull(t, http.MethodGet, gw.URL+t407BaselinePath, "", rbacToken(t, rolePatient))
	assert.Equal(t, http.StatusForbidden, code, "患者不得读设备校准偏移，body=%s", body)
	assert.Len(t, *gotDevice, before, "患者请求不得触达后端")
}

// ③ 反证「代理未注册」这一格是可见的：同前缀但未登记进 deviceManageRoutes 的兄弟路径
// 一律 404（gin 路由层即拒，见 TestRBAC_T260_DefaultDeny_UnregisteredPattern 的口径）。
// 没有这条对照，②里「代理未注册则为 0」可能与「网关本就不转发这一族」混为一谈。
func TestRBAC_T407_UnregisteredSiblingRouteIs404Not403(t *testing.T) {
	const unregistered = "/api/v1/devices/DEV-GW-T407/baseline-history"
	assert.False(t, matchStaffOnlyPattern(http.MethodGet, unregistered))

	device, gotDevice := captureBackend(t)
	other, _ := captureBackend(t)
	gw := startFullGateway(t, other.URL, device.URL, other.URL, other.URL, other.URL, testJWTSecretMain)

	code, body := httpDoFull(t, http.MethodGet, gw.URL+unregistered, "", rbacToken(t, roleAdmin))
	assert.Equal(t, http.StatusNotFound, code, "未注册路由应 404，body=%s", body)
	assert.Empty(t, *gotDevice, "404 不得穿透到后端")
}
