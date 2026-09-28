package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// requireAdminRole T190：后台患者管理写端点的 handler 层角色判定（纵深防御）。
// gateway RBAC 矩阵已在入口拦下非 admin，此处兜底「绕过网关直连服务」的请求。
// fail-closed：X-Role 缺失即视为无权限。
func requireAdminRole(c *gin.Context) bool {
	return c.GetHeader(headerRole) == roleAdmin
}

// unbindWechat T085：管理端解绑患者微信（wx_openid 置 NULL + 审计）。
func (h *Handler) unbindWechat(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can unbind patient wechat"))
		return
	}
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}
	op := operatorID(c, "")

	// T450 DEF-A：解绑前的绑定态（只记「有没有绑」，openid 本体不入审计）。
	// 这一发读只服务审计，失败不回滚、不改响应：未知就记 null，主流程照走 UnbindWechat 的原有判定。
	boundBefore := (*bool)(nil)
	if openID, err := h.store.GetPatientWXOpenID(c.Request.Context(), patientID); err == nil {
		bound := openID != ""
		boundBefore = &bound
	}

	if err := h.store.UnbindWechat(c.Request.Context(), patientID); err != nil {
		if err == repo.ErrPatientNotFound {
			fail(c, model.ErrNotFound("patient not found"))
			return
		}
		fail(c, model.ErrInternal("unbind wechat failed"))
		return
	}

	setAuditTrace(c, fmt.Sprintf("解绑患者 %s 的微信：解绑前绑定态 %s，解绑后未绑定", patientID, auditBoundLabel(boundBefore)), map[string]any{
		"before":  map[string]any{"wechatBound": auditBool(boundBefore)},
		"after":   map[string]any{"wechatBound": false},
		"changed": []string{"wx_openid"},
	})

	ctxLogger(c).Info().
		Str("action", "unbind_wechat").
		Str("operator_id", op).
		Str("patient_id", patientID).
		Msg("unbind patient wechat")

	ok(c, gin.H{"patientId": patientID})
}

// auditBoundLabel 绑定态文案；nil = 解绑前那次只读探测没拿到值，明写「未知」而不是编成未绑
func auditBoundLabel(bound *bool) string {
	if bound == nil {
		return "未知（读取绑定态失败）"
	}
	if *bound {
		return "已绑定"
	}
	return "未绑定"
}

// auditBool 结构化侧的同义表达：探测失败写 JSON null（字段存在但无值），与「false=确认未绑」区分
func auditBool(bound *bool) any {
	if bound == nil {
		return nil
	}
	return *bound
}

// updatePhoneRequest PUT /admin/patients/:id/phone 请求体
type updatePhoneRequest struct {
	Phone  string `json:"phone"`
	Reason string `json:"reason"`
}

