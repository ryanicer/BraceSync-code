// T252 2.2 告警规则配置端点实现侧测试（admin 告警页 Tab2）
//
// 验收对应（T252 ② 逐采集点阈值参数配置）：
//  1. GET 聚合视图恒 20 条点位（P01–P20 升序 + 4×5 行列/label），未落库点回默认；
//  2. 生效值合成：upperN/lowerN 为 null ⇒ 跟随统一上下限（前端不再自己算）；
//  3. PUT points 增量语义：未列出的点不动；monitored 缺省不改勾选；upperN=null 清除独立阈值；
//  4. 量程/编号/重复/上下限自洽校验一律 400，且校验失败不触达写通道；
//  5. 统一上限与 threshold_pressure_high 同键（不建第二份参数）；
//  6. 恢复默认 = 清空逐点表 + 统一上下限回默认，不越界动全局规则；
//  7. 每次写入留一条 config_change 审计（含变更点位清单），审计失败不阻断主流程。
//
// T257 12.4（三档合两键）追加：设备离线阈值 ≡ §7D.12 佩戴中断阈值（同键、同量程、同联动校验）。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t252RulesPath       = "/api/v1/admin/alert-rules"
	t252PointsPath      = "/api/v1/admin/alert-rules/points"
	t252PointsResetPath = "/api/v1/admin/alert-rules/points/reset"
	t252GlobalPath      = "/api/v1/admin/alert-rules/global"
)

func t252AdminHdr() map[string]string {
	return map[string]string{"X-User-Id": "A0001", "X-Role": "ROLE_ADMIN"}
}

func decodeRules(t *testing.T, raw json.RawMessage) model.AlertRulesDTO {
	t.Helper()
	var dto model.AlertRulesDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	return dto
}

