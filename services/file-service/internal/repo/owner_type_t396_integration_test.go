//go:build integration
// +build integration

// T396：迁移 000030 给 files.owner_type 加的 CHECK 在真库（PG15）里的语义。
//
// 与单测的分工：
//   - model/owner_type_t396_test.go 证「Go 枚举 == 迁移文本里的值集」（无需 Docker）；
//   - handler/owner_type_t396_test.go 证「presign 对集合外取值回 400 且零写」（内存桩）；
//   - 本文件只证单测证不了的那一半：约束真在库里、真按名拒、四个真实写入方的取值真进得来。
//     内存桩不会执行 CHECK，「我以为 SQL 写对了」在这里会被 23514 原文打回。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
)

// t396File 只动 owner_type 一条变量的 files 行（其余列与本包既有用例同形）
func t396File(id, ownerType string) *model.FileMetadata {
	return &model.FileMetadata{
		FileID:      id,
		Bucket:      "bracesync-it",
		ObjectKey:   "t396/" + id + ".png",
		FileType:    model.FileTypeCommPhoto,
		OwnerType:   ownerType,
		OwnerID:     "T396-OWNER",
		ContentType: "image/png",
		Status:      model.FileStatusPending,
	}
}

const t396Constraint = "files_owner_type_check"

// TestITT396OwnerTypeCheckExistsInCatalog 约束存在性：防「迁移编号被跳过 / 约束改名」后
// 只剩应用层一道门而无人报警。
func TestITT396OwnerTypeCheckExistsInCatalog(t *testing.T) {
	ctx := context.Background()
	var name string
	err := itPool.QueryRow(ctx, `
SELECT conname FROM pg_constraint
 WHERE conrelid = 'files'::regclass AND contype = 'c' AND conname = $1`, t396Constraint).Scan(&name)
	require.NoError(t, err, "库里找不到 %s（迁移 000030 没跑 up，或约束名漂移）", t396Constraint)
	assert.Equal(t, t396Constraint, name)
}

// TestITT396OwnerTypeCheckRejectsDirtyValues 集合外取值逐条必拒，且报的是本约束（不是别的约束/类型错）
func TestITT396OwnerTypeCheckRejectsDirtyValues(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)

	t.Cleanup(func() {
		_, _ = itPool.Exec(context.Background(), `DELETE FROM files WHERE file_id LIKE 'F-IT-T396-BAD-%'`)
	})

	// 前三条是 T391 取证里 staging 真实脏过/会被写出的形态（大小写错、单数、纯未知串）
	for i, ot := range []string{"Patient", "PATIENT", "review", "unknown_type", "patient ", ""} {
		id := fmt.Sprintf("F-IT-T396-BAD-%d", i)
		err := store.CreateFile(ctx, t396File(id, ot))
		require.Error(t, err, "owner_type=%q 竟写进了 files 表", ot)

		var pe *pgconn.PgError
		require.True(t, errors.As(err, &pe), "报错不是 PG 错误，无法归因到约束：%v", err)
		assert.Equal(t, "23514", pe.SQLState(), "check_violation 之外的错误码说明拦它的不是本约束：%s", pe.Message)
		assert.Equal(t, t396Constraint, pe.ConstraintName,
			"必须由 owner_type 的 CHECK 拦截（若被 uk_owner 或 NOT NULL 抢先，本收口等于没做）")

		var n int
		require.NoError(t, itPool.QueryRow(ctx, `SELECT COUNT(*) FROM files WHERE file_id = $1`, id).Scan(&n))
		assert.Zero(t, n, "被拒的 owner_type=%q 仍留了行：说明只报了错没回滚", ot)
	}
}

// TestITT396OwnerTypeCheckAcceptsRealWriters 反证：四个真实写入方的取值全部落得进去。
// 少一条通过 = 新约束打断了对应上传链路（卡面红线「否则会打断上传」）。
func TestITT396OwnerTypeCheckAcceptsRealWriters(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)

	t.Cleanup(func() {
		_, _ = itPool.Exec(context.Background(), `DELETE FROM files WHERE file_id LIKE 'F-IT-T396-OK-%'`)
	})

	for i, ot := range []string{
		model.OwnerTypePatient,
		model.OwnerTypeAlert,
		model.OwnerTypeReviewTemplate,
		model.OwnerTypeInstallRecord,
	} {
		id := fmt.Sprintf("F-IT-T396-OK-%d", i)
		require.NoError(t, store.CreateFile(ctx, t396File(id, ot)),
			"枚举内取值 %q 被 CHECK 拒了：值集与实际写入不一致", ot)

		var got string
		require.NoError(t, itPool.QueryRow(ctx, `SELECT owner_type FROM files WHERE file_id = $1`, id).Scan(&got))
		assert.Equal(t, ot, got, "入库值必须与写入方逐字相等（大小写敏感）")
	}
}
