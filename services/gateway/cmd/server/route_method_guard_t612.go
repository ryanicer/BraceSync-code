// Package main — T612 路由注册层的方法白名单校验
//
// 现网读数（修复前，2026-10-07 本卡实测）：设备以 POST 打只注册 GET 的校时端点，
// gin v1.10.0 的 Engine.HandleMethodNotAllowed 默认为 false ⇒ 方法不匹配不进 405 分支，
// 而是落到 noRoute 的纯文本 "404 page not found"。后果两格：
//  1. 状态码与「业务 404」（如 20404 设备未注册、裸 404 endpoint not available）同形，
//     设备侧无法分辨「路由没匹配上」还是「业务拒绝了我」；
//  2. 服务端 zerolog 零行 —— 黑洞里连「哪条路径、什么方法」都没有留下。
//
// 本文件只动路由注册层：不改验签、不改时间窗、不改既有业务 404 的报文形状。
package main

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// codeRouteMethodNotAllowed 设备域「方法不匹配」业务码，形状照 T402 甲-1「域号 2 + HTTP 三位 405」。
// 与 20404（设备未注册）、裸 404（端点不可用）都不同值，验收判据「不得与业务 404 混同」靠这一格。
const codeRouteMethodNotAllowed = 20405

// registerRouteMethodGuard 打开网关的方法不匹配分支，并给两个兜底各挂一行取证日志。
//
//	NoMethod —— 路径已注册、方法不在白名单：405 + 20405 + Allow 头 + 一行日志
//	NoRoute  —— 路径根本没注册：状态码与响应体维持原样（本卡不扩到 404 报文改造），
//	            只补上缺失的那一行日志，让「路由未匹配」在服务端留下证据
//
// Allow 头取自 gin 自己算出的那一串：它按路由树逐段匹配，参数化路径
// （/devices/:deviceId 一类）也能命中；自己拿 Engine.Routes() 的 fullPath 去等值比对会漏掉这一类。
func registerRouteMethodGuard(r *gin.Engine) {
	r.HandleMethodNotAllowed = true

	r.NoMethod(func(c *gin.Context) {
		allowed := allowedMethodsOf(c)
		requestID := requestIDOf(c)
		logTechnical(c, codeRouteMethodNotAllowed, http.StatusMethodNotAllowed,
			"route registered for other methods only: "+strings.Join(allowed, ","), requestID)
		c.AbortWithStatusJSON(http.StatusMethodNotAllowed, gin.H{
			"code":    codeRouteMethodNotAllowed,
			"message": userText(codeRouteMethodNotAllowed),
			"data":    gin.H{"allowed_methods": allowed},
			"trace":   errorTrace{ErrorCode: codeRouteMethodNotAllowed, RequestID: requestID},
		})
	})

	r.NoRoute(func(c *gin.Context) {
		logTechnical(c, http.StatusNotFound, http.StatusNotFound,
			"no route registered for this path", requestIDOf(c))
	})
}

// allowedMethodsOf 读 gin 在进 NoMethod 之前写入的 Allow 头，归一为排序后的方法白名单。
func allowedMethodsOf(c *gin.Context) []string {
	raw := c.Writer.Header().Get("Allow")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	methods := make([]string, 0, len(parts))
	for _, m := range parts {
		if trimmed := strings.TrimSpace(m); trimmed != "" {
			methods = append(methods, trimmed)
		}
	}
	sort.Strings(methods)
	return methods
}
