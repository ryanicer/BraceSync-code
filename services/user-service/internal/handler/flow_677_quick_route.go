// 677 快处路由一问（服务间端点，不挂网关、不鉴 JWT；与 T653 auto-start / T668 process-guard 同构）。
//
// 背景（677 卡面实锚①）：告警列表旧快处 POST /alerts/:id/process 只 UPDATE alerts 四列，
// 完全不走路程。alert-service 在落库前问本端点「该告警应直改还是转流程」，判定全在
// 绑定权威侧（T653 归属地），alert-service 只 obey：
//   - 类型不在可绑定集（如存量 pressure_fluctuation）或无绑定行 → route=direct
//     （677 最小闭环口径：无绑定类型的快处直改保持现状，完全收口留 T672）；
//   - 有绑定 → 复用 auto-start 同一段 ensureFlowInstanceForBinding 确保实例存在
//     （绑定后建的告警在入库时已自动建过，此处幂等回既有 id）→ route=flow + instanceId；
//   - alreadyProcessed=告警已是 processed，调用方对这类走原幂等直通，不回 409。
package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// quickRouteRequest 服务间请求体（alert-service 只回传 alertId，类型判定在本端）。
type quickRouteRequest struct {
	AlertID int64 `json:"alertId"`
}

// quickRouteResponse data：route=direct|flow；flow 时带 instanceId；
// alreadyProcessed=true 表示告警已 processed（调用方走幂等直通）。
type quickRouteResponse struct {
	Route            string  `json:"route"`
	InstanceID       *string `json:"instanceId,omitempty"`
	AlreadyProcessed bool    `json:"alreadyProcessed"`
}

// quickRoute POST /internal/flow/quick-route
func (h *Handler) quickRoute(c *gin.Context) {
	var req quickRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if req.AlertID <= 0 {
		fail(c, model.ErrInvalidParam("alertId must be a positive integer"))
		return
	}

	ctx := c.Request.Context()
	typeName, status, exists, err := h.store.AlertRouteInfo(ctx, req.AlertID)
	if err != nil {
		fail(c, model.ErrInternal("load alert route info failed"))
		return
	}
	if !exists {
		// alert-service 落库前必先自证告警存在；此处 400 只兜时序竞态（告警被并发清理）。
		fail(c, model.ErrInvalidParam("alert not found"))
		return
	}
	alreadyProcessed := status == "processed"

	// 可绑定集外（存量 pressure_fluctuation 等）= 永远进不了绑定表 → 直改现状。
	if !model.ValidFlowBindAlertType(typeName) {
		ok(c, quickRouteResponse{Route: "direct", AlreadyProcessed: alreadyProcessed})
		return
	}
	binding, err := h.store.GetFlowTypeBinding(ctx, typeName)
	if err != nil {
		fail(c, model.ErrInternal("load binding failed"))
		return
	}
	if binding == nil {
		ok(c, quickRouteResponse{Route: "direct", AlreadyProcessed: alreadyProcessed})
		return
	}

	id, _, err := h.ensureFlowInstanceForBinding(ctx, binding, req.AlertID)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrFlowTemplateNotFound):
			// 绑定指向的模板被删 = 数据不一致，与 auto-start 同口径按未绑定降级（直改现状）。
			ok(c, quickRouteResponse{Route: "direct", AlreadyProcessed: alreadyProcessed})
		case errors.Is(err, repo.ErrFlowAlertNotFound):
			fail(c, model.ErrInvalidParam("alert not found"))
		default:
			fail(c, model.ErrInternal("ensure flow instance failed"))
		}
		return
	}
	ok(c, quickRouteResponse{Route: "flow", InstanceID: &id, AlreadyProcessed: alreadyProcessed})
}