// kvOf 从写入的 sys_configs 增量里取某个键（第二返回值 = 是否存在）
func kvOf(kvs []repo.ConfigKV, key string) (string, bool) {
	for _, kv := range kvs {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return "", false
}

func ruleFor(rules []repo.AlertPointRuleRow, pointID string) (repo.AlertPointRuleRow, bool) {
	for _, r := range rules {
		if r.PointID == pointID {
			return r, true
		}
	}
	return repo.AlertPointRuleRow{}, false
}

// ── GET 聚合视图 ──

func TestT252_GetAlertRules_EmptyDBFallsBackToDefaults(t *testing.T) {
	e := newEnv(t, true, true)

	w, resp := e.do(http.MethodGet, t252RulesPath, nil, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code)
	dto := decodeRules(t, resp.Data)

	assert.Equal(t, 5.0, dto.UnifiedUpperN, "统一上限默认取 T203 ÷10 后的 5N")
	assert.Equal(t, 1.0, dto.UnifiedLowerN, "统一下限默认与迁移 000021 / seed 的 threshold_pressure_low = 1 同值（T287）")
	require.Len(t, dto.Points, alertPointCount, "恒 20 条，前端不用判缺失")
	assert.Equal(t, "P01", dto.Points[0].PointID)
	assert.Equal(t, "P20", dto.Points[19].PointID)
	for _, p := range dto.Points {
		assert.True(t, p.Monitored, "未落库 = 从未取消勾选")
		assert.Nil(t, p.UpperN)
		assert.InDelta(t, 5.0, p.EffectiveUpperN, 1e-9)
		assert.InDelta(t, 1.0, p.EffectiveLowerN, 1e-9)
	}
	// 4×5 网格行列（P05 = 第 1 行末列，P06 = 第 2 行首列）
	assert.Equal(t, model.AlertPointRuleDTO{PointID: "P05", Row: 1, Col: 5, Label: "R1C5", Monitored: true, EffectiveUpperN: 5, EffectiveLowerN: 1}, dto.Points[4])
	assert.Equal(t, 2, dto.Points[5].Row)
	assert.Equal(t, 1, dto.Points[5].Col)
	assert.Equal(t, "R2C1", dto.Points[5].Label)

	assert.InDelta(t, 60, dto.GlobalRules.DeviceOfflineMinutes, 1e-9, "T257 12.4：默认同 §7D.12 佩戴中断阈值 60 分钟")
	assert.InDelta(t, 22, dto.GlobalRules.DailyWearMinHours, 1e-9, "佩戴下限默认复用 wear_target_hours（§7D.12），不建第二份参数")
	assert.InDelta(t, 23, dto.GlobalRules.ContinuousWearMaxHours, 1e-9)
	assert.InDelta(t, 5, dto.GlobalRules.ReportTimeoutMinutes, 1e-9)
}

func TestT252_GetAlertRules_MergesSparsePointRows(t *testing.T) {
	e := newEnv(t, true, true)
	upper, lower := 20.0, 5.0
	e.store.configs = map[string]string{
		keyPressureHigh:  "50",
		keyPressureLow:   "12",
		keyWearTarget:    "18",
		keyWearInterrupt: "45",
	}
	e.store.pointRules = []repo.AlertPointRuleRow{
		{PointID: "P01", Monitored: false},
		{PointID: "P07", Monitored: true, UpperN: &upper, LowerN: &lower},
		{PointID: "P20", Monitored: true}, // 只改勾选、无独立阈值
	}

	dto := decodeRules(t, func() json.RawMessage {
		w, resp := e.do(http.MethodGet, t252RulesPath, nil, t252AdminHdr())
		require.Equal(t, http.StatusOK, w.Code)
		return resp.Data
	}())

	assert.Equal(t, 50.0, dto.UnifiedUpperN)
	assert.Equal(t, 12.0, dto.UnifiedLowerN)
	assert.False(t, dto.Points[0].Monitored)
	assert.InDelta(t, 50.0, dto.Points[0].EffectiveUpperN, 1e-9, "未设独立上限仍回统一值")

	p07 := dto.Points[6]
	assert.Equal(t, "P07", p07.PointID)
	assert.InDelta(t, 20.0, p07.EffectiveUpperN, 1e-9, "生效值后端合成")
	assert.InDelta(t, 5.0, p07.EffectiveLowerN, 1e-9)

	assert.InDelta(t, 50.0, dto.Points[19].EffectiveUpperN, 1e-9)
	assert.InDelta(t, 45, dto.GlobalRules.DeviceOfflineMinutes, 1e-9, "T257 12.4：读的是 threshold_wear_interrupt_minutes")
	assert.InDelta(t, 18, dto.GlobalRules.DailyWearMinHours, 1e-9)
}

func TestT252_GetAlertRules_StoreErrorsReturn500(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configsErr = errors.New("db down")
	w, resp := e.do(http.MethodGet, t252RulesPath, nil, t252AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)

	e2 := newEnv(t, true, true)
	e2.store.pointRulesErr = errors.New("db down")
	w2, _ := e2.do(http.MethodGet, t252RulesPath, nil, t252AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w2.Code)
	assert.Nil(t, e2.store.savedKVs)
}

// ── PUT /points 保存规则 ──

func TestT252_UpdateAlertPointRules_WritesUnifiedKVAndIncrement(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyPressureHigh: "45", keyWearInterrupt: "30"}
	monitored := false
	upper := 20.0
	lower := 5.0

	body := map[string]any{
		"unifiedUpperN": 50,
		"points": []map[string]any{
			{"pointId": "P01", "monitored": monitored},
			{"pointId": "P07", "upperN": upper, "lowerN": lower},
			{"pointId": "P08", "upperN": nil}, // 显式 null = 清除独立阈值
		},
	}
	w, resp := e.do(http.MethodPut, t252PointsPath, body, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	// 统一上限写回既有键；未给出的 unifiedLowerN 不写（不覆盖现值）
	require.Len(t, e.store.savedKVs, 1)
	v, ok := kvOf(e.store.savedKVs, keyPressureHigh)
	require.True(t, ok)
	assert.Equal(t, "50", v)

	require.Len(t, e.store.savedRules, 3)
	p01, ok := ruleFor(e.store.savedRules, "P01")
	require.True(t, ok)
	assert.False(t, p01.Monitored)
	assert.Nil(t, p01.UpperN, "monitored-only 行不带独立阈值")

	p07, ok := ruleFor(e.store.savedRules, "P07")
	require.True(t, ok)
	assert.True(t, p07.Monitored, "monitored 缺省 = 不改勾选，保持默认勾选")
	require.NotNil(t, p07.UpperN)
	assert.Equal(t, 20.0, *p07.UpperN)

	p08, ok := ruleFor(e.store.savedRules, "P08")
	require.True(t, ok)
	assert.Nil(t, p08.UpperN, "显式 null 清除独立阈值")

	// 响应回完整聚合视图（恒 20 条）
	assert.Len(t, decodeRules(t, resp.Data).Points, alertPointCount)

	// 审计一条 config_change，detail 带变更点位清单
	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Equal(t, "config_change", row.Action)
	assert.Equal(t, "alert_rule", row.TargetType)
	assert.Equal(t, "points", row.TargetID)
	assert.Equal(t, "A0001", row.OperatorID)
	assert.Equal(t, "ROLE_ADMIN", row.OperatorRole)
	assert.ElementsMatch(t, []string{"P01", "P07", "P08"}, row.Detail["pointIds"])
	assert.Contains(t, row.Description, "逐点变更 3 个")
}

func TestT252_UpdateAlertPointRules_RejectsBadInputBeforeWriting(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		msg  string
	}{
		{"点位编号越界", map[string]any{"points": []map[string]any{{"pointId": "P21", "upperN": 20}}}, "P01-P20"},
		{"点位编号非零填充", map[string]any{"points": []map[string]any{{"pointId": "P3", "upperN": 20}}}, "P01-P20"},
		{"同一点位重复", map[string]any{"points": []map[string]any{
			{"pointId": "P02", "upperN": 20}, {"pointId": "P02", "upperN": 25}}}, "duplicate"},
		{"独立上限量程", map[string]any{"points": []map[string]any{{"pointId": "P02", "upperN": 300}}}, "upperN"},
		{"独立下限为负", map[string]any{"points": []map[string]any{{"pointId": "P02", "lowerN": -1}}}, "lowerN"},
		{"独立上限不高于下限", map[string]any{"points": []map[string]any{{"pointId": "P02", "upperN": 10, "lowerN": 20}}}, "effective upper"},
		// P02 未给 upperN ⇒ 与统一上限 50 合成后仍要自洽（契约「含与统一值合成后」）
		{"统一上下限颠倒", map[string]any{"unifiedUpperN": 8, "unifiedLowerN": 10}, "unifiedUpperN"},
		{"统一上限超量程", map[string]any{"unifiedUpperN": 500}, "unifiedUpperN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			e.store.configs = map[string]string{keyPressureHigh: "45"}
			w, resp := e.do(http.MethodPut, t252PointsPath, tc.body, t252AdminHdr())
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, model.CodeInvalidParam, resp.Code)
			assert.Contains(t, resp.Message, tc.msg)
			assert.Nil(t, e.store.savedKVs, "拒绝时不得触达写通道")
			assert.Nil(t, e.store.savedRules)
			assert.Empty(t, e.store.auditRows, "失败请求不留配置变更审计")
		})
	}
}

