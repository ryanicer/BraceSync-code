// T450 DEF-A 审计留痕实现侧测试：患者域两条写通道的「原因 + 改前改后快照」必须同批转好
//
// 验收对应（PM 12:53「判据以她那条为准」，Alice 卡内四条）：
//  1. 改号留痕四项：原因可见 / 改前值可读 / 改后值可读 / 失败写零留痕；
//  2. 还原性：审计行里既不出现明文手机号，也不出现密文十六进制串（改前改后一律脱敏号）；
//  3. 档案编辑通道：PR #126（T248）欠下的埋点必须补上，且与改号通道**同批**产出结构化内容
//     ——Alice「只有一条转好我判不通过」，故两组用例成对，且都断言 Detail 而不仅断言行数；
//  4. HTTP ≥ 400 的拒绝写仍零留痕（这条口径不能因为补字段而放宽）；
//  5. 「操作发起设备」维度（audit_logs 无设备列）落 detail.userAgent。
package handler

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/phone"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t450PhoneBefore = "13800001111" // 库里已有的号
	t450PhoneAfter  = "13900002222" // 本次改成的新号
)

// t450CipherPhone 造 patients.phone_enc 的落库形态（AES-GCM nonce||ciphertext）
func t450CipherPhone(t *testing.T, plain string) []byte {
	t.Helper()
	c, err := phone.NewCipher(testPhoneKey)
	require.NoError(t, err)
	enc, err := c.Encrypt(plain)
	require.NoError(t, err)
	return enc
}

// t450RowBlob 把审计行的人类可读面与结构化面拼成一个串，供「不得出现某字面量」的反证使用
// （只查 Description 会漏 detail，只查 detail 会漏描述文案）
func t450RowBlob(t *testing.T, row repo.AuditInput) string {
	t.Helper()
	raw, err := json.Marshal(row.Detail)
	require.NoError(t, err)
	return row.Description + "|" + string(raw)
}

// t450MapDetail 取 detail 里的一层对象（handler 直接塞 map[string]any，无需再过 JSON）
func t450MapDetail(t *testing.T, row repo.AuditInput, key string) map[string]any {
	t.Helper()
	m, ok := row.Detail[key].(map[string]any)
	require.True(t, ok, "detail.%s 应是对象，实得 %T", key, row.Detail[key])
	return m
}

// ── 通道一：改号 ──

func TestT450_Audit_PhoneWriteRecordsReasonAndMaskedSnapshot(t *testing.T) {
	e := newEnv(t, true, true)
	p := samplePatient()
	p.PhoneEnc = t450CipherPhone(t, t450PhoneBefore)
	e.store.patient = &p

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001/phone",
		map[string]string{"phone": t450PhoneAfter, "reason": "家长换号"}, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	// 一次请求一行审计：中间件仍只管路由，handler 只回填内容，不另写第二行
	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Equal(t, auditActionDataModify, row.Action)
	assert.Equal(t, "patient", row.TargetType)
	assert.Equal(t, "P20260001", row.TargetID)
	assert.Equal(t, "ADMIN0001", row.OperatorID)

	// 判据 1 原因可见 + 判据 2/3 改前改后可读（前端审计页只渲染「操作描述」一列，故三项都得在句子里）
	assert.Contains(t, row.Description, "家长换号")
	assert.Contains(t, row.Description, "138****1111")
	assert.Contains(t, row.Description, "139****2222")

	// 结构化侧（等保要能按字段回溯，不能只有一段话）
	assert.Equal(t, "家长换号", row.Detail["reason"])
	before := t450MapDetail(t, row, "before")
	assert.Equal(t, "138****1111", before["phone"])
	assert.Equal(t, string(phone.PhoneStateMasked), before["phoneState"])
	after := t450MapDetail(t, row, "after")
	assert.Equal(t, "139****2222", after["phone"])
	assert.Equal(t, string(phone.PhoneStateMasked), after["phoneState"])
	assert.Equal(t, []string{"phone"}, row.Detail["changed"])

	// 反证（Alice 13:12 第三条「还原性」）：明文号与密文十六进制都不进审计
	blob := t450RowBlob(t, row)
	assert.NotContains(t, blob, t450PhoneBefore, "改前明文不得入审计")
	assert.NotContains(t, blob, t450PhoneAfter, "改后明文不得入审计")
	assert.NotContains(t, blob, hex.EncodeToString(p.PhoneEnc), "密文十六进制串不得入审计")
}

