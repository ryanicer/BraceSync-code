// T448 清除设备 WiFi 的审计留痕通路。
//
// 清除动作本身是纯 BLE（apps/tech-miniapp/src/utils/ble.ts 向 B513 写 0x02、等 B512 notify=0），
// 按 T446 §二 的定性它本就不该有 HTTP 端点，所以「无端点」不是漏做；本卡补的是它的零留痕。
// 技师端在清除成功后复用既有 POST /api/v1/devices/:deviceId/wifi（body 带 cleared=true）
// 上报一次，本文件把那一次落成一行 audit_logs。
//
// audit_logs 的 owner 是 user-service（T252），仓内没有跨服务写审计的 HTTP 通道，
// 而各服务连的是同一个库 ⇒ 这里同库直写。表在 000001_init_schema.up.sql:301-313，
// action / target_type 两列都无 CHECK，取值沿用 T252 词表，不新造动词。
package repo

import (
	"context"
	"encoding/json"
	"fmt"
)

// 审计动词与对象类型：data_modify 在 T252 的五值词表内（user-service handler/audit_t252.go:31
// 的 auditActionDataModify），admin-web 操作日志的筛选下拉也已收录该值
// （apps/admin-web/src/pages/settings/index.vue:189 的 el-option）。
const (
	auditActionDataModify    = "data_modify"
	auditTargetTypeDevice    = "device"
	wifiClearAuditDescriptor = "清除设备 WiFi（BLE 写 B513=0x02，配网凭据已在设备侧擦除）"
)

// WifiClearAuditInput 清除留痕入参。空字符串字段落 NULL（与 user-service WriteAuditLog 同口径）。
type WifiClearAuditInput struct {
	DeviceID     string
	OperatorID   string
	OperatorRole string
	IP           string
}

// WriteWifiClearAudit 写一条「谁在何时清了哪台设备的 WiFi」。
// 这条通路的全部产出就是这行记录，失败必须由调用方回错，不许静默吞掉
// （user-service 侧的 h.audit 是「附带动作所以只 WARN」，语义不同）。
func (r *PGStore) WriteWifiClearAudit(ctx context.Context, in WifiClearAuditInput) error {
	payload, err := json.Marshal(map[string]any{"description": wifiClearAuditDescriptor})
	if err != nil {
		return fmt.Errorf("marshal wifi clear audit detail: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO audit_logs (operator_id, operator_role, action, target_type, target_id, detail, ip)
		 VALUES (NULLIF($1, ''), NULLIF($2, ''), $3, NULLIF($4, ''), NULLIF($5, ''), $6::jsonb, NULLIF($7, ''))`,
		in.OperatorID, in.OperatorRole, auditActionDataModify, auditTargetTypeDevice, in.DeviceID, string(payload), in.IP)
	if err != nil {
		return fmt.Errorf("write wifi clear audit: %w", err)
	}
	return nil
}
