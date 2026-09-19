// T252 11.2 角色增删改（roles，owner: user-service）
//
// 🔴 表约束现况（000001:18-26）：role_id 是 PK，name 只有 NOT NULL、**无唯一索引**
//
//	⇒ 重名只能应用层查重（RoleNameTaken），不加索引是因为 seed/测试数据里可能已有同名行，
//	加唯一约束会让迁移在既有库上直接失败。并发窗口与是否收口已登记 T252 交件待裁。
package repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrRoleNotFound 角色 ID 不存在。handler 映射 404。
var ErrRoleNotFound = errors.New("role not found")

// ErrRoleInUse 角色仍被运营账号引用（admins.role_id 外键），不可删除。
// handler 据 MemberCount 拼装 409 文案，提示先转移成员。
type ErrRoleInUse struct{ MemberCount int }

func (e *ErrRoleInUse) Error() string {
	return fmt.Sprintf("role in use by %d admin account(s)", e.MemberCount)
}

// newRoleID 生成自定义角色 ID：ROLE_C + 10 位大写 hex（避开预置字面量 ROLE_ADMIN 等，
// 且不与 gateway RBAC 的既有角色常量相撞）。
func newRoleID() (string, error) {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ROLE_C" + strings.ToUpper(hex.EncodeToString(buf)), nil
}

// RoleNameTaken 角色名是否已被占用（excludeRoleID 排除自身；空串表示不排除）
//
// 🔴 唯一性只在应用层保证：roles.name 无唯一索引（000001:20 只有 NOT NULL），
//
//	并发创建同名角色存在窗口。根治要加唯一索引，但现库/测试种子可能已有同名行，
//	贸然加会让迁移在既有库上直接失败 ⇒ 已登记 T252 交件待裁，不自行改数据语义。
func (s *PGStore) RoleNameTaken(ctx context.Context, name, excludeRoleID string) (bool, error) {
	var taken bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM roles WHERE name = $1 AND role_id <> $2)`,
		name, excludeRoleID).Scan(&taken)
	return taken, err
}

// CreateRole 新建角色（重名由调用方先用 RoleNameTaken 拦 409）；成功回读完整行。
func (s *PGStore) CreateRole(ctx context.Context, name, description, permissionsJSON string) (*RoleRow, error) {
	roleID, err := newRoleID()
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO roles (role_id, name, description, permissions_json)
		 VALUES ($1, $2, NULLIF($3, ''), $4::jsonb)`,
		roleID, name, description, permissionsJSON); err != nil {
		return nil, err
	}
	return s.GetRole(ctx, roleID)
}

// UpdateRole 改角色名/描述/状态（nil 字段不改）；角色不存在 → ErrRoleNotFound。
func (s *PGStore) UpdateRole(ctx context.Context, roleID string, name, description, status *string) (*RoleRow, error) {
	// 动态 SET：仅拼已给出的列（列名白名单硬编码，值走占位符）
	var sets []string
	args := []any{roleID}
	if name != nil {
		args = append(args, *name)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
	}
	if description != nil {
		args = append(args, *description)
		sets = append(sets, fmt.Sprintf("description = NULLIF($%d, '')", len(args)))
	}
	if status != nil {
		args = append(args, *status)
		sets = append(sets, fmt.Sprintf("status = $%d", len(args)))
	}
	if len(sets) == 0 {
		return s.GetRole(ctx, roleID)
	}
	sql := "UPDATE roles SET " + strings.Join(sets, ", ") + " WHERE role_id = $1"
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrRoleNotFound
	}
	return s.GetRole(ctx, roleID)
}

// DeleteRole 删除角色。MemberCount>0 → ErrRoleInUse（先查后删；FK 兜底见下）。
func (s *PGStore) DeleteRole(ctx context.Context, roleID string) error {
	row, err := s.GetRole(ctx, roleID)
	if err != nil {
		return err
	}
	if row == nil {
		return ErrRoleNotFound
	}
	if row.MemberCount > 0 {
		return &ErrRoleInUse{MemberCount: row.MemberCount}
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM roles WHERE role_id = $1`, roleID)
	if err != nil {
		// 并发下刚被占用：admins.role_id 外键 23503 → 同口径 409
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return &ErrRoleInUse{MemberCount: 1}
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}
