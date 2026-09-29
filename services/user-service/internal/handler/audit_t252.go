// T252 12.3 操作日志：写入埋点 + GET /api/v1/admin/audit-logs 查询
//
// 埋点位置（本卡实际覆盖，均在 user-service 写通道内）：
//
//	登录成功 / 患者档案新增 / 患者团队绑定与批量绑定 / 患者详情查看（§9.2a 读审计）
//	团队与成员增删改 / 技师增删改启停 / 医护账号增改·重置密码·启停（T314）/ 角色增删改（11.2）/ 权限矩阵写入（11.3）
//	系统参数写入（§7D.12）/ 告警规则写入（2.2）
//
// 订正一（T450 DEF-A，2026-09-28）：上面这句原写「患者档案新增与编辑」，但 PUT /admin/patients/:patientId
// 一直不在 auditRoutes 表内（本文件头注原来自述「PR #126（T248）合并后要补同一埋点」，未补）⇒
// 档案编辑通道此前零留痕，本笔补进表内，并把「原因 / 改前改后快照」经 setAuditTrace 汇进同一条 detail。
//
// 🔴 跨服务埋点（device 安装与校准、alert 告警处理、data 归档删除、msg 通知规则）
//
//	需 PM 先定归属与写入通道，本卡不自行跨服务改。
//	订正二（T448，2026-09-28）：device 域已有一条不在本通道内的埋点先例——清除设备 WiFi 的留痕
//	由 device-service 同库直写 audit_logs（services/device-service/internal/repo/audit_t448.go），
//	动词沿用本文件词表的 data_modify。归属仍待 PM 认，认之前别把本段读成「device 域全无留痕」。
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// 操作类型（audit_logs.action，设计稿 系统配置.html:160 下拉四项）
const (
	auditActionLogin       = "login"
	auditActionDataModify  = "data_modify"
	auditActionConfig      = "config_change"
	auditActionPermissions = "permission_change"
	auditActionDataRead    = "data_read" // §9.2a「查看了哪个患者的数据」——PRD 四类之外的读审计，见交件待裁
)

// auditActionLabels action → 中文标签（后端给，避免前端硬编码映射；未知值原样回显）
var auditActionLabels = map[string]string{
	auditActionLogin:       "登录",
	auditActionDataModify:  "数据修改",
	auditActionConfig:      "配置变更",
	auditActionPermissions: "权限变更",
	auditActionDataRead:    "数据查看",
}

// audit 写一条操作日志：操作人身份取 gateway 注入头（X-User-Id / X-Role，架构 §5.2），
// IP 取 c.ClientIP()（gateway ReverseProxy 已透传 X-Forwarded-For）。
// 🔴 审计失败不阻断主业务（只记 WARN）：留痕是合规要求，但不能因为审计表写不进
// 就让登录/改配置整体失败——那样运维只能停机，风险更高。
func (h *Handler) audit(c *gin.Context, in repo.AuditInput) {
	if in.OperatorID == "" {
		in.OperatorID = c.GetHeader(headerUserID)
	}
	if in.OperatorRole == "" {
		in.OperatorRole = c.GetHeader(headerRole)
	}
	if in.IP == "" {
		in.IP = c.ClientIP()
	}
	// T450 DEF-A：audit_logs 无设备列，「操作发起设备」只有 User-Agent 可取（ip 列已在行内）。
	// 中间件与 handler 两条埋点通道都经本函数，故在此统一补，调用方不必各自记一遍。
	if ua := strings.TrimSpace(c.Request.UserAgent()); ua != "" {
		if in.Detail == nil {
			in.Detail = map[string]any{}
		}
		if _, set := in.Detail["userAgent"]; !set {
			in.Detail["userAgent"] = ua
		}
	}
	if err := h.store.WriteAuditLog(c.Request.Context(), in); err != nil {
		ctxLogger(c).Warn().Err(err).Str("action", in.Action).
			Str("target_type", in.TargetType).Str("target_id", in.TargetID).
			Msg("audit log write failed")
	}
}

// auditRoute 表驱动埋点：一条路由 → 一类审计
type auditRoute struct {
	action     string
	targetType string
	param      string // 取 c.Param(param) 作 target_id；空 = 无目标 ID（如批量/创建）
	desc       string // 含 %s 时用 target_id 填充
}

