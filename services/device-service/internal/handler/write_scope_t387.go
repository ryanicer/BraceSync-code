// T387：设备域写侧身份门禁（bind / rebind / unbind / wifi / 安装记录创建 / 安装元数据回填 / 基线保存）。
//
// 缺陷原貌：T378 只收了这一族的读侧。写侧七条端点在服务层零判定，而网关把它们登记在
// staffOnlyPatterns（医护 / 客服 / 技师 / 运营四角色齐放行，只有患者被挡），于是医护令牌
// 能把任意设备绑到任意患者名下、能解绑、能改写他人安装记录与校准基线 —— 写侧跨团队无拦截。
// 唯一例外是 PUT /install-records/:id：T190 已在网关收口为技师+管理员，但服务层同样零判定，
// 属「缺口未补」而非「重复收紧」（直连 device-service 即绕过网关）。
//
// 口径依据（设计层，非推断）：
//   - PRD §3.4 用户类型表：技师 = 设备安装、空载校准、保存基线、WiFi 配网、管理安装记录；
//   - PRD §7D.11 权限矩阵：📱 设备管理（§7D.5）与 📋 安装记录（§7D.9）两行医护均为「—」；
//   - 前端事实：全仓写端点调用方只有 tech-miniapp（技师 JWT），admin-web 的设备管理页只做
//     注册与读，医护与客服两端零调用页面（doctor 侧栏 7 项、cs 侧栏 1 项均无这两页）。
//
// 放行面按 PM 口径取「技师 + 运营管理员」allow-list：
//   - 医护一律 403，不进任何归属推导 —— 团队隔离在这里用不上，因为该身份根本不该做这件事；
//   - 技师与管理员走原路径，不受团队隔离（技师面是否收团队已挂待 Boss 裁决，裁前不动）；
//   - 患者与未知/缺失身份同样 403（fail-closed，形状同本服务 T193 的 requireDeviceBoundToCaller）。
//
// 判定排在方法体最前、先于 JSON 解析与任何仓储访问 ⇒ 拒绝路径零写，且设备号 / 安装记录号
// 的存在性不作为探测面（存在与不存在的设备给出逐字相同的 403）。
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// assertDeviceWriteRole 设备域写端点的身份门禁：放行返回 true，否则 403 已写出。
//
// action 只描述动作本身（供文案），不带 deviceId / installId / patientId —— 带着就等于
// 把「这个号在库里存不存在」变成可辨信息。
func (h *Handler) assertDeviceWriteRole(c *gin.Context, action string) bool {
	role := c.GetHeader(headerRole)
	if role == roleAdmin || role == roleTech {
		return true
	}
	fail(c, model.ErrForbidden("role %q may not %s", role, action))
	return false
}
