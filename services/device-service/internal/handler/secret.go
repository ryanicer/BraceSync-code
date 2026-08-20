// Package handler — internal 验签密钥端点（T032：gateway 设备验签密钥来源）
//
// GET /internal/devices/:deviceId/secret：服务间白名单直连（架构 §5.2），
// 不经网关、不对前端暴露；nginx 仅对外暴露 /api/*，/internal/* 不出内网。
package handler

import (
	"github.com/gin-gonic/gin"
)

// getSecret 返回设备 HMAC 验签密钥明文（gateway 验签专用）。
// 未注册设备 → 20404（ErrNotFound 映射），响应体仍为统一结构。
func (h *Handler) getSecret(c *gin.Context) {
	secret, appErr := h.svc.GetDeviceSecret(c.Request.Context(), c.Param("deviceId"))
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, gin.H{"secret": secret})
}