// handler 侧回填审计内容的两个 gin 键（T450 DEF-A）。
//
// 中间件在 handler 之后跑，只拿得到路由与路径参数；「变更原因 / 改前改后快照」这些
// 只有 handler 自己知道的字段由 setAuditTrace 汇进同一条 detail，一次请求仍只写一行
// audit_logs（不再像 T252 那样为结构化内容把整条埋点移到 handler 内，避免两条通道各写一份）。
const (
	auditDetailKey  = "auditDetail"  // map[string]any → AuditInput.Detail
	auditSummaryKey = "auditSummary" // string → 覆盖表内 desc（前端审计页只有「操作描述」一列，不渲染 detail）
)

// setAuditTrace handler 在写成功后调用：kv 落 detail，summary 作为「操作描述」文案。
// summary 传空串表示沿用表内 desc。多次调用按后写覆盖同键。
func setAuditTrace(c *gin.Context, summary string, kv map[string]any) {
	if summary != "" {
		c.Set(auditSummaryKey, summary)
	}
	if len(kv) == 0 {
		return
	}
	if prev, ok := c.Get(auditDetailKey); ok {
		if m, ok := prev.(map[string]any); ok {
			for k, v := range kv {
				m[k] = v
			}
			return // gin 存的是同一份 map，就地合并即可
		}
	}
	c.Set(auditDetailKey, kv)
}

func auditTraceDetail(c *gin.Context) map[string]any {
	if v, ok := c.Get(auditDetailKey); ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return nil
}