func TestT252_UpdateAlertPointRules_InheritsCurrentValueForUnlistedPoints(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyPressureHigh: "45"}
	upper := 20.0
	e.store.pointRules = []repo.AlertPointRuleRow{{PointID: "P03", Monitored: true, UpperN: &upper}}

	// 只提交 P04 的勾选变更 + 新的独立阈值：P03 的 20N 不能被抹掉
	low := 15.0
	body := map[string]any{"points": []map[string]any{{"pointId": "P04", "monitored": false, "upperN": low}}}
	w, resp := e.do(http.MethodPut, t252PointsPath, body, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code)

	require.Len(t, e.store.savedRules, 1)
	assert.Equal(t, "P04", e.store.savedRules[0].PointID)
	assert.False(t, e.store.savedRules[0].Monitored)
	assert.Nil(t, e.store.savedKVs, "未给统一上下限时不写 KV")

	dto := decodeRules(t, resp.Data)
	assert.Equal(t, 45.0, dto.UnifiedUpperN, "统一上限未提交则保持现值")
	assert.InDelta(t, 20.0, dto.Points[2].EffectiveUpperN, 1e-9, "未列出的点位保持原值")
}

func TestT252_UpdateAlertPointRules_SaveFailureReturns500AndNoAudit(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.saveRulesErr = errors.New("db")
	w, resp := e.do(http.MethodPut, t252PointsPath, map[string]any{"unifiedUpperN": 50}, t252AdminHdr())
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, model.CodeInternal, resp.Code)
	assert.Empty(t, e.store.auditRows, "写库失败不落成功审计（避免日志与实际配置不一致）")
}

