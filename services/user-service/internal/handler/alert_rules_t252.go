// T252 2.2 告警规则配置（设计稿 admin/告警管理.html:255-296 Tab2）
//
// 路由（均挂 gateway adminOnly RBAC）：
//
//	GET  /api/v1/admin/alert-rules            聚合视图（统一上下限 + 恒 20 条点位 + 全局规则）
//	PUT  /api/v1/admin/alert-rules/points     保存规则（网格逐点 + 统一上下限，一键提交）
//	POST /api/v1/admin/alert-rules/points/reset 恢复默认
//	PUT  /api/v1/admin/alert-rules/global     保存全局告警规则四项
//
// 键复用口径（不建第二份参数，避免双写漂移）：
//   - 统一压力上限 ≡ sys_configs threshold_pressure_high（§7D.12「压力偏高阈值」，引擎已消费）
//   - 佩戴时长下限 ≡ sys_configs wear_target_hours（§7D.12 dailyWearTargetHours）
//
// 🔴 生效范围（本卡只落存储 + 上限方向接线）：
//   - monitored=false 与逐点 upperN：alert-service 引擎按点覆盖 + 跳过未勾选点（本批同时改）
//   - lowerN：完整落库但**不产生告警**——「压力低于下限」需新告警类型（alerts.type 有 CHECK 枚举），待 PM/Boss 定
//   - continuous_wear_max_hours / report_timeout_minutes：可配置存储，触发何种告警 + 通知谁需 Boss 定
package handler

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// 告警规则专用 sys_configs 键（threshold_pressure_high / wear_target_hours 复用既有键，见 handler.go）
const (
	keyPressureLow            = "threshold_pressure_low"
	keyDeviceOfflineMinutes   = "device_offline_minutes"
	keyContinuousWearMaxHours = "continuous_wear_max_hours"
	keyReportTimeoutMinutes   = "report_timeout_minutes"
)

const (
	// alertPointCount 压力采集点总数（与 data-service model.PointCount / alert-service sensorPoints 同源）
	alertPointCount = 20
	// alertGridCols 设计稿 4×5 网格列数
	alertGridCols = 5
	// defaultUnifiedLowerN 统一压力下限默认（设计稿 :282 示意值 10；上限默认沿用 §7D.12 的 45N）
	defaultUnifiedLowerN = 10
	// alertTargetType audit_logs.target_type 取值
	alertTargetType = "alert_rule"
)

// alertPointIDRe 点位编号（与 DB CHECK 同式；P01–P20 零填充两位）
var alertPointIDRe = regexp.MustCompile(`^P(0[1-9]|1[0-9]|20)$`)

// floatOr 指针取值，nil 回退
func floatOr(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

// loadAlertRules 组装 Tab2 聚合视图：sys_configs 标量 + 稀疏逐点表 → 恒 20 条点位
func (h *Handler) loadAlertRules(ctx context.Context) (*model.AlertRulesDTO, *model.AppError) {
	keys := []string{
		keyPressureHigh, keyPressureLow, keyDeviceOfflineMinutes,
		keyWearTarget, keyContinuousWearMaxHours, keyReportTimeoutMinutes,
	}
	kvs, err := h.store.GetConfigs(ctx, keys)
	if err != nil {
		return nil, model.ErrInternal("read alert rules failed")
	}
	stored, err := h.store.ListAlertPointRules(ctx)
	if err != nil {
		return nil, model.ErrInternal("read alert point rules failed")
	}
	byID := make(map[string]repo.AlertPointRuleRow, len(stored))
	for _, r := range stored {
		byID[r.PointID] = r
	}

	unifiedUpper := numOr(kvs[keyPressureHigh], settingsDefaults.PressureHighThresholdN)
	unifiedLower := numOr(kvs[keyPressureLow], defaultUnifiedLowerN)

	points := make([]model.AlertPointRuleDTO, 0, alertPointCount)
	for i := 0; i < alertPointCount; i++ {
		id := fmt.Sprintf("P%02d", i+1)
		row, col := i/alertGridCols+1, i%alertGridCols+1
		p := model.AlertPointRuleDTO{
			PointID: id, Row: row, Col: col,
			Label: fmt.Sprintf("R%dC%d", row, col),
			// 默认勾选：未落库 = 从未被改过 ⇒ 与设计稿「全选 (20点)」初始态一致
			Monitored: true,
		}
		if r, exists := byID[id]; exists {
			p.Monitored = r.Monitored
			p.UpperN = r.UpperN
			p.LowerN = r.LowerN
		}
		p.EffectiveUpperN = floatOr(p.UpperN, unifiedUpper)
		p.EffectiveLowerN = floatOr(p.LowerN, unifiedLower)
		points = append(points, p)
	}

	return &model.AlertRulesDTO{
		UnifiedUpperN: unifiedUpper,
		UnifiedLowerN: unifiedLower,
		Points:        points,
		GlobalRules: model.AlertGlobalRulesDTO{
			DeviceOfflineMinutes:   numOr(kvs[keyDeviceOfflineMinutes], 30),
			DailyWearMinHours:      numOr(kvs[keyWearTarget], settingsDefaults.DailyWearTargetHours),
			ContinuousWearMaxHours: numOr(kvs[keyContinuousWearMaxHours], 23),
			ReportTimeoutMinutes:   numOr(kvs[keyReportTimeoutMinutes], 5),
		},
	}, nil
}

// getAlertRules GET /api/v1/admin/alert-rules
func (h *Handler) getAlertRules(c *gin.Context) {
	dto, appErr := h.loadAlertRules(c.Request.Context())
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, dto)
}