// auditRoutes 既有端点的审计埋点表（key = "METHOD /gin 全路径"，直接精确匹配 c.FullPath()）。
//
// 为什么用中间件而不是逐个 handler 埋点：这 20 条是 T030~T085 已有实现，逐个改要动 12 个函数、
// 每处都要找成功返回点；表驱动集中一处、新增端点加一行即可，且只在 HTTP < 400 时留痕
// （校验失败/不存在的请求不产生审计噪声）。
//
// T252 新增的告警规则/角色写端点**不在本表**：它们由 handler 内 h.audit 直接埋点，
// 因为能给出中间件拿不到的结构化内容（变更点位清单、权限快照），避免同一请求写两条。
var auditRoutes = map[string]auditRoute{
	http.MethodGet + " /api/v1/admin/patients/:patientId": {auditActionDataRead, "patient", "patientId", "查看患者档案 %s（等保 §9.2a 读留痕）"},

	http.MethodPost + " /api/v1/admin/patients":                          {auditActionDataModify, "patient", "", "创建患者"},
	http.MethodPut + " /api/v1/admin/patients/:patientId/team":           {auditActionDataModify, "patient", "patientId", "为患者 %s 分配团队"},
	http.MethodPost + " /api/v1/admin/patients/batch-bind":               {auditActionDataModify, "patient", "", "批量绑定患者到团队"},
	http.MethodPost + " /api/v1/admin/patients/:patientId/unbind-wechat": {auditActionDataModify, "patient", "patientId", "解绑患者 %s 的微信"},
	http.MethodPut + " /api/v1/admin/patients/:patientId/phone":          {auditActionDataModify, "patient", "patientId", "修改患者 %s 的手机号"},
	// T477 设登录口令：desc 不带口令也不带哈希（同上面 doctor reset-password 的口径）。
	http.MethodPost + " /api/v1/admin/patients/:patientId/password": {auditActionDataModify, "patient", "patientId", "为患者 %s 设置登录口令"},
	// T450 DEF-A：PR #126（T248）合并时欠下的档案编辑埋点，本笔补上（文件头注自述「合并后要补同一埋点」）。
	http.MethodPut + " /api/v1/admin/patients/:patientId": {auditActionDataModify, "patient", "patientId", "编辑患者档案 %s"},
	// T467 患者档案删除：收口档案比改手机号更该留痕，同族写端点全在表内，这条不能漏。
	http.MethodDelete + " /api/v1/admin/patients/:patientId": {auditActionDataModify, "patient", "patientId", "删除患者档案 %s"},

	http.MethodPost + " /api/v1/teams":                             {auditActionDataModify, "team", "", "创建团队"},
	http.MethodPut + " /api/v1/teams/:teamId":                      {auditActionDataModify, "team", "teamId", "编辑团队 %s"},
	http.MethodDelete + " /api/v1/teams/:teamId":                   {auditActionDataModify, "team", "teamId", "删除团队 %s"},
	http.MethodPost + " /api/v1/teams/:teamId/members":             {auditActionDataModify, "team", "teamId", "团队 %s 新增成员"},
	http.MethodPut + " /api/v1/teams/:teamId/members/:memberId":    {auditActionDataModify, "member", "memberId", "编辑成员 %s"},
	http.MethodDelete + " /api/v1/teams/:teamId/members/:memberId": {auditActionDataModify, "member", "memberId", "移除成员 %s"},

	http.MethodPost + " /api/v1/admin/technicians":          {auditActionDataModify, "technician", "", "创建技师账号"},
	http.MethodPut + " /api/v1/admin/technicians/:techId":   {auditActionDataModify, "technician", "techId", "编辑技师账号 %s"},
	http.MethodPost + " /api/v1/technicians/:techId/toggle": {auditActionDataModify, "technician", "techId", "启停技师账号 %s"},

	// T314 医护账号四类写操作（PRD §7D.10（6）「本页全部写操作计入操作日志」）。
	// 🔴 description 不带密码：随机初始密码只在 HTTP 响应里一次性出现，不留进 audit_logs.detail。
	http.MethodPost + " /api/v1/admin/doctors":                          {auditActionDataModify, "doctor", "", "创建医护账号（登录账号由服务端发号）"},
	http.MethodPut + " /api/v1/admin/doctors/:doctorId":                 {auditActionDataModify, "doctor", "doctorId", "编辑医护账号 %s"},
	http.MethodPost + " /api/v1/admin/doctors/:doctorId/reset-password": {auditActionDataModify, "doctor", "doctorId", "重置医护账号 %s 的登录密码"},
	http.MethodPost + " /api/v1/admin/doctors/:doctorId/status":         {auditActionDataModify, "doctor", "doctorId", "启停医护账号 %s"},

	http.MethodPut + " /api/v1/admin/roles/:roleId/permissions": {auditActionPermissions, "role", "roleId", "写入角色 %s 的权限矩阵"},
	http.MethodPut + " /api/v1/admin/settings":                  {auditActionConfig, "sys_config", "", "写入系统参数（§7D.12）"},

	http.MethodPost + " /api/v1/patients/:patientId/orthosis-plans": {auditActionDataModify, "orthosis_plan", "patientId", "为患者 %s 保存矫形方案"},
	http.MethodPut + " /api/v1/patients/:patientId":                 {auditActionDataModify, "patient", "patientId", "患者本人修改档案 %s（T226 白名单字段）"},
	http.MethodPost + " /api/v1/feeling-logs/:logId/reply":          {auditActionDataModify, "feeling_log", "logId", "回复佩戴感受日志 %s"},
	http.MethodPost + " /api/v1/feedbacks/:feedbackId/process":      {auditActionDataModify, "feedback", "feedbackId", "处理反馈 %s"},
	http.MethodPost + " /api/v1/admin/review-records":               {auditActionDataModify, "review_record", "", "创建复查记录"},
}

// auditTrail 表驱动审计中间件（挂在 /api/v1 组、scopeGuard 之后）：
// 请求处理完成且 HTTP < 400 才留痕；身份/IP 由 h.audit 从 gateway 注入头补齐。
func (h *Handler) auditTrail() gin.HandlerFunc {
	return func(c *gin.Context) {
		route, tracked := auditRoutes[c.Request.Method+" "+c.FullPath()]
		if !tracked {
			c.Next()
			return
		}
		c.Next()
		if c.Writer.Status() >= http.StatusBadRequest {
			return
		}
		targetID := ""
		if route.param != "" {
			targetID = c.Param(route.param)
		}
		desc := route.desc
		if strings.Contains(desc, "%s") {
			desc = fmt.Sprintf(desc, targetID)
		}
		if summary := c.GetString(auditSummaryKey); summary != "" {
			desc = summary
		}
		h.audit(c, repo.AuditInput{
			Action:      route.action,
			TargetType:  route.targetType,
			TargetID:    targetID,
			Description: desc,
			Detail:      auditTraceDetail(c),
		})
	}
}