// TestT450_Audit_PhoneWriteConflictLeavesNoRow 409 拒绝写零留痕：
// 「HTTP ≥ 400 不留痕」这条口径不能因为补字段而放宽（Alice 13:12 第四条）。
func TestT450_Audit_PhoneWriteConflictLeavesNoRow(t *testing.T) {
	e := newEnv(t, true, true)
	p := samplePatient()
	p.PhoneEnc = t450CipherPhone(t, t450PhoneBefore)
	e.store.patient = &p
	e.store.phoneTaken = true // phone_hash 撞库 → 409

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001/phone",
		map[string]string{"phone": t450PhoneAfter, "reason": "换个已存在的号"}, t248AdminHdr())
	require.Equal(t, http.StatusConflict, w.Code, resp.Message)
	assert.Empty(t, e.store.auditRows, "被拒绝的写不得留痕")
}

// TestT450_Audit_PhoneWriteUnreadableBeforeIsStated 改前密文解不开（seed 占位 bytea / 换 key）时，
// 描述里要写清「取不出来」，不能留个空串让人误读成「原来没填手机号」。
func TestT450_Audit_PhoneWriteUnreadableBeforeIsStated(t *testing.T) {
	e := newEnv(t, true, true)
	p := samplePatient()
	p.PhoneEnc = []byte("not-a-valid-aes-gcm-blob")
	e.store.patient = &p

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001/phone",
		map[string]string{"phone": t450PhoneAfter, "reason": "换号"}, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Contains(t, row.Description, phone.MaskUnavailable)
	assert.Contains(t, row.Description, string(phone.PhoneStateUnreadable))
	before := t450MapDetail(t, row, "before")
	assert.Equal(t, string(phone.PhoneStateUnreadable), before["phoneState"])
}

// TestT450_Audit_PhoneWriteWithoutStoredNumber 库里原本没号（absent）时句子仍要成文。
func TestT450_Audit_PhoneWriteWithoutStoredNumber(t *testing.T) {
	e := newEnv(t, true, true)
	p := samplePatient() // samplePatient 不带 PhoneEnc = 库里没存号
	e.store.patient = &p

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001/phone",
		map[string]any{"phone": t450PhoneAfter}, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Contains(t, row.Description, "无手机号（absent）")
	assert.Contains(t, row.Description, "未填写", "直连 API 不带 reason 时明写未填写，不伪造原因")
	before := t450MapDetail(t, row, "before")
	assert.Equal(t, string(phone.PhoneStateAbsent), before["phoneState"])
	assert.Equal(t, "", before["phone"])
}

// ── 通道二：档案编辑（本笔补上的欠账埋点） ──

func TestT450_Audit_ProfileEditIsTrackedWithDiff(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p
	e.store.profileApplies = true // 写后那次 GETPatient 读到新行，改前/改后才不是同一份快照

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001",
		map[string]any{"name": "患者小明改", "age": 15}, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	// 改前：这条路由压根不在 auditRoutes 表内 → 零留痕。改后：恰好一行。
	require.Len(t, e.store.auditRows, 1, "档案编辑通道必须留痕（PR #126 欠账，T450 补上）")
	row := e.store.auditRows[0]
	assert.Equal(t, auditActionDataModify, row.Action)
	assert.Equal(t, "patient", row.TargetType)
	assert.Equal(t, "P20260001", row.TargetID)
	assert.Contains(t, row.Description, "2 个字段变更")
	assert.Contains(t, row.Description, "name")
	assert.Contains(t, row.Description, "age")

	before := t450MapDetail(t, row, "before")
	after := t450MapDetail(t, row, "after")
	assert.Equal(t, "患者小明", before["name"])
	assert.Equal(t, "患者小明改", after["name"])
	assert.Equal(t, 14, before["age"])
	assert.Equal(t, 15, after["age"])
	assert.Equal(t, []string{"name", "age"}, row.Detail["changed"])

	// 没提交的字段不得进快照（进了会被读成「被改成了现值」）
	assert.NotContains(t, before, "gender")
	assert.NotContains(t, after, "gender")
	assert.NotContains(t, before, "diagnosis")
}

