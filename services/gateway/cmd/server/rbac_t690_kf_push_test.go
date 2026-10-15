// T690 微信客服推送入口（/api/v1/wechat/kf-push）免 JWT 白名单契约。
//
// 该端点由微信服务器外部调用（GET=URL 验签挑战，POST=user_enter_tempsession 事件），
// 无 JWT 可言；信任锚下沉到 user-service handler 内的微信验签（WX_KF_MSG_TOKEN fail-closed）。
// 白名单只覆盖这两种方法——其余方法仍须 JWT（默认拒绝不回退）。
package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestT690_KFPush_AuthWhitelisted_Methods(t *testing.T) {
	const path = "/api/v1/wechat/kf-push"

	assert.True(t, authWhitelisted(http.MethodGet, path), "GET = 微信 URL 验签挑战必须免 JWT")
	assert.True(t, authWhitelisted(http.MethodPost, path), "POST = 微信进入事件推送必须免 JWT")

	assert.False(t, authWhitelisted(http.MethodPut, path), "PUT 不在推送契约内，仍须 JWT")
	assert.False(t, authWhitelisted(http.MethodDelete, path), "DELETE 不在推送契约内，仍须 JWT")

	// 正对照：既有登录白名单不受影响
	assert.True(t, authWhitelisted(http.MethodPost, "/api/v1/patient/wx-login"))
	// 负对照：普通业务端点不因本次改动误入白名单
	assert.False(t, authWhitelisted(http.MethodPost, "/api/v1/feedbacks"))
	assert.False(t, authWhitelisted(http.MethodGet, "/api/v1/patient/profile"))
}
