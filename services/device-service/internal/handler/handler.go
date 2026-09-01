// Package handler device-service HTTP 接入层（Gin）
//
// 路由：
//
//	POST /api/v1/devices                          设备注册（幂等）
//	GET  /api/v1/devices                          设备分页列表（T030：patientName join）
//	GET  /api/v1/devices/:deviceId                设备详情
//	GET  /api/v1/devices/:deviceId/bindings       绑定历史（追溯）
//	POST /api/v1/devices/:deviceId/bind           绑定（互斥：已被他患者绑定 → 409）
//	POST /api/v1/devices/:deviceId/rebind         换绑（旧绑定历史可追溯）
//	POST /api/v1/devices/:deviceId/unbind         解绑（幂等）
//	POST /api/v1/devices/:deviceId/wifi           WiFi 配置状态（wifi_ssid 维护）
//	POST /api/v1/devices/:deviceId/provision-key  配网密钥派生（T067，HKDF-SHA256 16B→32hex）
//	POST /api/v1/install-records                  新建安装记录（技师安装流程）
//	GET  /api/v1/install-records                  安装记录分页列表（T030：姓名 join）
//	POST /api/v1/baselines                        校准基线落库（契约 saveBaseline）
//	POST /internal/devices/:deviceId/report       上报/补传状态校正（服务间，不经网关）
//	GET  /internal/devices/:deviceId/secret       验签密钥查询（T032，仅 gateway 验签用，不经网关）
//	GET  /healthz                                 存活探针
//
// 统一响应体（架构 §3.5）：{ "code": 0, "message": "success", "data": {...} }
// 鉴权归 gateway（JWT / 设备验签）；操作人取网关注入的 X-User-Id（§5.2 内部信任链）。
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
	"github.com/bracesync/bracesync/services/device-service/internal/service"
)

// headerUserID gateway 鉴权通过后注入的操作人身份头（架构 §5.2）
const headerUserID = "X-User-Id"

// Handler HTTP 处理器
type Handler struct {
	svc  *service.DeviceService
	list repo.ListStore // T030 管理端列表查询（SetListStore 注入；nil 时列表端点 500）
}

// New 创建 Handler
func New(svc *service.DeviceService) *Handler { return &Handler{svc: svc} }

// Router 组装路由（可测试）
func (h *Handler) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/v1")
	{
		v1.POST("/devices", h.register)
		v1.GET("/devices/:deviceId", h.getDevice)
		v1.GET("/devices/:deviceId/bindings", h.listBindings)
		v1.POST("/devices/:deviceId/bind", h.bind)
		v1.POST("/devices/:deviceId/rebind", h.rebind)
		v1.POST("/devices/:deviceId/unbind", h.unbind)
		v1.POST("/devices/:deviceId/wifi", h.setWifi)
		v1.POST("/devices/:deviceId/provision-key", h.provisionKey) // T067
		v1.POST("/install-records", h.createInstall)
		v1.POST("/baselines", h.saveBaseline)
		h.registerListRoutes(v1) // T030：GET /devices 列表 + GET /install-records 列表
	}

	internal := r.Group("/internal")
	{
		internal.POST("/devices/:deviceId/report", h.report)
		internal.GET("/devices/:deviceId/secret", h.getSecret) // T032 gateway 设备验签密钥来源
	}
	return r
}

// jsonResp 统一响应体（命名避开 Ella 预置测试 handler_test.go 中的 apiResponse 定义）
type jsonResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, jsonResp{Code: model.CodeOK, Message: "success", Data: data})
}

func fail(c *gin.Context, appErr *model.AppError) {
	c.JSON(appErr.HTTPStatus, jsonResp{Code: appErr.Code, Message: appErr.Message, Data: nil})
}

// operatorID 操作人：请求体显式传入优先，否则取网关注入的 X-User-Id
func operatorID(c *gin.Context, fromBody string) string {
	if fromBody != "" {
		return fromBody
	}
	return c.GetHeader(headerUserID)
}

// ─────────────────────────────────────────────────────────────
// 请求 DTO
// ─────────────────────────────────────────────────────────────

type registerRequest struct {
	DeviceID string `json:"deviceId"`
	Model    string `json:"model"`
}

type bindRequest struct {
	PatientID  string `json:"patientId"`
	OperatorID string `json:"operatorId"`
}

type unbindRequest struct {
	OperatorID string `json:"operatorId"`
}

type wifiRequest struct {
	Ssid string `json:"ssid"`
}

type installRequest struct {
	DeviceID     string `json:"deviceId"`
	PatientID    string `json:"patientId"`
	TechID       string `json:"techId"`
	Notes        string `json:"notes"`
	SignatureURL string `json:"signatureUrl"`
}

type baselineRequest struct {
	InstallID    string    `json:"installId"` // 契约 saveBaseline：string
	OffsetValues []float32 `json:"offsetValues"`
	Notes        string    `json:"notes"`
	SignatureURL string    `json:"signatureUrl"`
	CalibratorID string    `json:"calibratorId"` // 缺省取 X-User-Id
}

type reportRequest struct {
	Timestamp int64 `json:"timestamp"` // Unix 秒；0=服务器当前时刻
	FaultCode int   `json:"fault_code"`
}

// ─────────────────────────────────────────────────────────────
// 处理器
// ─────────────────────────────────────────────────────────────