// TestT450_Audit_ProfileEditBadRequestLeavesNoRow 空编辑 400 → 零留痕（与改号通道同一口径）。
func TestT450_Audit_ProfileEditBadRequestLeavesNoRow(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001", map[string]any{}, t248AdminHdr())
	require.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Empty(t, e.store.auditRows)
}

// TestT450_Audit_ProfileEditNoFieldChange 提交的值与库内现值逐字相同：仍留痕（运营确实点了保存、
// 请求也确实放行了），但描述得明写「无字段变更」，不能写成「0 个字段变更（）」这种半截话。
func TestT450_Audit_ProfileEditNoFieldChange(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p
	e.store.profileApplies = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001",
		map[string]any{"name": p.Name}, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Contains(t, row.Description, "无字段变更")
	assert.Equal(t, []string{}, row.Detail["changed"])
}

// TestT450_Audit_ProfileEditWithoutBeforeSnapshot 写前那发只读失败：改前值不可知，
// 描述必须写「读取失败、无法比对」，不得退化成「无字段变更」（把「我看不到」写成「没变化」）。
func TestT450_Audit_ProfileEditWithoutBeforeSnapshot(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p
	e.store.profileApplies = true
	e.store.getPatientFirstErr = repo.ErrPatientNotFound // 只让审计用的写前读失败，写与写后读照常

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001",
		map[string]any{"name": "患者小明改"}, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, "改前快照读失败不该影响本端点响应", resp.Message)

	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Contains(t, row.Description, "改前快照读取失败")
	assert.NotContains(t, row.Description, "无字段变更")
	assert.Nil(t, row.Detail["before"], "取不到的快照落 null（键在、值无），不落成一份现值冒充改前")
	assert.Nil(t, row.Detail["after"])
}

// ── 设备维度 ──

// TestT450_Audit_UserAgentGoesToDetail audit_logs 没有设备列 ⇒ 「操作发起设备」只有 User-Agent 可取。
// 补在 h.audit 里，两条埋点通道（中间件 + handler 直调）都覆盖。
func TestT450_Audit_UserAgentGoesToDetail(t *testing.T) {
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p
	hdr := map[string]string{"X-User-Id": "ADMIN0001", "X-Role": roleAdmin,
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0) Chrome/141"}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001",
		map[string]any{"age": 15}, hdr)
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.auditRows, 1)
	assert.Equal(t, "Mozilla/5.0 (Windows NT 10.0) Chrome/141", e.store.auditRows[0].Detail["userAgent"])
}

// TestT450_Audit_MissingUserAgentWritesNoKey 无 UA（curl / 服务间直连）时不落空串键，
// 免得查询侧把「没带 UA」读成「设备是空字符串」。
func TestT450_Audit_MissingUserAgentWritesNoKey(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	w, resp := e.do(http.MethodPut, "/api/v1/admin/settings", validSettingsBody(),
		map[string]string{"X-User-Id": "A0001", "X-Role": roleAdmin})
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.auditRows, 1)
	assert.NotContains(t, e.store.auditRows[0].Detail, "userAgent")
}

// ── 纯函数侧：快照取值口径 ──

