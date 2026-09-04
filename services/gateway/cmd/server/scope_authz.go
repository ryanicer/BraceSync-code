package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// scopeBindPrefix 绑定态 JWT sub 前缀（openid 开头 → scope=bind）
const scopeBindPrefix = "openid_"

// bindPhonePath 绑定手机号端点（scope=bind 唯一放行路径）
const bindPhonePath = "/api/v1/patient/bind-phone"

// scopeAuthz T085：scope 授权中间件。
// 依赖 jwtAuth 注入的 X-User-Id（=JWT sub）：
//   - sub 前缀 "openid_" → scope=bind：仅允许 POST /api/v1/patient/bind-phone，其余 403
//   - 否则 → scope=full：禁止 POST /api/v1/patient/bind-phone，其余放行
//
// 白名单路径（登录类）不进入本中间件（jwtAuth 已放行且未设置 X-User-Id）。
// 通过后注入 X-Scope 头供后端识别。
func scopeAuthz() gin.HandlerFunc {
	return func(c *gin.Context) {
		sub := c.GetHeader("X-User-Id")
		// 白名单：无 X-User-Id（登录类端点 jwtAuth 放行后未注入身份）→ 跳过 scope 校验
		if sub == "" {
			c.Next()
			return
		}

		scope := "full"
		if strings.HasPrefix(sub, scopeBindPrefix) {
			scope = "bind"
		}
		c.Request.Header.Set("X-Scope", scope)

		path := c.Request.URL.Path
		switch {
		case scope == "bind" && !(c.Request.Method == http.MethodPost && path == bindPhonePath):
			abortJSON(c, http.StatusForbidden, 40301, "bind scope only allows /patient/bind-phone")
			return
		case scope == "full" && c.Request.Method == http.MethodPost && path == bindPhonePath:
			abortJSON(c, http.StatusForbidden, 40301, "full scope cannot call /patient/bind-phone")
			return
		}
		c.Next()
	}
}