// register 设备注册（契约 registerDevice；重复注册幂等返回既有记录）
func (h *Handler) register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	dev, _, appErr := h.svc.Register(c.Request.Context(), req.DeviceID, req.Model)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, dev.ToDTO())
}

// getDevice 设备详情
func (h *Handler) getDevice(c *gin.Context) {
	dev, appErr := h.svc.GetDevice(c.Request.Context(), c.Param("deviceId"))
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, dev.ToDTO())
}

// listBindings 绑定历史（验收：历史可追溯）
func (h *Handler) listBindings(c *gin.Context) {
	bindings, appErr := h.svc.ListBindings(c.Request.Context(), c.Param("deviceId"))
	if appErr != nil {
		fail(c, appErr)
		return
	}
	list := make([]model.BindingDTO, 0, len(bindings))
	for i := range bindings {
		list = append(list, bindings[i].ToDTO())
	}
	ok(c, gin.H{"list": list})
}

// bind 绑定（契约 bindDevice → ApiResponse<null>；互斥：已被他患者绑定 → 409）
func (h *Handler) bind(c *gin.Context) {
	var req bindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	_, appErr := h.svc.Bind(c.Request.Context(), c.Param("deviceId"), req.PatientID, operatorID(c, req.OperatorID))
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, nil)
}

// rebind 换绑（旧绑定写 unbind_at+reason=rebind+operator，历史可追溯）
func (h *Handler) rebind(c *gin.Context) {
	var req bindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	_, appErr := h.svc.Rebind(c.Request.Context(), c.Param("deviceId"), req.PatientID, operatorID(c, req.OperatorID))
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, nil)
}

// unbind 解绑（幂等）
func (h *Handler) unbind(c *gin.Context) {
	var req unbindRequest
	// body 可空（幂等解绑可无 body）
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, model.ErrInvalidParam("invalid request body: %v", err))
			return
		}
	}
	_, appErr := h.svc.Unbind(c.Request.Context(), c.Param("deviceId"), operatorID(c, req.OperatorID))
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, nil)
}

// setWifi 配网状态：devices.wifi_ssid 维护（架构 §2.3）
func (h *Handler) setWifi(c *gin.Context) {
	var req wifiRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if appErr := h.svc.SetWifiSSID(c.Request.Context(), c.Param("deviceId"), req.Ssid); appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, nil)
}

// provisionKey 配网密钥派生（T067，硬件清单 §2.1 HKDF-SHA256 16B→32hex）。
// 联调期由 gateway 裸组注册（不强制 JWT）；未注册 device → 20404。
// TODO(T068)：鉴权收紧 + expires_in_sec 真实 enforcement。
func (h *Handler) provisionKey(c *gin.Context) {
	keyHex, appErr := h.svc.GetProvisionKey(c.Request.Context(), c.Param("deviceId"))
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, gin.H{"provision_key_hex": keyHex, "expires_in_sec": service.ProvisionKeyExpiresInSec})
}

// createInstall 新建安装记录（技师安装流程 bind → matrix → save-baseline → complete）
func (h *Handler) createInstall(c *gin.Context) {
	var req installRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	in := &service.CreateInstallRequest{
		DeviceID:  req.DeviceID,
		PatientID: req.PatientID,
		TechID:    req.TechID,
	}
	if req.Notes != "" {
		in.Notes = &req.Notes
	}
	if req.SignatureURL != "" {
		in.SignatureURL = &req.SignatureURL
	}
	rec, appErr := h.svc.CreateInstall(c.Request.Context(), in)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, gin.H{"installId": strconv.FormatInt(rec.InstallID, 10)})
}

// saveBaseline 校准基线落库（契约 saveBaseline → ApiResponse<null>）
func (h *Handler) saveBaseline(c *gin.Context) {
	var req baselineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	installID, err := strconv.ParseInt(req.InstallID, 10, 64)
	if err != nil || installID <= 0 {
		fail(c, model.ErrInvalidParam("invalid installId %q", req.InstallID))
		return
	}
	calibrator := operatorID(c, req.CalibratorID)
	if _, appErr := h.svc.SaveBaseline(c.Request.Context(), installID, req.OffsetValues, calibrator); appErr != nil {
		fail(c, appErr)
		return
	}
	// notes/signatureUrl 回填安装记录（基线之外的一次性元数据）
	var notes, sigURL *string
	if req.Notes != "" {
		notes = &req.Notes
	}
	if req.SignatureURL != "" {
		sigURL = &req.SignatureURL
	}
	if notes != nil || sigURL != nil {
		if err := h.svc.UpdateInstallMeta(c.Request.Context(), installID, notes, sigURL); err != nil {
			fail(c, err)
			return
		}
	}
	ok(c, nil)
}

// report 上报/补传状态校正（internal：data-service 联动或运维手工；不经网关）
func (h *Handler) report(c *gin.Context) {
	var req reportRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, model.ErrInvalidParam("invalid request body: %v", err))
			return
		}
	}
	var ts time.Time // 零值 → service 用服务器当前时刻
	if req.Timestamp > 0 {
		ts = time.Unix(req.Timestamp, 0)
	}
	if appErr := h.svc.Touch(c.Request.Context(), c.Param("deviceId"), ts, req.FaultCode); appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, nil)
}
