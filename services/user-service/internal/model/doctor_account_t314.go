// T314 医护账号管理（PRD §7D.10）响应契约
package model

// DoctorAccountCreateDTO 创建成功响应 = 整行档案 + 一次性初始密码。
//
// 🔴 initialPassword 只在本响应出现一次：库里存的是 bcrypt 哈希，服务端不留明文、
// 之后任何端点都取不回来（PRD（3）「页面不保留、不可二次查看」）。密码遗失走 reset-password。
type DoctorAccountCreateDTO struct {
	DoctorDTO
	InitialPassword string `json:"initialPassword"`
}

// DoctorAccountResetDTO 重置密码响应：同样一次性返回新密码，旧密码即时失效。
type DoctorAccountResetDTO struct {
	DoctorID string `json:"doctorId"`
	Username string `json:"username"`
	Password string `json:"password"`
}
