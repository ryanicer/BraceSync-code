// T653 告警类型 ↔ 流程模板绑定 handler（PRD V3.44 §7D.6 Tab4 绑定区 + R8 自动建实例）
//
// 路由（网关 RBAC 见 services/gateway/cmd/server/{proxy_admin.go,rbac.go}，本文件另做 handler 层兜底）：
//
//	GET  /api/v1/admin/flow/type-bindings   四类型绑定视图（adminOnly；含未绑定 null 行）
//	PUT  /api/v1/admin/flow/type-bindings   逐项 upsert/解绑（adminOnly；审计 target=flow_binding/<type>）
//	POST /internal/flow/auto-start          服务间端点（不挂网关、不鉴 JWT；alert-service 告警入库后调）
//
// 语义：无绑定不自动建实例（告警照常通知）；一告警一实例（409 幂等）；绑定变更不追溯在途。
package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// flowBindingToDTO 现存行 → DTO（已绑定形态）。
func flowBindingToDTO(r *repo.FlowTypeBindingRow) model.FlowTypeBindingDTO {
	dto := model.FlowTypeBindingDTO{
		AlertType:     r.AlertType,
		AlertTypeName: model.FlowBindAlertTypeNames[r.AlertType],
		UpdatedBy:     &r.UpdatedBy,
		UpdatedByName: r.UpdatedByName,
		UpdatedAt:     flowTimeStrPtr(&r.UpdatedAt),
		TemplateID:    &r.TemplateID,
		TemplateName:  r.TemplateName,
	}
	return dto
}

// flowBindingUnboundDTO 未绑定占位行（templateId 等指针全 nil，前端据此黄标）。
func flowBindingUnboundDTO(alertType string) model.FlowTypeBindingDTO {
	return model.FlowTypeBindingDTO{
		AlertType:     alertType,
		AlertTypeName: model.FlowBindAlertTypeNames[alertType],
	}
}

// getFlowTypeBindings GET /api/v1/admin/flow/type-bindings
// 固定回规范四类型（PRD §7D.6），未绑定类型补 null 行，前端无需自行补洞。
func (h *Handler) getFlowTypeBindings(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can read flow type bindings"))
		return
	}
	rows, err := h.store.ListFlowTypeBindings(c.Request.Context())
	if err != nil {
		fail(c, model.ErrInternal("list flow type bindings failed"))
		return
	}
	byType := make(map[string]*repo.FlowTypeBindingRow, len(rows))
	for i := range rows {
		byType[rows[i].AlertType] = &rows[i]
	}
	list := make([]model.FlowTypeBindingDTO, 0, len(model.FlowBindAlertTypes))
	for _, t := range model.FlowBindAlertTypes {
		if r, ok := byType[t]; ok {
			list = append(list, flowBindingToDTO(r))
		} else {
			list = append(list, flowBindingUnboundDTO(t))
		}
	}
	ok(c, gin.H{"list": list})
}

