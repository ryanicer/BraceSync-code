// T480 技师登录口令（新建发号 + admin 通道重置）响应契约
package model

// TechnicianCreateDTO 新建技师成功响应 = 整行档案 + 一次性初始口令。
//
// 🔴 initialPassword 只在本响应出现一次：库里存 bcrypt 哈希，服务端不留明文，
// 之后任何端点都取不回来（与 T314 医护账号同口径：关闭弹窗即不可再看，遗失走 reset-password）。
// 技师没有独立登录账号列 —— 登录身份是手机号，账号位展示的是 techId（设计稿技师管理页口径）。
type TechnicianCreateDTO struct {
	TechnicianDTO
	InitialPassword string `json:"initialPassword"`
}

// TechnicianPasswordResetDTO 重置口令响应：新口令同样一次性返回，旧口令即时失效。
type TechnicianPasswordResetDTO struct {
	TechID   string `json:"techId"`
	Password string `json:"password"`
}