func TestT252_UpdateAlertPointRules_AuditFailureDoesNotBlockSave(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.auditErr = errors.New("audit table unavailable")
	w, resp := e.do(http.MethodPut, t252PointsPath, map[string]any{"unifiedUpperN": 50}, t252AdminHdr())
	assert.Equal(t, http.StatusOK, w.Code, "留痕失败不应让配置保存整体失败: %s", resp.Message)
	require.Len(t, e.store.savedKVs, 1)
}

// ── POST /points/reset 恢复默认 ──

func TestT252_ResetAlertPointRules(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyPressureHigh: "60", keyWearInterrupt: "90"}

	w, resp := e.do(http.MethodPost, t252PointsResetPath, nil, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.True(t, e.store.resetCalled)

	require.Len(t, e.store.resetKVs, 2)
	v, ok := kvOf(e.store.resetKVs, keyPressureHigh)
	require.True(t, ok)
	assert.Equal(t, "5", v, "上限回 T203 ÷10 后默认")
	v, ok = kvOf(e.store.resetKVs, keyPressureLow)
	require.True(t, ok)
	assert.Equal(t, "1", v, "T287：恢复默认写回的下限必须 = 迁移 000021 / seed 的 1，不能把库里正确值抹成 0.5")

	require.Len(t, e.store.auditRows, 1)
	assert.Equal(t, "config_change", e.store.auditRows[0].Action)
	assert.Contains(t, e.store.auditRows[0].Description, "恢复默认")

	dto := decodeRules(t, resp.Data)
	assert.Len(t, dto.Points, alertPointCount, "响应回恢复后的聚合视图")
	assert.InDelta(t, 90, dto.GlobalRules.DeviceOfflineMinutes, 1e-9, "恢复默认只作用于规则卡，不动全局规则")
}

// ── PUT /global 全局告警规则 ──

func TestT252_UpdateAlertGlobalRules_PartialWrite(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyPressureHigh: "45", keyWearInterrupt: "30", keyWearTarget: "22"}

	body := map[string]any{"reportTimeoutMinutes": 8, "dailyWearMinHours": 18}
	w, resp := e.do(http.MethodPut, t252GlobalPath, body, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.savedKVs, 2, "只写提交的两项，其余保持现值")
	v, ok := kvOf(e.store.savedKVs, keyReportTimeoutMinutes)
	require.True(t, ok)
	assert.Equal(t, "8", v)
	v, ok = kvOf(e.store.savedKVs, keyWearTarget)
	require.True(t, ok)
	assert.Equal(t, "18", v, "佩戴时长下限与 §7D.12 dailyWearTargetHours 同键")
	assert.Nil(t, e.store.savedRules, "全局规则端点不写逐点表")

	require.Len(t, e.store.auditRows, 1)
	assert.Equal(t, "global", e.store.auditRows[0].TargetID)

	dto := decodeRules(t, resp.Data)
	assert.InDelta(t, 30, dto.GlobalRules.DeviceOfflineMinutes, 1e-9)
	assert.InDelta(t, 45, dto.UnifiedUpperN, 1e-9, "统一上下限不在本端点范围")
}