// updatePatientPhone T085：管理端改手机号（格式校验 → hash 冲突 409 → 同步更新 enc+hash + 审计）。
func (h *Handler) updatePatientPhone(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can update patient phone"))
		return
	}
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}
	var req updatePhoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	if !validPhone(req.Phone) {
		fail(c, model.ErrInvalidParam("invalid phone format: must be 11 digits starting with 1"))
		return
	}
	op := operatorID(c, "")

	// 患者存在性校验
	patient, err := h.store.GetPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("query patient failed"))
		return
	}
	if patient == nil {
		fail(c, model.ErrNotFound("patient not found"))
		return
	}

	// phone_hash 冲突校验（排除自身）
	newHash := phone.Hash(req.Phone)
	taken, err := h.store.PatientPhoneHashTaken(c.Request.Context(), newHash, patientID)
	if err != nil {
		fail(c, model.ErrInternal("check phone hash failed"))
		return
	}
	if taken {
		fail(c, model.ErrConflict("phone already exists"))
		return
	}

	// 加密 + 哈希同步更新
	enc, hash, appErr := h.preparePhone(req.Phone)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	if err := h.store.UpdatePatientPhone(c.Request.Context(), patientID, enc, hash); err != nil {
		if err == repo.ErrPatientNotFound {
			fail(c, model.ErrNotFound("patient not found"))
			return
		}
		fail(c, model.ErrInternal("update patient phone failed"))
		return
	}

	// 审计日志（before/after 快照）
	// 🔴 脱敏口径（T450 DEF-A，回 Alice 13:12 第三条「还原性」）：改前/改后都取脱敏号（138****1111 一类），
	// 密文本体及其十六进制串既不落审计 detail 也不落运行日志；改前另带 phoneState 说明能否解开。
	before := h.phoneView(patient.PhoneEnc)
	afterMasked := phone.Mask(req.Phone)

	setAuditTrace(c, fmt.Sprintf("修改患者 %s 的手机号：原因「%s」，改前 %s，改后 %s",
		patientID, auditReason(req.Reason), auditPhonePhrase(before), afterMasked), map[string]any{
		"reason":  req.Reason,
		"before":  map[string]any{"phone": before.Masked, "phoneState": string(before.State)},
		"after":   map[string]any{"phone": afterMasked, "phoneState": string(phone.PhoneStateMasked)},
		"changed": []string{"phone"},
	})

	ctxLogger(c).Info().
		Str("action", "update_patient_phone").
		Str("operator_id", op).
		Str("patient_id", patientID).
		Str("before", before.Masked).
		Str("before_state", string(before.State)).
		Str("after", afterMasked).
		Str("reason", req.Reason).
		Msg("update patient phone")

	ok(c, gin.H{"patientId": patientID})
}

// auditReason 原因留空时的文案（改号弹窗必填原因，直连 API 可为空 ⇒ 明写「未填写」不伪造）
func auditReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "未填写"
	}
	return reason
}

// auditPhonePhrase 改前手机号的描述文案。absent 时 Masked 是空串，直接拼进句子会读成「改前 （absent）」
// 这种半截话，故明写「无手机号」；unreadable 保留占位符 + 状态，说明当前值取不出来。
func auditPhonePhrase(v phone.PhoneView) string {
	if v.State == phone.PhoneStateAbsent {
		return "无手机号（absent）"
	}
	return fmt.Sprintf("%s（%s）", v.Masked, v.State)
}

// adminPatientEditRequest PUT /api/v1/admin/patients/:patientId 入参（T248 4.3，指针=nil=不改）。
// 只覆盖 PRD §7D.3「编辑患者弹窗」五项；phone / teamId / primaryDoctorId / status 各有专属端点，
// 由 DisallowUnknownFields 拒掉，避免调用方以为改了其实被静默丢弃。
//
// ClearFields（T450-②b 乙案，PM 2026-09-28 17:41 拍）= 显式声明「这些列改回 NULL」。
// 五个指针键只能表达「不给 / 给个值」两态：JSON null 解码后与键缺席同形 ⇒ 接口此前
// 根本没有「恢复为空」的入参形态，管理端唯一置空通道只能写空串（Alice 第 70 轮登记的那条差异）。
// 同一列既给值又被列进 clearFields 判 400，不留「最后一个赢」的隐式优先级。
type adminPatientEditRequest struct {
	Name        *string  `json:"name"`
	Gender      *string  `json:"gender"`
	Age         *int     `json:"age"`
	Diagnosis   *string  `json:"diagnosis"`
	CobbAngle   *float64 `json:"cobbAngle"`
	ClearFields []string `json:"clearFields"`
}

// clearFieldOrder 白名单枚举序（错误文案按它拼，map 序随机，不进文案会写成「看得到却复现不出」的串）。
var clearFieldOrder = []string{"gender", "age", "diagnosis", "cobbAngle"}

// maxDiagnosisLen patients.diagnosis VARCHAR(255)
const maxDiagnosisLen = 255

