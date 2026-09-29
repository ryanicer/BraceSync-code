// T477 患者登录口令（admin 通道设密）响应契约
package model

// PatientPasswordSetDTO 设密成功响应：新口令只在本响应出现一次，同 T314 医护口径。
//
// 🔴 库里存 bcrypt 哈希，服务端不留明文、之后任何端点都取不回来；口令遗失走本端点重设。
// 消费方是患者端 CI 自动化登录（T462 S4 链 C）：自建患者 → admin 设密 → 拿本响应里的口令登录。
type PatientPasswordSetDTO struct {
	PatientID string `json:"patientId"`
	Password  string `json:"password"`
}