// validateThresholdN 压力阈值量程（与 §7D.12 上限校验口径一致：[1,200]；下限允许 0）
func validateThresholdN(field string, v float64, min, max float64) *model.AppError {
	if v < min || v > max {
		return model.ErrInvalidParam("%s must be in [%g,%g]", field, min, max)
	}
	return nil
}

// updateAlertPointRules PUT /api/v1/admin/alert-rules/points
//
// 逐点语义：points 为增量——**未列出的点位保持原值**；列出的点位内，
// upperN/lowerN 为 null 或缺省 ⇒ 清除独立阈值回退统一值；monitored 缺省 ⇒ 不改勾选。
func (h *Handler) updateAlertPointRules(c *gin.Context) {
	var req model.UpdateAlertPointRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	ctx := c.Request.Context()
	current, appErr := h.loadAlertRules(ctx)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	byID := make(map[string]model.AlertPointRuleDTO, len(current.Points))
	for _, p := range current.Points {
		byID[p.PointID] = p
	}

	unifiedUpper := floatOr(req.UnifiedUpperN, current.UnifiedUpperN)
	unifiedLower := floatOr(req.UnifiedLowerN, current.UnifiedLowerN)
	if appErr = validateThresholdN("unifiedUpperN", unifiedUpper, 1, 200); appErr != nil {
		fail(c, appErr)
		return
	}
	if appErr = validateThresholdN("unifiedLowerN", unifiedLower, 0, 200); appErr != nil {
		fail(c, appErr)
		return
	}
	if unifiedUpper <= unifiedLower {
		fail(c, model.ErrInvalidParam("unifiedUpperN (%g) must be greater than unifiedLowerN (%g)", unifiedUpper, unifiedLower))
		return
	}

	rules := make([]repo.AlertPointRuleRow, 0, len(req.Points))
	seen := make(map[string]struct{}, len(req.Points))
	for _, u := range req.Points {
		if !alertPointIDRe.MatchString(u.PointID) {
			fail(c, model.ErrInvalidParam("invalid pointId %q (want P01-P20)", u.PointID))
			return
		}
		if _, dup := seen[u.PointID]; dup {
			fail(c, model.ErrInvalidParam("duplicate pointId %q", u.PointID))
			return
		}
		seen[u.PointID] = struct{}{}

		monitored := byID[u.PointID].Monitored
		if u.Monitored != nil {
			monitored = *u.Monitored
		}
		effUpper := floatOr(u.UpperN, unifiedUpper)
		effLower := floatOr(u.LowerN, unifiedLower)
		if u.UpperN != nil {
			if appErr = validateThresholdN("upperN", *u.UpperN, 1, 200); appErr != nil {
				fail(c, appErr)
				return
			}
		}
		if u.LowerN != nil {
			if appErr = validateThresholdN("lowerN", *u.LowerN, 0, 200); appErr != nil {
				fail(c, appErr)
				return
			}
		}
		// 含与统一值合成后的生效区间也要自洽（契约：upperN <= lowerN → 400）
		if effUpper <= effLower {
			fail(c, model.ErrInvalidParam("point %s effective upper %.3g must be greater than lower %.3g", u.PointID, effUpper, effLower))
			return
		}
		rules = append(rules, repo.AlertPointRuleRow{
			PointID: u.PointID, Monitored: monitored, UpperN: u.UpperN, LowerN: u.LowerN,
		})
	}

	var kvs []repo.ConfigKV
	if req.UnifiedUpperN != nil {
		kvs = append(kvs, repo.ConfigKV{Key: keyPressureHigh, Value: fmtNum(unifiedUpper)})
	}
	if req.UnifiedLowerN != nil {
		kvs = append(kvs, repo.ConfigKV{Key: keyPressureLow, Value: fmtNum(unifiedLower)})
	}

	operator := operatorID(c, "ops")
	if err := h.store.SaveAlertRules(ctx, kvs, rules, operator); err != nil {
		fail(c, model.ErrInternal("save alert rules failed"))
		return
	}
	h.audit(c, repo.AuditInput{
		OperatorID:  operator,
		Action:      auditActionConfig,
		TargetType:  alertTargetType,
		TargetID:    "points",
		Description: fmt.Sprintf("保存告警规则：统一上限 %.0fN / 下限 %.0fN，逐点变更 %d 个", unifiedUpper, unifiedLower, len(rules)),
		Detail:      map[string]any{"pointIds": pointIDs(rules)},
	})

	latest, appErr := h.loadAlertRules(ctx)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, latest)
}