// updatePatientAdmin PUT /api/v1/admin/patients/:patientId —— admin 侧患者档案编辑（T248 4.3）。
// Cobb 角度可写系 Boss 2026-09-17 裁定 B 的「临床侧写通道」延伸：患者自助通道仍拒该键
// （见 patient_profile_write.go 头注）。
func (h *Handler) updatePatientAdmin(c *gin.Context) {
	if !requireAdminRole(c) {
		fail(c, model.ErrForbidden("only admin can edit patient profile"))
		return
	}
	patientID := c.Param("patientId")
	if patientID == "" {
		fail(c, model.ErrInvalidParam("patientId is required"))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		fail(c, model.ErrInvalidParam("read request body failed"))
		return
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var req adminPatientEditRequest
	if decErr := dec.Decode(&req); decErr != nil {
		fail(c, model.ErrInvalidParam("request contains fields outside the editable set: %v", decErr))
		return
	}
	in, appErr := buildAdminPatientEdit(&req)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	// T450 DEF-A：改前快照需要写前的行值。这一发读只服务审计 ⇒ 失败不拦主流程，
	// 拿不到就把 before 记为 null（改后值仍来自下面那次既有读），不改本端点原有的 404/500 判定序。
	var beforeRow *repo.PatientRow
	if row, err := h.store.GetPatient(c.Request.Context(), patientID); err == nil {
		beforeRow = row
	}
	if err := h.store.UpdatePatientProfile(c.Request.Context(), patientID, *in); err != nil {
		if err == repo.ErrPatientNotFound {
			fail(c, model.ErrNotFound("patient not found: %s", patientID))
			return
		}
		fail(c, model.ErrInternal("update patient failed"))
		return
	}
	row, err := h.store.GetPatient(c.Request.Context(), patientID)
	if err != nil {
		fail(c, model.ErrInternal("get patient failed"))
		return
	}
	if row == nil {
		fail(c, model.ErrNotFound("patient not found: %s", patientID))
		return
	}
	before, after, changed := auditProfileDiff(beforeRow, row)
	summary := fmt.Sprintf("编辑患者档案 %s：%d 个字段变更（%s）", patientID, len(changed), strings.Join(changed, "、"))
	switch {
	case beforeRow == nil:
		// 写前那发只读没成功：不能反推「无变更」（那是把「我看不到」写成「没变化」）
		summary = fmt.Sprintf("编辑患者档案 %s：改前快照读取失败，本次变更字段无法比对", patientID)
	case len(changed) == 0:
		summary = fmt.Sprintf("编辑患者档案 %s：提交字段与库内现值相同，无字段变更", patientID)
	}
	setAuditTrace(c, summary, map[string]any{
		"before":  before,
		"after":   after,
		"changed": changed,
	})
	ok(c, toPatientDTO(*row))
}

// auditProfileDiff 只比对本端点可写的五个字段（name/gender/age/diagnosis/cobbAngle），
// 逐字段给出改前/改后；未提交的字段不进快照（写了会误导成「被改成现值」）。
// beforeRow 为 nil = 写前那一发读没成功，快照记空并在卡面明说。
func auditProfileDiff(beforeRow, afterRow *repo.PatientRow) (map[string]any, map[string]any, []string) {
	before := map[string]any{}
	after := map[string]any{}
	changed := make([]string, 0, 5)
	if beforeRow == nil || afterRow == nil {
		return nil, nil, changed
	}
	pairs := []struct {
		key           string
		before, after any
	}{
		{"name", beforeRow.Name, afterRow.Name},
		{"gender", auditStr(beforeRow.Gender), auditStr(afterRow.Gender)},
		{"age", auditInt(beforeRow.Age), auditInt(afterRow.Age)},
		{"diagnosis", auditStr(beforeRow.Diagnosis), auditStr(afterRow.Diagnosis)},
		{"cobbAngle", auditFloat(beforeRow.CobbAngle), auditFloat(afterRow.CobbAngle)},
	}
	for _, p := range pairs {
		if p.before == p.after {
			continue
		}
		before[p.key] = p.before
		after[p.key] = p.after
		changed = append(changed, p.key)
	}
	return before, after, changed
}

// auditStr / auditInt / auditFloat 快照取值：库里为 NULL 就记 null（不是空串、不是 0），
// 与「本端点没提交该字段」区分得开——后者压根不进快照。
func auditStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func auditInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func auditFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// buildAdminPatientEdit 值域校验 + 装配 repo 入参；一个字段都没给 → 400（空编辑无意义）。
// clearFields 单独出现也算「有字段」：只置空不写值，本身就是一次有效编辑。
func buildAdminPatientEdit(req *adminPatientEditRequest) (*repo.PatientProfileUpdate, *model.AppError) {
	in := &repo.PatientProfileUpdate{}
	anyField := false
	given := map[string]bool{} // 本次已给值的字段名，用于与 clearFields 撞列判定
	if req.Name != nil {
		name := trimStr(*req.Name)
		if name == "" || runeLen(name) > maxPatientNameLen {
			return nil, model.ErrInvalidParam("name must be 1-%d characters", maxPatientNameLen)
		}
		in.Name = &name
		anyField = true
		given["name"] = true
	}
	if req.Gender != nil {
		if *req.Gender != "male" && *req.Gender != "female" {
			return nil, model.ErrInvalidParam("gender must be male or female")
		}
		in.Gender = req.Gender
		anyField = true
		given["gender"] = true
	}
	if req.Age != nil {
		if *req.Age < 0 || *req.Age > 150 { // patients.age CHECK (BETWEEN 0 AND 150)
			return nil, model.ErrInvalidParam("age must be between 0 and 150")
		}
		in.Age = req.Age
		anyField = true
		given["age"] = true
	}
	if req.Diagnosis != nil {
		v := trimStr(*req.Diagnosis)
		if runeLen(v) > maxDiagnosisLen {
			return nil, model.ErrInvalidParam("diagnosis exceeds %d characters", maxDiagnosisLen)
		}
		in.Diagnosis = &v
		anyField = true
		given["diagnosis"] = true
	}
	if req.CobbAngle != nil {
		// patients.cobb_angle NUMERIC(5,2)；值域与建档端点 createPatient 一致
		if *req.CobbAngle < 0 || *req.CobbAngle > 180 {
			return nil, model.ErrInvalidParam("invalid cobbAngle range: [0,180]")
		}
		in.CobbAngle = req.CobbAngle
		anyField = true
		given["cobbAngle"] = true
	}
	if len(req.ClearFields) > 0 {
		cols, appErr := resolveClearFields(req.ClearFields, given)
		if appErr != nil {
			return nil, appErr
		}
		in.ClearColumns = cols
		anyField = true
	}
	if !anyField {
		return nil, model.ErrInvalidParam("no updatable fields in request body")
	}
	return in, nil
}

// resolveClearFields 校验 clearFields 并映射成列名（顺序随请求，去重由重复项判定负责）。
// 三道判定各自独立成用例：表外字段名 / 重复项 / 与值键撞同一列。
// 表外名单按 clearFieldOrder 整份拼进文案 —— 只报「不接受」而不报可接受集合，
// 调用方只能靠猜，而猜出来的请求体形不成契约。
func resolveClearFields(fields []string, given map[string]bool) ([]string, *model.AppError) {
	accepted := strings.Join(clearFieldOrder, "、")
	seen := make(map[string]bool, len(fields))
	cols := make([]string, 0, len(fields))
	for _, raw := range fields {
		key := trimStr(raw)
		col, ok := repo.PatientProfileClearColumns[key]
		if !ok {
			// name 走不通置空也落在这里：patients.name 是 NOT NULL，压根不在可空列集合内
			return nil, model.ErrInvalidParam("clearFields accepts only %s", accepted)
		}
		if seen[key] {
			return nil, model.ErrInvalidParam("clearFields contains duplicated field: %s", key)
		}
		seen[key] = true
		if given[key] {
			return nil, model.ErrInvalidParam("field %s is both assigned and listed in clearFields", key)
		}
		cols = append(cols, col)
	}
	return cols, nil
}