// saveFlowTypeBindings PUT /api/v1/admin/flow/type-bindings
// 部分覆盖：body 只含要变更的类型项；templateId 空串 = 解绑。
// 每项独立校验后单事务落库；仅对「确有变化」的项写审计（防重复保存刷审计）。
func (h *Handler) saveFlowTypeBindings(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can manage flow type bindings"))
		return
	}
	var req model.UpdateFlowTypeBindingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if len(req.Bindings) == 0 {
		fail(c, model.ErrInvalidParam("bindings must contain at least one item"))
		return
	}

	// 去重 + 词表/量程校验（同类型重复提交直接 400，不做静默后者覆盖）
	seen := map[string]bool{}
	sets := make([]repo.FlowTypeBindingSet, 0, len(req.Bindings))
	for i, b := range req.Bindings {
		t := strings.TrimSpace(b.AlertType)
		if !model.ValidFlowBindAlertType(t) {
			fail(c, model.ErrInvalidParam("bindings[%d].alertType %q is not one of the four bindable types", i, t))
			return
		}
		if seen[t] {
			fail(c, model.ErrInvalidParam("bindings[%d].alertType %q is duplicated", i, t))
			return
		}
		seen[t] = true
		tid := strings.TrimSpace(b.TemplateID)
		if len(tid) > 64 {
			fail(c, model.ErrInvalidParam("bindings[%d].templateId must be at most 64 chars", i))
			return
		}
		sets = append(sets, repo.FlowTypeBindingSet{AlertType: t, TemplateID: tid})
	}

	ctx := c.Request.Context()
	oldRows, err := h.store.ListFlowTypeBindings(ctx)
	if err != nil {
		fail(c, model.ErrInternal("load flow type bindings failed"))
		return
	}
	oldByType := map[string]*repo.FlowTypeBindingRow{}
	for i := range oldRows {
		oldByType[oldRows[i].AlertType] = &oldRows[i]
	}

	// 模板存在性（绑定项）与变更判定：模板名用于审计文案
	op := operatorID(c, "ops")
	changed := make([]repo.FlowTypeBindingSet, 0, len(sets))
	type auditDesc struct {
		t    string
		bind bool
		name string
		tid  string
	}
	descs := make([]auditDesc, 0, len(sets))
	for _, s := range sets {
		old := oldByType[s.AlertType]
		if s.TemplateID == "" {
			if old == nil {
				continue // 本来就没绑定，无变化
			}
		} else if old != nil && old.TemplateID == s.TemplateID {
			continue // 绑的还是同一模板，无变化
		}
		var tplName string
		if s.TemplateID != "" {
			tpl, err := h.store.GetFlowTemplate(ctx, s.TemplateID)
			if err != nil {
				if errors.Is(err, repo.ErrFlowTemplateNotFound) {
					fail(c, model.ErrInvalidParam("template not found: %s", s.TemplateID))
				} else {
					fail(c, model.ErrInternal("get flow template failed"))
				}
				return
			}
			tplName = tpl.Name
		}
		s.UpdatedBy = op
		changed = append(changed, s)
		descs = append(descs, auditDesc{t: s.AlertType, bind: s.TemplateID != "", name: tplName, tid: s.TemplateID})
	}
	if len(changed) == 0 {
		// 无变化：直接回当前视图，不写库不写审计
		h.respondBindings(c)
		return
	}
	if err := h.store.ReplaceFlowTypeBindings(ctx, changed); err != nil {
		if errors.Is(err, repo.ErrFlowTemplateNotFound) {
			fail(c, model.ErrInvalidParam("flow template not found"))
		} else {
			fail(c, model.ErrInternal("save flow type bindings failed"))
		}
		return
	}
	for _, d := range descs {
		desc := ""
		if d.bind {
			desc = fmt.Sprintf("告警类型「%s」(%s) 绑定流程模板「%s」(%s)；变更对在途实例不追溯",
				model.FlowBindAlertTypeNames[d.t], d.t, d.name, d.tid)
		} else {
			desc = fmt.Sprintf("告警类型「%s」(%s) 解绑流程模板；此后新告警不自动建实例，在途实例按原模板走完",
				model.FlowBindAlertTypeNames[d.t], d.t)
		}
		h.audit(c, repo.AuditInput{
			Action:      auditActionDataModify,
			TargetType:  "flow_binding",
			TargetID:    d.t,
			Description: desc,
		})
	}
	h.respondBindings(c)
}

// respondBindings 落库后回最新四类型视图。
func (h *Handler) respondBindings(c *gin.Context) {
	rows, err := h.store.ListFlowTypeBindings(c.Request.Context())
	if err != nil {
		fail(c, model.ErrInternal("list flow type bindings failed"))
		return
	}
	byType := make(map[string]*repo.FlowTypeBindingRow, len(rows))
	for i := range rows {
		byType[rows[i].AlertType] = &rows[i]
	}
	list := make([]model.FlowTypeBindingDTO, 0, len(model.FlowBindAlertTypes))
	for _, t := range model.FlowBindAlertTypes {
		if r, ok := byType[t]; ok {
			list = append(list, flowBindingToDTO(r))
		} else {
			list = append(list, flowBindingUnboundDTO(t))
		}
	}
	ok(c, gin.H{"list": list})
}