func TestT252_UpdateAlertGlobalRules_RejectsOutOfRangeAndEmpty(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		msg  string
	}{
		{"离线阈值超范围", map[string]any{"deviceOfflineMinutes": 0}, "deviceOfflineMinutes"},
		{"连续佩戴超 24h", map[string]any{"continuousWearMaxHours": 30}, "continuousWearMaxHours"},
		{"上报超范围", map[string]any{"reportTimeoutMinutes": 2000}, "reportTimeoutMinutes"},
		{"空提交", map[string]any{}, "no global rule field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			w, resp := e.do(http.MethodPut, t252GlobalPath, tc.body, t252AdminHdr())
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, resp.Message, tc.msg)
			assert.Nil(t, e.store.savedKVs)
		})
	}
}

// ── T257 12.4：设备离线阈值与 §7D.12 佩戴中断阈值合键 ──

func TestT257_GlobalRules_DeviceOfflineWritesWearInterruptKey(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	w, resp := e.do(http.MethodPut, t252GlobalPath, map[string]any{"deviceOfflineMinutes": 90}, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	require.Len(t, e.store.savedKVs, 1, "只改这一项就只写一个键")
	assert.Equal(t, keyWearInterrupt, e.store.savedKVs[0].Key, "设备离线 ≡ 佩戴中断阈值，不建第三键")
	assert.Equal(t, "90", e.store.savedKVs[0].Value)
	_, hasOld := kvOf(e.store.savedKVs, "device_offline_minutes")
	assert.False(t, hasOld, "旧键不再被写（库里的历史行保留，只是没人读）")

	dto := decodeRules(t, resp.Data)
	assert.Len(t, dto.Points, alertPointCount, "响应仍回完整聚合视图")

	require.Len(t, e.store.auditRows, 1)
	assert.Equal(t, "global", e.store.auditRows[0].TargetID)
	assert.Contains(t, e.store.auditRows[0].Description, "设备离线 90 分钟")
}

func TestT257_GlobalRules_DeviceOfflineHonoursCollectIntervalLinkage(t *testing.T) {
	// alert-service ValidateThresholds 要求中断阈值 ≥ 2×采集间隔；
	// 本页若放行 50，落库后 alert-service 会拒载整份配置（引擎静默保持旧值）
	e := newEnv(t, true, true)
	e.store.configs = map[string]string{keyCollectInterval: "30"}

	w, resp := e.do(http.MethodPut, t252GlobalPath, map[string]any{"deviceOfflineMinutes": 50}, t252AdminHdr())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp.Message, "2x collect interval")
	assert.Nil(t, e.store.savedKVs, "拒绝时不得触达写通道")
	assert.Empty(t, e.store.auditRows)

	w, resp = e.do(http.MethodPut, t252GlobalPath, map[string]any{"deviceOfflineMinutes": 60}, t252AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, "60", e.store.savedKVs[0].Value, "恰好 2× 采集间隔应放行")
}

func TestT257_GlobalRules_DeviceOfflineRangeMatchesSettingsPage(t *testing.T) {
	// §7D.12 的量程是 [10,720]；合键后本页不能写出 settings 页会拒绝的值
	for _, v := range []int{9, 721} {
		e := newEnv(t, true, true)
		w, resp := e.do(http.MethodPut, t252GlobalPath, map[string]any{"deviceOfflineMinutes": v}, t252AdminHdr())
		assert.Equal(t, http.StatusBadRequest, w.Code, "deviceOfflineMinutes=%d", v)
		assert.Contains(t, resp.Message, "deviceOfflineMinutes")
		assert.Nil(t, e.store.savedKVs)
	}
}