// pointIDs 审计 detail 用：本次变更的点位编号
func pointIDs(rules []repo.AlertPointRuleRow) []string {
	ids := make([]string, 0, len(rules))
	for _, r := range rules {
		ids = append(ids, r.PointID)
	}
	return ids
}

// resetAlertPointRules POST /api/v1/admin/alert-rules/points/reset
// 清空逐点表 + 统一上下限回默认（上限 §7D.12 = 45N，下限设计稿 = 10N）。
// 🔴 全局规则四项**不在本按钮范围内**（设计稿「恢复默认」只在规则卡内，全局卡另有保存按钮）。
func (h *Handler) resetAlertPointRules(c *gin.Context) {
	ctx := c.Request.Context()
	kvs := []repo.ConfigKV{
		{Key: keyPressureHigh, Value: strconv.FormatFloat(settingsDefaults.PressureHighThresholdN, 'f', -1, 64)},
		{Key: keyPressureLow, Value: strconv.Itoa(defaultUnifiedLowerN)},
	}
	operator := operatorID(c, "ops")
	if err := h.store.ResetAlertRules(ctx, kvs, operator); err != nil {
		fail(c, model.ErrInternal("reset alert rules failed"))
		return
	}
	h.audit(c, repo.AuditInput{
		OperatorID:  operator,
		Action:      auditActionConfig,
		TargetType:  alertTargetType,
		TargetID:    "points",
		Description: fmt.Sprintf("恢复默认告警规则：统一上限 %.0fN / 下限 %dN，清空逐点阈值", settingsDefaults.PressureHighThresholdN, defaultUnifiedLowerN),
	})

	latest, appErr := h.loadAlertRules(ctx)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, latest)
}

// updateAlertGlobalRules PUT /api/v1/admin/alert-rules/global（nil = 不改该键）
func (h *Handler) updateAlertGlobalRules(c *gin.Context) {
	var req model.UpdateAlertGlobalRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	ctx := c.Request.Context()
	current, appErr := h.loadAlertRules(ctx)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	merged := []struct {
		key   string
		in    *float64
		cur   float64
		field string
		min   float64
		max   float64
	}{
		{keyDeviceOfflineMinutes, req.DeviceOfflineMinutes, current.GlobalRules.DeviceOfflineMinutes, "deviceOfflineMinutes", 1, 1440},
		{keyWearTarget, req.DailyWearMinHours, current.GlobalRules.DailyWearMinHours, "dailyWearMinHours", 1, 24},
		{keyContinuousWearMaxHours, req.ContinuousWearMaxHours, current.GlobalRules.ContinuousWearMaxHours, "continuousWearMaxHours", 1, 24},
		{keyReportTimeoutMinutes, req.ReportTimeoutMinutes, current.GlobalRules.ReportTimeoutMinutes, "reportTimeoutMinutes", 1, 1440},
	}

	var kvs []repo.ConfigKV
	for _, m := range merged {
		if m.in == nil {
			continue
		}
		if appErr := validateThresholdN(m.field, *m.in, m.min, m.max); appErr != nil {
			fail(c, appErr)
			return
		}
		kvs = append(kvs, repo.ConfigKV{Key: m.key, Value: fmtNum(*m.in)})
	}
	if len(kvs) == 0 {
		fail(c, model.ErrInvalidParam("no global rule field provided"))
		return
	}

	operator := operatorID(c, "ops")
	// 统一上下限不在本端点范围（rules 传 nil ⇒ 只写 sys_configs 全局键）
	if err := h.store.SaveAlertRules(ctx, kvs, nil, operator); err != nil {
		fail(c, model.ErrInternal("save global alert rules failed"))
		return
	}
	h.audit(c, repo.AuditInput{
		OperatorID:  operator,
		Action:      auditActionConfig,
		TargetType:  alertTargetType,
		TargetID:    "global",
		Description: fmt.Sprintf("保存全局告警规则：变更 %d 项（设备离线 %.0f 分钟）", len(kvs), floatOr(req.DeviceOfflineMinutes, current.GlobalRules.DeviceOfflineMinutes)),
	})

	latest, appErr := h.loadAlertRules(ctx)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	ok(c, latest)
}
