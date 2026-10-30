// T733（方案A）量纲边界：系统参数页 / 告警规则页的 N ↔ mN 换算
//
// 🔴 链路口径（依据卡 TAPD 1133005753001000520，Boss 2026-10-22 拍板方案A）：
//
//	存储 / 传输 / 聚合 / 告警评估 / 校准减除 —— 全部是 mN，链路上零换算；
//	N 只存在于 HTTP 展示响应层。
//
// sys_configs 压力量纲键（threshold_pressure_high / threshold_pressure_low /
// threshold_sensor_drift / threshold_calibration_offset）与 alert_point_rules
// 的 upper_n / lower_n 列自 T733 配套迁移起存 **mN**。
// 而本服务这两个端点服务的是 admin 前端的「系统参数」与「告警管理 Tab2」界面，
// 界面契约（model.SystemSettingsDTO / model.AlertRulesDTO 的 *N 字段）是 **N**。
// ⇒ 本文件就是那条唯一边界：GET 出参 ÷MnPerN，PUT/POST 落库 ×MnPerN。
//
// 换算分母唯一真源在 services/units（Go 侧唯一分母定义），
// data-service 的 model.MnPerN、alert-service 的 config.MnPerN 均为指向它的薄别名。
package handler

import "github.com/bracesync/bracesync/services/units"

// MnPerN 兼容别名，指向 services/units.MnPerN（T733 方案A 唯一分母）。
// 兼容别名：新代码请直接使用 units.MnPerN。
const MnPerN = units.MnPerN

// mnToN 存储空间(mN) → 展示空间(N)：只允许出现在 HTTP 出参组装处。
// 兼容包装：新代码请直接使用 units.MnToN；保留本包装只为既有调用点零改动。
func mnToN(mn float64) float64 { return units.MnToN(mn) }

// nToMn 展示空间(N) → 存储空间(mN)：只允许出现在写 sys_configs / 逐点表的落库处。
// 兼容包装：新代码请直接使用 units.NToMn；保留本包装只为既有调用点零改动。
func nToMn(n float64) float64 { return units.NToMn(n) }

// numOrMnToN 读 sys_configs 压力量纲键：库里存 mN，缺行/脏值回退**展示空间默认 defN**。
// （默认值在各处仍以 N 声明，与前端表单 min/max 同档，读侧在此换算，避免两处硬编码。）
func numOrMnToN(raw string, defN float64) float64 { return mnToN(numOr(raw, nToMn(defN))) }

// nToMnPtr / mnToNPtr 指针版：nil（= 未设置 / 清除独立阈值）原样传 nil，
// 让逐采集点 upperN(N) ↔ upper_n(mN) 在读写两侧都能一行换算。
// 兼容包装：新代码请直接使用 units.NStampToMn / units.MnStampToN；保留本包装只为既有调用点零改动。
func nToMnPtr(n *float64) *float64 { return units.NStampToMn(n) }

func mnToNPtr(mn *float64) *float64 { return units.MnStampToN(mn) }