// parseAuditTime 时间窗入参：RFC3339 优先；无时区后缀的按业务时区（Asia/Shanghai）解释，
// 避免切日偏移 8 小时（ParseInLocation 仅在输入不含时区信息时用默认位置）。
func parseAuditTime(field, v string) (*time.Time, *model.AppError) {
	if v == "" {
		return nil, nil
	}
	layouts := []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, v, shanghaiLoc()); err == nil {
			return &t, nil
		}
	}
	return nil, model.ErrInvalidParam("invalid %s: %q (want RFC3339)", field, v)
}

// shanghaiLoc 业务时区（PRD §9.2a 按自然日切分）；tzdata 缺失时回退固定 +08:00
func shanghaiLoc() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}

// toAuditDTO 行投影 → 契约 AuditLog（description 取 detail->>'description'）
func toAuditDTO(r repo.AuditLogRow) model.AuditLogDTO {
	dto := model.AuditLogDTO{
		LogID:        r.LogID,
		OperatorID:   r.OperatorID,
		OperatorName: r.OperatorName,
		OperatorRole: r.OperatorRole,
		Action:       r.Action,
		ActionLabel:  auditActionLabels[r.Action],
		TargetType:   r.TargetType,
		TargetID:     r.TargetID,
		IP:           r.IP,
		Ts:           r.Ts.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if dto.ActionLabel == "" {
		dto.ActionLabel = r.Action
	}
	if r.Detail != nil && *r.Detail != "" {
		dto.Detail = json.RawMessage(*r.Detail)
		var parsed map[string]json.RawMessage
		if err := json.Unmarshal([]byte(*r.Detail), &parsed); err == nil {
			if raw, present := parsed["description"]; present {
				var desc string
				if err := json.Unmarshal(raw, &desc); err == nil {
					dto.Description = desc
				}
			}
		}
	}
	return dto
}

// getAuditLogs GET /api/v1/admin/audit-logs —— 操作日志分页查询（T252 12.3）
//
// date=YYYY-MM-DD（Asia/Shanghai 单日）与 from/to（半开区间）互斥，同时给 → 400。
func (h *Handler) getAuditLogs(c *gin.Context) {
	page, pageSize, appErr := parsePaging(c)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	date := strings.TrimSpace(c.Query("date"))
	fromRaw := strings.TrimSpace(c.Query("from"))
	toRaw := strings.TrimSpace(c.Query("to"))
	if date != "" && (fromRaw != "" || toRaw != "") {
		fail(c, model.ErrInvalidParam("date and from/to are mutually exclusive"))
		return
	}

	var from, to *time.Time
	if date != "" {
		day, err := time.ParseInLocation("2006-01-02", date, shanghaiLoc())
		if err != nil {
			fail(c, model.ErrInvalidParam("invalid date %q (want YYYY-MM-DD)", date))
			return
		}
		start, end := day, day.AddDate(0, 0, 1)
		from, to = &start, &end
	} else {
		var appErr *model.AppError
		if from, appErr = parseAuditTime("from", fromRaw); appErr != nil {
			fail(c, appErr)
			return
		}
		if to, appErr = parseAuditTime("to", toRaw); appErr != nil {
			fail(c, appErr)
			return
		}
		if from != nil && to != nil && !to.After(*from) {
			fail(c, model.ErrInvalidParam("to must be after from"))
			return
		}
	}

	rows, total, err := h.store.QueryAuditLogs(c.Request.Context(), repo.AuditFilter{
		From:       from,
		To:         to,
		Action:     strings.TrimSpace(c.Query("action")),
		Operator:   strings.TrimSpace(c.Query("operator")),
		TargetType: strings.TrimSpace(c.Query("targetType")),
		TargetID:   strings.TrimSpace(c.Query("targetId")),
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		fail(c, model.ErrInternal("query audit logs failed"))
		return
	}
	list := make([]model.AuditLogDTO, 0, len(rows))
	for _, r := range rows {
		list = append(list, toAuditDTO(r))
	}
	ok(c, model.PageData{List: list, Total: total, Page: page, PageSize: pageSize})
}
