// Package handler — T576 甲案：患者端档案出参下发 dailyWearTargetHours（同源 + 降级留痕）
//
// 承接 T563 裁定乙 + Peter 的 D3 三步法（comment 630/634）。派发单判据，本文件覆盖后端那一腿：
//  1. 真源是 sys_configs.wear_target_hours，不是前端硬编码 —— 先把库值配成 18，响应必须回 18；
//     回 22（默认）就说明这条链路压根没读配置，判据 1 当场失效；
//  2. 读不到（查询失败 / 键缺失 / 值非数）一律退 22，不回 0 也不回 null ——
//     降级值要「有据可查」，所以三档各断一条 Warn 留痕；
//  3. 改动面失控的防线：这枚字段只加在患者自助档案上，
//     后台患者列表/详情复用同一个 AdminPatientDTO，那两张面不许出现这一枚。
//
// 注牙实测见 PR 正文：把 wearTargetHours 改成恒回默认值再跑本文件，第 1 格必须转红。
package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const wearTargetJSONKey = "dailyWearTargetHours"

// captureProfileLog 把全局 logger 接到 buffer：handler 用 ctxLogger，
// zerolog.Ctx 为空时落 log.Logger（本文件用例都不开 t.Parallel，串行改写全局量安全）。
func captureProfileLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	original := log.Logger
	buf := &bytes.Buffer{}
	log.Logger = zerolog.New(buf).With().Timestamp().Logger()
	t.Cleanup(func() { log.Logger = original })
	return buf
}

// selfProfile 以本人身份读档案，data 解成 map（同时能断「键在场」与「是不是裸数值」）
func selfProfile(t *testing.T, e *testEnv, patientID string) map[string]any {
	t.Helper()
	w, resp := e.do(http.MethodGet, profilePath, nil, selfHdr(patientID, "patient"))
	require.Equal(t, http.StatusOK, w.Code, "本人档案应可读（状态面/越权面已由 t186 用例覆盖）")
	var m map[string]any
	require.NoError(t, json.Unmarshal(resp.Data, &m), "data 必须是扁平对象")
	return m
}

// TestT576_ProfileWearTarget_FromConfig 判据 1：有库值就取库值（配成 18 证明不是默认回声）。
func TestT576_ProfileWearTarget_FromConfig(t *testing.T) {
	e := newEnv(t, true, true)
	row := seedProfilePatient(e)
	e.store.configs = map[string]string{keyWearTarget: "18"}
	buf := captureProfileLog(t)

	m := selfProfile(t, e, row.PatientID)
	v, ok := m[wearTargetJSONKey]
	require.True(t, ok, "档案出参必须带 %s（小程序两页读的就是这一枚）", wearTargetJSONKey)
	n, isNum := v.(float64)
	require.True(t, isNum, "必须是 JSON number，不能是串或 null：%T", v)
	assert.Equal(t, 18.0, n, "值必须来自 sys_configs，不是兜底默认")
	assert.Equal(t, "P20260001", m["patientId"], "内嵌 AdminPatientDTO 后既有字段层级不许变")
	assert.Empty(t, buf.String(), "有值那一档不得报降级 Warn")
}

// TestT576_ProfileWearTarget_DegradeMatrix 判据 2：三档降级都回 22 且各留一条 Warn。
func TestT576_ProfileWearTarget_DegradeMatrix(t *testing.T) {
	cases := []struct {
		name    string
		configs map[string]string
		cfgErr  error
		needle  string
	}{
		{"键缺失", map[string]string{keyPressureHigh: "5"}, nil, "missing"},
		{"值非法", map[string]string{keyWearTarget: "abc"}, nil, "not a number"},
		{"查询失败", nil, errors.New("db down"), "unreadable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, true)
			row := seedProfilePatient(e)
			e.store.configs = tc.configs
			e.store.configsErr = tc.cfgErr
			buf := captureProfileLog(t)

			m := selfProfile(t, e, row.PatientID)
			v, ok := m[wearTargetJSONKey]
			require.True(t, ok, "降级时这一枚也不许缺席")
			n, isNum := v.(float64)
			require.True(t, isNum, "降级值也必须是 number（null 或串会让前端两页各退各的）：%T", v)
			assert.Equal(t, 22.0, n, "降级值必须逐字等于 22（派发单硬判据）")

			logs := buf.String()
			assert.Contains(t, logs, keyWearTarget, "降级日志要能定位到键")
			assert.Contains(t, logs, tc.needle, "降级必须留痕（有据可查），不许静默兜底")
			assert.Contains(t, logs, `"level":"warn"`, "降级走 Warn 档")
		})
	}
}

// TestT576_ProfileWearTarget_AdminFacesUnchanged 判据 3：后台列表/详情两张面不被污染。
func TestT576_ProfileWearTarget_AdminFacesUnchanged(t *testing.T) {
	e := newEnv(t, true, true)
	row := seedProfilePatient(e)
	e.store.patients = []repo.PatientRow{row}
	e.store.patientTotal = 1
	e.store.configs = map[string]string{keyWearTarget: "18"} // 即便配置有值，也不许从后台面漏出来
	buf := captureProfileLog(t)

	dw, detail := e.do(http.MethodGet, "/api/v1/admin/patients/"+row.PatientID, nil, nil)
	require.Equal(t, http.StatusOK, dw.Code, "后台详情这格只量字段面，状态面不是本卡改动引入的")
	require.Contains(t, string(detail.Data), "P20260001", "正对照：后台详情面确实出了这个患者（否则「不含新字段」是空面假绿）")
	var dto model.AdminPatientDTO
	require.NoError(t, json.Unmarshal(detail.Data, &dto))
	assert.Equal(t, "P20260001", dto.PatientID, "后台详情本体字段照常")
	assert.NotContains(t, string(detail.Data), wearTargetJSONKey,
		"后台患者详情不得出现 %s", wearTargetJSONKey)

	lw, list := e.do(http.MethodGet, "/api/v1/admin/patients", nil, nil)
	require.Equal(t, http.StatusOK, lw.Code, "后台列表状态面不是本卡改动引入的")
	require.Contains(t, string(list.Data), "P20260001", "正对照：列表面确实出了这一行")
	assert.NotContains(t, string(list.Data), wearTargetJSONKey,
		"后台患者列表不得出现 %s（列表每行携带一份系统配置值 = 改动面失控）", wearTargetJSONKey)
	assert.Empty(t, buf.String(), "后台两张面不触达佩戴目标读取，不该有降级日志")
}
