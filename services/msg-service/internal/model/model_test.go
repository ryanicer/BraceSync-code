// Package model — DTO 序列化口径测试（前端契约依赖的字段名与可空性）
package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T278-③：通知记录 DTO 必须输出 patientName（后台列表患者列），
// 且 join 未命中时显式输出 null，而不是整个键消失（前端据此回落 patientId）。
func TestNotificationRecordDTO_PatientName(t *testing.T) {
	name := "张阿姨"
	withName := (&NotificationRecord{
		RecordID: 12, PatientID: "P-001", PatientName: &name,
		Kind: KindAlert, Channel: ChannelWechat, Status: StatusSent,
		CreatedAt: time.Date(2026, 9, 21, 3, 4, 5, 0, time.UTC),
	}).ToDTO()
	raw, err := json.Marshal(withName)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"patientName":"张阿姨"`)

	noName := (&NotificationRecord{
		RecordID: 13, PatientID: "P-002",
		Kind: KindAlert, Channel: ChannelSMS, Status: StatusPending,
		CreatedAt: time.Date(2026, 9, 21, 3, 4, 5, 0, time.UTC),
	}).ToDTO()
	raw, err = json.Marshal(noName)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"patientName":null`, "键必须存在且为 null（不带 omitempty）")
	assert.Nil(t, noName.PatientName)
}