func TestT450_Audit_ProfileDiffCases(t *testing.T) {
	old := samplePatient() // name/gender/age/diagnosis/cobbAngle 全有值
	newRow := old
	newRow.Name = "改名"
	newGender := "female"
	newRow.Gender = &newGender // 库里原值不变，只把 gender 改掉
	newCobb := 31.5
	newRow.CobbAngle = &newCobb

	t.Run("NULL 现值要记成 null 不是空串", func(t *testing.T) {
		nullBefore := samplePatient()
		nullBefore.Gender = nil
		nullAfter := nullBefore
		afterGender := "male"
		nullAfter.Gender = &afterGender

		before, after, changed := auditProfileDiff(&nullBefore, &nullAfter)
		assert.Equal(t, []string{"gender"}, changed)
		assert.Contains(t, before, "gender")
		assert.Nil(t, before["gender"], "库里 NULL ⇒ 快照记 null（区别于「没提交该字段」压根不进快照）")
		assert.Equal(t, "male", after["gender"])
	})

	t.Run("改前快照缺失⇒不比对", func(t *testing.T) {
		before, after, changed := auditProfileDiff(nil, &newRow)
		assert.Nil(t, before)
		assert.Nil(t, after)
		assert.Empty(t, changed)
	})

	t.Run("逐字段比对", func(t *testing.T) {
		before, after, changed := auditProfileDiff(&old, &newRow)
		assert.Equal(t, []string{"name", "gender", "cobbAngle"}, changed)
		assert.Equal(t, "患者小明", before["name"])
		assert.Equal(t, "改名", after["name"])
		assert.Equal(t, "male", before["gender"])
		assert.Equal(t, "female", after["gender"])
		assert.Equal(t, 28.0, before["cobbAngle"])
		assert.Equal(t, 31.5, after["cobbAngle"])
		assert.NotContains(t, before, "age", "未变更的字段不进快照")
		assert.NotContains(t, after, "age")
	})
}

func TestT450_Audit_BoundLabelAndBoolAgree(t *testing.T) {
	// 描述文案与结构化值必须同向，否则审计页一句话和 detail 里的布尔互相打脸
	yes := true
	no := false
	for _, tc := range []struct {
		in        *bool
		wantLabel string
		wantVal   any
	}{
		{&yes, "已绑定", true},
		{&no, "未绑定", false},
		{nil, "未知（读取绑定态失败）", nil},
	} {
		assert.Equal(t, tc.wantLabel, auditBoundLabel(tc.in))
		assert.Equal(t, tc.wantVal, auditBool(tc.in))
	}
}

// ── 通道一/二的解绑腿：绑定态快照 ──

func TestT450_Audit_UnbindRecordsBeforeBoundState(t *testing.T) {
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p
	// 让「解绑前确有绑定」这一事实可从库里读出来（fake 按 patient_id 反查 openid）
	e.store.wxPatientByOpenID = map[string]*repo.PatientLoginRow{"oT450OLD": {PatientID: p.PatientID}}

	w, resp := e.do(http.MethodPost, "/api/v1/admin/patients/P20260001/unbind-wechat", nil, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Contains(t, row.Description, "解绑前绑定态 已绑定")
	assert.Equal(t, []string{"wx_openid"}, row.Detail["changed"])
	assert.Equal(t, true, t450MapDetail(t, row, "before")["wechatBound"])
	assert.Equal(t, false, t450MapDetail(t, row, "after")["wechatBound"])

	// openid 本体不入审计（它是可关联到微信账号的稳定标识，不是「改前值」）
	assert.NotContains(t, t450RowBlob(t, row), "oT450OLD")
}

// TestT450_Audit_UnbindUnknownBeforeState 库里没绑定记录时记 false（确认未绑），
// 与「读失败 ⇒ null」是两种语义；这里证前者，读失败那格由 auditBoundLabel(nil) 的表侧覆盖。
func TestT450_Audit_UnbindUnknownBeforeState(t *testing.T) {
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	w, resp := e.do(http.MethodPost, "/api/v1/admin/patients/P20260001/unbind-wechat", nil, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Contains(t, row.Description, "解绑前绑定态 未绑定")
	assert.Equal(t, false, t450MapDetail(t, row, "before")["wechatBound"])
}
