// Package handler T418：安装记录详情补三列展示值（patientName / techName / model）
//
// 卡面口径：GET /api/v1/install-records/:id 的 installDetailDTO 此前只回 ID 不回姓名，
// 而契约 InstallRecordDetail 按 extends InstallRecordRow 声明了这两列 ⇒ 对拍门禁靠 ignore
// 压着；model 是 T312 I-3 登记的「设计稿 安装记录.html:177 设备型号行」数据源。
// 本文件锁两件事：
//  1. LEFT JOIN 命中 ⇒ 三列真值在线上 JSON 里可见（不是前端从列表行兜底来的）；
//  2. LEFT JOIN 未命中 ⇒ 三列是 null 且键恒在（补 omitempty 会让键消失，前端按
//     「一定有这个键」写就拿到 undefined —— 对拍 C3 的同一条形状，这里用原文断言锁住）。
//
// 🔴 第 2 条只在夹具层面可达：install_records 的三个 *_id 列都是 NOT NULL + FK
// （scripts/db/migrations/000001_init_schema.up.sql:120-132），真库造不出「关联行缺失」的
// 安装记录 ⇒ 真库腿见 repo/integration_t418_test.go，只测第 1 条与 FK 保证的非空。
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// t418DetailRaw 取详情响应 data 的原文键值表。
// 键存在性必须按原文判：DTO 反序列化后「键缺失」与「值为 null」都是 nil 指针，分不出来。
func t418DetailRaw(t *testing.T, env *testEnv, installID string) map[string]json.RawMessage {
	t.Helper()
	status, resp := env.do(t, http.MethodGet, "/api/v1/install-records/"+installID, nil, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, model.CodeOK, resp.Code)
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.Data, &obj), "data=%s", string(resp.Data))
	return obj
}

func TestT418DetailReturnsJoinColumns(t *testing.T) {
	env := newTestEnv(t)
	id := createInstallForCalib(t, env, "DEV-T418-A", "P-T418-A", "TECH-T418-A")

	nid, err := strconv.ParseInt(id, 10, 64)
	require.NoError(t, err)
	env.store.AddInstallJoinNames(nid, "患者小甲", "技师小李")

	detail := getInstallDetail(t, env, id)
	require.NotNil(t, detail.PatientName)
	assert.Equal(t, "患者小甲", *detail.PatientName)
	require.NotNil(t, detail.TechName)
	assert.Equal(t, "技师小李", *detail.TechName)
	// model 由夹具按 PGStore 的 LEFT JOIN devices 从已注册设备行推导（register 落 DefaultModel）
	require.NotNil(t, detail.Model)
	assert.Equal(t, model.DefaultModel, *detail.Model)

	raw := t418DetailRaw(t, env, id)
	assert.Equal(t, `"患者小甲"`, string(raw["patientName"]))
	assert.Equal(t, `"技师小李"`, string(raw["techName"]))
	assert.Equal(t, `"`+model.DefaultModel+`"`, string(raw["model"]))
}

// TestT418DetailNullNotOmitted 关联行缺失腿：三列回 null 而不是被 omitempty 吞掉。
// 姓名两列不做注入、设备行从未注册 ⇒ 三列皆 nil。
func TestT418DetailNullNotOmitted(t *testing.T) {
	env := newTestEnv(t)
	installID, err := env.store.CreateInstall(context.Background(), &model.InstallRecord{
		DeviceID:      "DEV-T418-MISSING", // 该设备从未注册 ⇒ LEFT JOIN devices 未命中
		PatientID:     "P-T418-B",
		TechID:        "TECH-T418-B",
		CalibrateTime: time.Now().UTC(),
		WifiStatus:    "unconfigured",
	})
	require.NoError(t, err)

	detail := getInstallDetail(t, env, strconv.FormatInt(installID, 10))
	assert.Nil(t, detail.PatientName)
	assert.Nil(t, detail.TechName)
	assert.Nil(t, detail.Model)

	raw := t418DetailRaw(t, env, strconv.FormatInt(installID, 10))
	for _, k := range []string{"patientName", "techName", "model"} {
		v, ok := raw[k]
		require.True(t, ok, "详情响应缺键 %s —— 加 omitempty 会让按「键恒在」写的前端拿到 undefined", k)
		assert.Equal(t, "null", string(v), "键 %s 必须是 null，不能伪造空串", k)
	}
}
