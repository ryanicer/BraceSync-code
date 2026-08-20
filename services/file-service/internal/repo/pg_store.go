// Package repo — file-service 文件元数据仓储（T022，表 owner：file-service）
//
// files 表写入闭环（对齐任务需求 2）：
//   - CreateFile：签发预签名 URL 时登记 pending 行（幂等：ON CONFLICT file_id）
//   - MarkUploaded：upload-complete 回调置 uploaded + uploaded_at/size/url（幂等）
//   - QueryFiles/CountFiles：按 owner/type/status 过滤分页查询（total 为全量计数）
package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
)

// ErrNotFound 文件元数据不存在
var ErrNotFound = errors.New("file metadata not found")

// Store 文件元数据仓储接口
type Store interface {
	// CreateFile 登记文件元数据（幂等：同 file_id 重复登记不产生重复行）
	CreateFile(ctx context.Context, fm *model.FileMetadata) error
	// MarkUploaded 上传完成闭环：置 status=uploaded、写 uploaded_at/size/url；
	// 幂等：重复回调不重复更新 uploaded_at（COALESCE 保留首次完成时刻）
	MarkUploaded(ctx context.Context, fileID, publicURL string, size int64) error
	// GetFileByFileID 按 ID 查询元数据；不存在返回 ErrNotFound
	GetFileByFileID(ctx context.Context, fileID string) (*model.FileMetadata, error)
	// QueryFiles 按过滤条件分页查询（created_at 倒序）
	QueryFiles(ctx context.Context, filters QueryFilter) ([]model.FileMetadata, error)
	// CountFiles 过滤条件下的总行数（分页 total，不受 page/pageSize 影响）
	CountFiles(ctx context.Context, filters QueryFilter) (int64, error)
}

// QueryFilter 查询过滤条件（空值不过滤）
type QueryFilter struct {
	OwnerType string
	OwnerID   string
	FileType  model.FileType
	Status    model.FileStatus
	Page      int
	PageSize  int
}

// PGStore PostgreSQL 仓储实现（pgxpool，对齐仓库其他服务）
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore 构造 PGStore
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

const fileColumns = `file_id, bucket, object_key, url, file_type, owner_type, owner_id,
	size, content_type, status, uploaded_at, created_at, updated_at`

// CreateFile 登记元数据（幂等）：INSERT ON CONFLICT(file_id) 只回填缺失字段。
// 重复登记（同 file_id）不覆盖已有上传进度（uploaded_at/url 取已有值）。
func (s *PGStore) CreateFile(ctx context.Context, fm *model.FileMetadata) error {
	query := `
		INSERT INTO files (` + fileColumns + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now(),now())
		ON CONFLICT (file_id) DO UPDATE SET
			updated_at = now()`
	_, err := s.pool.Exec(ctx, query,
		fm.FileID, fm.Bucket, fm.ObjectKey, fm.URL, string(fm.FileType),
		fm.OwnerType, fm.OwnerID, fm.Size, fm.ContentType, string(fm.Status), fm.UploadedAt,
	)
	return err
}

// MarkUploaded 上传完成登记（需求 2 闭环）：
// status pending→uploaded + uploaded_at（仅首次写入）+ size + url（非空才覆盖）。
// 文件不存在返回 ErrNotFound；重复回调幂等（uploaded_at 不漂移）。
func (s *PGStore) MarkUploaded(ctx context.Context, fileID, publicURL string, size int64) error {
	query := `
		UPDATE files SET
			status = 'uploaded',
			uploaded_at = COALESCE(uploaded_at, now()),
			size = $2,
			url = CASE WHEN $3 <> '' THEN $3 ELSE url END,
			updated_at = now()
		WHERE file_id = $1`
	tag, err := s.pool.Exec(ctx, query, fileID, size, publicURL)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetFileByFileID 按 ID 查询元数据
func (s *PGStore) GetFileByFileID(ctx context.Context, fileID string) (*model.FileMetadata, error) {
	query := `SELECT ` + fileColumns + ` FROM files WHERE file_id = $1`

	var fm model.FileMetadata
	err := s.pool.QueryRow(ctx, query, fileID).Scan(
		&fm.FileID, &fm.Bucket, &fm.ObjectKey, &fm.URL, &fm.FileType,
		&fm.OwnerType, &fm.OwnerID, &fm.Size, &fm.ContentType, &fm.Status,
		&fm.UploadedAt, &fm.CreatedAt, &fm.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &fm, nil
}

// buildFilterSQL 拼接过滤子句与参数（QueryFiles/CountFiles 共用，保证口径一致）；
// 占位符序号由 len(args) 推导，无需独立计数器
func buildFilterSQL(filters QueryFilter) (string, []interface{}) {
	where := ""
	args := []interface{}{}

	if filters.OwnerType != "" {
		where += fmt.Sprintf(" AND owner_type = $%d", len(args)+1)
		args = append(args, filters.OwnerType)
	}
	if filters.OwnerID != "" {
		where += fmt.Sprintf(" AND owner_id = $%d", len(args)+1)
		args = append(args, filters.OwnerID)
	}
	if filters.FileType != "" {
		where += fmt.Sprintf(" AND file_type = $%d", len(args)+1)
		args = append(args, string(filters.FileType))
	}
	if filters.Status != "" {
		where += fmt.Sprintf(" AND status = $%d", len(args)+1)
		args = append(args, string(filters.Status))
	}
	return where, args
}

// normalizePaging 分页参数归一（page 1 起，pageSize 默认 20 上限 100，对齐架构 §3.5）
func normalizePaging(filters QueryFilter) (page, pageSize int) {
	page = filters.Page
	if page <= 0 {
		page = 1
	}
	pageSize = filters.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

// QueryFiles 过滤 + 分页查询（created_at 倒序）
func (s *PGStore) QueryFiles(ctx context.Context, filters QueryFilter) ([]model.FileMetadata, error) {
	where, args := buildFilterSQL(filters)
	page, pageSize := normalizePaging(filters)

	query := `SELECT ` + fileColumns + ` FROM files WHERE 1=1` + where +
		fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]model.FileMetadata, 0, pageSize)
	for rows.Next() {
		var fm model.FileMetadata
		if err := rows.Scan(
			&fm.FileID, &fm.Bucket, &fm.ObjectKey, &fm.URL, &fm.FileType,
			&fm.OwnerType, &fm.OwnerID, &fm.Size, &fm.ContentType, &fm.Status,
			&fm.UploadedAt, &fm.CreatedAt, &fm.UpdatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, fm)
	}
	return results, rows.Err()
}

// CountFiles 过滤条件下的总行数（分页 total 取总数而非当前页条数）
func (s *PGStore) CountFiles(ctx context.Context, filters QueryFilter) (int64, error) {
	where, args := buildFilterSQL(filters)
	query := `SELECT count(*) FROM files WHERE 1=1` + where

	var total int64
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}
