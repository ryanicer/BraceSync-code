//go:build integration
// +build integration

// Package repo 集成测试：真实 PG15（testcontainers）验证 files 表 SQL 与闭环
//
// 对齐：T022 验收标准 2/6（元数据登记闭环 + 集成链路）：
//
//	CreateFile 幂等 / MarkUploaded 状态翻转与幂等 / GetFileByFileID 404 /
//	QueryFiles 过滤分页 / CountFiles 全量计数 / uk_owner 约束
//
// 运行：make test-integration（需 Docker）
package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
	"github.com/bracesync/bracesync/services/testhelper"
)

var itPool *pgxpool.Pool

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		return runIT(m, cfg.DBURL)
	})
}

// runIT 建连 + 迁移 + 执行用例（file-service 仅依赖 PG，不使用 Redis）
func runIT(m *testing.M, dbURL string) int {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "it: pgxpool: %v\n", err)
		return 1
	}
	itPool = pool
	defer pool.Close()

	applyMigrations(ctx)
	return m.Run()
}

// migrationsDir 定位 scripts/db/migrations（相对本文件 4 级上）
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "db", "migrations")
}

// applyMigrations 顺序执行全部 *.up.sql（000006_file_service 建 files 表）
func applyMigrations(ctx context.Context) {
	entries, err := os.ReadDir(migrationsDir())
	if err != nil {
		panic("it: read migrations dir: " + err.Error())
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		sqlBytes, readErr := os.ReadFile(filepath.Join(migrationsDir(), name))
		if readErr != nil {
			panic("it: read migration: " + readErr.Error())
		}
		if _, execErr := itPool.Exec(ctx, string(sqlBytes)); execErr != nil {
			panic(fmt.Sprintf("it: apply %s: %v", name, execErr))
		}
	}
}

func newTestFile(id string) *model.FileMetadata {
	return &model.FileMetadata{
		FileID:      id,
		Bucket:      "bracesync-it",
		ObjectKey:   "install_record/1/" + id + ".jpg",
		URL:         "",
		FileType:    model.FileTypeSignature,
		OwnerType:   "install_record",
		OwnerID:     "1",
		Size:        0,
		ContentType: "image/jpeg",
		Status:      model.FileStatusPending,
	}
}

// TestCreateFile_Idempotent 同 file_id 重复登记不产生重复行
func TestCreateFile_Idempotent(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)
	fm := newTestFile("file-it-idem")

	require.NoError(t, store.CreateFile(ctx, fm))
	require.NoError(t, store.CreateFile(ctx, fm), "重复登记必须幂等成功")

	// 行数验证：仅一行
	var count int
	err := itPool.QueryRow(ctx, `SELECT count(*) FROM files WHERE file_id = $1`, fm.FileID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestMarkUploaded_ClosedLoop pending → uploaded 闭环（status/uploaded_at/size/url）
func TestMarkUploaded_ClosedLoop(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)
	fm := newTestFile("file-it-upload")
	require.NoError(t, store.CreateFile(ctx, fm))

	require.NoError(t, store.MarkUploaded(ctx, fm.FileID, "https://cdn.example.com/x.jpg", 4096))

	got, err := store.GetFileByFileID(ctx, fm.FileID)
	require.NoError(t, err)
	assert.Equal(t, model.FileStatusUploaded, got.Status)
	require.NotNil(t, got.UploadedAt, "uploaded_at 必须写入")
	assert.WithinDuration(t, time.Now(), *got.UploadedAt, time.Minute)
	assert.Equal(t, int64(4096), got.Size)
	assert.Equal(t, "https://cdn.example.com/x.jpg", got.URL)

	// 幂等：重复回调 uploaded_at 不漂移
	firstAt := *got.UploadedAt
	require.NoError(t, store.MarkUploaded(ctx, fm.FileID, "https://cdn.example.com/y.jpg", 9999))
	got2, err := store.GetFileByFileID(ctx, fm.FileID)
	require.NoError(t, err)
	assert.True(t, got2.UploadedAt.Equal(firstAt), "重复回调 uploaded_at 不变")
}

// TestMarkUploaded_EmptyURL_KeepsExisting 空 url 不覆盖既有值
func TestMarkUploaded_EmptyURL_KeepsExisting(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)
	fm := newTestFile("file-it-emptyurl")
	fm.URL = "https://keep.example.com/a.jpg"
	require.NoError(t, store.CreateFile(ctx, fm))

	require.NoError(t, store.MarkUploaded(ctx, fm.FileID, "", 10))

	got, err := store.GetFileByFileID(ctx, fm.FileID)
	require.NoError(t, err)
	assert.Equal(t, "https://keep.example.com/a.jpg", got.URL, "空 url 不得覆盖既有值")
}

// TestMarkUploaded_NotFound 不存在的 file_id → ErrNotFound
func TestMarkUploaded_NotFound(t *testing.T) {
	store := NewPGStore(itPool)
	err := store.MarkUploaded(context.Background(), "file-it-ghost", "", 1)
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestGetFileByID_NotFound 无行 → ErrNotFound
func TestGetFileByID_NotFound(t *testing.T) {
	store := NewPGStore(itPool)
	_, err := store.GetFileByFileID(context.Background(), "file-it-none")
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestQueryFiles_FilterAndPaging 过滤 + 分页 + CountFiles 全量计数
func TestQueryFiles_FilterAndPaging(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)

	// 造 3 条同 owner 数据（独立 owner 避免与他例互相污染）
	for i := 1; i <= 3; i++ {
		fm := newTestFile(fmt.Sprintf("file-it-q-%d", i))
		fm.OwnerType = "patient"
		fm.OwnerID = "P-IT-QUERY"
		require.NoError(t, store.CreateFile(ctx, fm))
	}

	filters := QueryFilter{OwnerType: "patient", OwnerID: "P-IT-QUERY", Page: 1, PageSize: 2}
	files, err := store.QueryFiles(ctx, filters)
	require.NoError(t, err)
	assert.Len(t, files, 2, "pageSize=2 仅返回 2 条")

	total, err := store.CountFiles(ctx, filters)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total, "CountFiles 必须是过滤后总数而非当前页条数")

	// 翻页取剩余 1 条
	filters.Page = 2
	files2, err := store.QueryFiles(ctx, filters)
	require.NoError(t, err)
	assert.Len(t, files2, 1)

	// status 过滤
	filters = QueryFilter{OwnerType: "patient", OwnerID: "P-IT-QUERY", Status: model.FileStatusUploaded}
	total, err = store.CountFiles(ctx, filters)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
}

// TestUKOwnerConstraint 同 owner 同 object_key 唯一约束
func TestUKOwnerConstraint(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)

	fm1 := newTestFile("file-it-uk-1")
	require.NoError(t, store.CreateFile(ctx, fm1))

	fm2 := newTestFile("file-it-uk-2")
	fm2.ObjectKey = fm1.ObjectKey // 同 owner 同 key → 违反 uk_owner
	err := store.CreateFile(ctx, fm2)
	assert.Error(t, err, "uk_owner 必须拦截同 owner 同 object_key 重复行")
}

// TestCreateFile_TypeCheck file_type CHECK 约束
func TestCreateFile_TypeCheck(t *testing.T) {
	ctx := context.Background()
	store := NewPGStore(itPool)

	fm := newTestFile("file-it-badtype")
	fm.FileType = model.FileType("video_mp4") // 不在 CHECK 枚举内
	err := store.CreateFile(ctx, fm)
	assert.Error(t, err, "CHECK 约束必须拦截非法 file_type")
}
