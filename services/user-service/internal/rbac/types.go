// Package rbac — RBAC 角色权限与数据范围过滤（测试先行/TDD 桩）
//
// TDD 说明（T002）：
//
//	本文件提供 RBAC 数据范围过滤的最小接口桩，**不实现业务逻辑**（后续 T003 Winner 实现）。
//	测试用例 src: docs/ §3.3（R1-R6）
//
// 角色与数据范围：
//
//	ROLE_ADMIN  → scope:all，无团队隔离
//	ROLE_DOCTOR → scope:team，仅本团队患者
//	ROLE_CS     → scope:patients，全量患者但仅沟通域
package rbac

// Role 角色定义
type Role struct {
	RoleID      string
	Name        string
	Permissions Permissions
}

// Permissions 权限集
type Permissions struct {
	Scope   string // all / team / patients
	Modules []string
}

// UserContext 用户上下文（从 JWT/网关注入）
type UserContext struct {
	UserID  string
	RoleID  string
	TeamID  string // 仅 ROLE_DOCTOR 有
	AdminID string
}

// DataScope 数据可见范围
type DataScope struct {
	TeamFilter    string   // SQL WHERE team_id = ?
	PatientFilter string   // SQL WHERE patient_id IN (...)
	ModuleFilter  []string // 允许访问的模块
}

// Service 实现后应做的角色判断
const (
	RoleAdmin  = "ROLE_ADMIN"
	RoleDoctor = "ROLE_DOCTOR"
	RoleCS     = "ROLE_CS"
)

// PatientRecord 患者记录（最小定义）
type PatientRecord struct {
	PatientID    string
	Name         string
	TeamID       string
	PrimaryDocID string
	Status       string
}

// RBACFilter RBAC 数据过滤器（桩）
// TODO(T003): Winner 实现 SQL WHERE 拼接逻辑
type RBACFilter struct{}

// BuildPatientScope 根据用户角色返回患者数据可见范围
// TODO(T003): Winner 实现
func (f *RBACFilter) BuildPatientScope(ctx UserContext) *DataScope {
	return nil
}

// CanAccessModule 检查角色是否可访问指定模块
// TODO(T003): Winner 实现
func (f *RBACFilter) CanAccessModule(ctx UserContext, module string) bool {
	return false
}

// CanViewPatient 检查医生是否可以查看指定患者
// TODO(T003): Winner 实现
func (f *RBACFilter) CanViewPatient(ctx UserContext, patient PatientRecord) bool {
	return false
}