// autoStartFlow POST /internal/flow/auto-start（服务间，不挂网关、不鉴 JWT）
//
// alert-service 在告警创建成功后调用：按类型查绑定 → 取模板建实例。
// 无绑定 → 200 started=false,reason=unbound；已有实例（409）→ 200 started=false,reason=exists；
// 成功 → 200 started=true。入参非法/告警不存在 → 4xx（调用方仅记日志，不阻塞告警与通知）。
func (h *Handler) autoStartFlow(c *gin.Context) {
	var req model.AutoStartFlowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResp{Code: model.CodeInvalidParam, Message: "invalid request body", Data: nil})
		return
	}
	alertID, err := strconv.ParseInt(strings.TrimSpace(req.AlertID), 10, 64)
	if err != nil || alertID <= 0 {
		c.JSON(http.StatusBadRequest, jsonResp{Code: model.CodeInvalidParam, Message: "invalid alertId", Data: nil})
		return
	}
	alertType := strings.TrimSpace(req.AlertType)
	if !model.ValidFlowBindAlertType(alertType) {
		c.JSON(http.StatusBadRequest, jsonResp{Code: model.CodeInvalidParam, Message: "invalid alertType", Data: nil})
		return
	}

	ctx := c.Request.Context()
	binding, err := h.store.GetFlowTypeBinding(ctx, alertType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResp{Code: model.CodeInternal, Message: "load binding failed", Data: nil})
		return
	}
	if binding == nil {
		c.JSON(http.StatusOK, jsonResp{Code: model.CodeOK, Message: "success",
			Data: model.AutoStartFlowResponse{Started: false, Reason: "unbound"}})
		return
	}

	tpl, err := h.store.GetFlowTemplate(ctx, binding.TemplateID)
	if err != nil {
		// 绑定指向的模板被删（FK 正常应拦住；此处为双保险）= 数据不一致，按未绑定降级
		if errors.Is(err, repo.ErrFlowTemplateNotFound) {
			c.JSON(http.StatusOK, jsonResp{Code: model.CodeOK, Message: "success",
				Data: model.AutoStartFlowResponse{Started: false, Reason: "unbound"}})
		} else {
			c.JSON(http.StatusInternalServerError, jsonResp{Code: model.CodeInternal, Message: "get template failed", Data: nil})
		}
		return
	}
	g, appErr := parseFlowGraph(tpl.Nodes, tpl.Edges)
	if appErr != nil {
		c.JSON(http.StatusInternalServerError, jsonResp{Code: model.CodeInternal, Message: "invalid template graph", Data: nil})
		return
	}
	entries := make([]string, 0, len(g.nodeIDs))
	for _, id := range g.nodeIDs {
		if g.entryNodeID[id] {
			entries = append(entries, id)
		}
	}
	row, err := h.store.CreateFlowInstance(ctx, binding.TemplateID, alertID, g.nodeIDs, entries)
	if err != nil {
		var exists *repo.ErrFlowInstanceExists
		switch {
		case errors.As(err, &exists):
			id := exists.Existing.InstanceID
			c.JSON(http.StatusOK, jsonResp{Code: model.CodeOK, Message: "success",
				Data: model.AutoStartFlowResponse{Started: false, Reason: "exists", InstanceID: &id}})
		case errors.Is(err, repo.ErrFlowAlertNotFound):
			c.JSON(http.StatusBadRequest, jsonResp{Code: model.CodeInvalidParam, Message: "alert not found", Data: nil})
		default:
			c.JSON(http.StatusInternalServerError, jsonResp{Code: model.CodeInternal, Message: "create flow instance failed", Data: nil})
		}
		return
	}
	id := row.InstanceID
	c.JSON(http.StatusOK, jsonResp{Code: model.CodeOK, Message: "success",
		Data: model.AutoStartFlowResponse{Started: true, InstanceID: &id}})
}
