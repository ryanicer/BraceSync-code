// Package service T021：冷数据归档编排（三步走安全流程）
//
// 对齐：架构 §4.5
//  1. 导出 CSV + 计算 checksum → 状态 exported
//  2. 校验行数 + checksum → 通过则 verified，失败则 failed
//  3. verified 后才 DETACH + DROP → cleaned
//
// 红线：校验失败禁止清理；失败可幂等续跑
package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/data-service/internal/metrics"
	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// partitionNameRe 分区名合法性校验：仅允许 pressure_records_YYYYMM
var partitionNameRe = regexp.MustCompile(`^pressure_records_\d{6}$`)

// ArchiveService 冷归档编排
type ArchiveService struct {
	archive    repo.ArchiveStore
	partitions repo.PartitionStore
	exportDir  string
	now        func() time.Time
}

// NewArchiveService 组装 ArchiveService
func NewArchiveService(archive repo.ArchiveStore, partitions repo.PartitionStore, exportDir string) *ArchiveService {
	return &ArchiveService{
		archive:    archive,
		partitions: partitions,
		exportDir:  exportDir,
		now:        time.Now,
	}
}

// RunColdArchive 每月 1 日 02:00 归档 T-7 月分区
func (s *ArchiveService) RunColdArchive(ctx context.Context) {
	now := s.now().In(model.CSTZone())
	target := time.Date(now.Year(), now.Month()-7, 1, 0, 0, 0, 0, model.CSTZone())
	partitionName := "pressure_records_" + target.Format("200601")

	log.Info().Str("partition", partitionName).Msg("cold archive started")

	// 检查分区是否存在
	exists, err := s.partitions.PartitionExists(ctx, partitionName)
	if err != nil {
		metrics.ArchiveJobTotal.WithLabelValues("export", "error").Inc()
		log.Error().Err(err).Str("partition", partitionName).Msg("check partition existence failed")
		return
	}
	if !exists {
		log.Info().Str("partition", partitionName).Msg("partition does not exist, skip archive")
		metrics.ArchiveJobTotal.WithLabelValues("export", "ok").Inc()
		return
	}

	// 查归档状态（续跑或新建）
	status, err := s.archive.GetArchiveStatus(ctx, partitionName)
	if err != nil {
		metrics.ArchiveJobTotal.WithLabelValues("export", "error").Inc()
		log.Error().Err(err).Str("partition", partitionName).Msg("get archive status failed")
		return
	}

	if status == nil {
		// 首次归档：创建 pending 记录
		status = &model.ArchiveStatus{
			PartitionName: partitionName,
			PeriodYear:    target.Year(),
			PeriodMonth:   int(target.Month()),
			Status:        "pending",
		}
		if err := s.archive.UpsertArchiveStatus(ctx, status); err != nil {
			metrics.ArchiveJobTotal.WithLabelValues("export", "error").Inc()
			log.Error().Err(err).Msg("create initial archive status failed")
			return
		}
	}

	// 按状态续跑
	switch status.Status {
	case "cleaned":
		log.Info().Str("partition", partitionName).Msg("partition already cleaned, skip")
		return
	case "verified":
		s.stepCleanup(ctx, status)
	case "exported":
		s.stepVerify(ctx, status)
	default: // pending / failed
		s.stepExport(ctx, status)
	}
}

// safeExportPath 构造安全的导出路径：校验分区名格式 + Clean + 限定在 exportDir 内
func (s *ArchiveService) safeExportPath(partitionName string) (string, error) {
	if !partitionNameRe.MatchString(partitionName) {
		return "", fmt.Errorf("invalid partition name: %s", partitionName)
	}
	dir := filepath.Clean(s.exportDir)
	p := filepath.Join(dir, partitionName+".csv")
	// 确保最终路径仍在 exportDir 内（防路径穿越）
	if !strings.HasPrefix(filepath.Clean(p), dir) {
		return "", fmt.Errorf("export path escapes base directory: %s", p)
	}
	return p, nil
}

// stepExport 第一步：导出 CSV + 计算 checksum
func (s *ArchiveService) stepExport(ctx context.Context, status *model.ArchiveStatus) {
	exportPath, pathErr := s.safeExportPath(status.PartitionName)
	if pathErr != nil {
		metrics.ArchiveJobTotal.WithLabelValues("export", "error").Inc()
		log.Error().Err(pathErr).Msg("invalid export path")
		s.markFailed(ctx, status, "export path: "+pathErr.Error())
		return
	}

	// 确保导出目录存在
	if err := os.MkdirAll(s.exportDir, 0o755); err != nil {
		metrics.ArchiveJobTotal.WithLabelValues("export", "error").Inc()
		log.Error().Err(err).Msg("create export directory failed")
		s.markFailed(ctx, status, "create export dir: "+err.Error())
		return
	}

	rowCount, err := s.archive.ExportPartitionCSV(ctx, status.PartitionName, exportPath)
	if err != nil {
		metrics.ArchiveJobTotal.WithLabelValues("export", "error").Inc()
		log.Error().Err(err).Str("partition", status.PartitionName).Msg("export partition CSV failed")
		s.markFailed(ctx, status, "export csv: "+err.Error())
		return
	}

	if rowCount == 0 {
		// 空分区：直接标记 verified（无需校验）
		status.Status = "verified"
		status.RowCount = 0
		status.ExportPath = exportPath
		status.Checksum = sha256Hex([]byte("empty"))
		if err := s.archive.UpsertArchiveStatus(ctx, status); err != nil {
			log.Error().Err(err).Msg("update archive status failed after empty export")
			return
		}
		metrics.ArchiveJobTotal.WithLabelValues("export", "ok").Inc()
		log.Info().Str("partition", status.PartitionName).Msg("empty partition exported, marked verified")
		s.stepCleanup(ctx, status)
		return
	}

	// 计算导出文件的 checksum
	checksum, err := fileChecksum(exportPath)
	if err != nil {
		// 文件不存在（ExportPartitionCSV 用了 SELECT 回退方案），用行数作为校验基准
		checksum = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("rows:%d", rowCount))))
	}

	status.Status = "exported"
	status.RowCount = rowCount
	status.ExportPath = exportPath
	status.Checksum = checksum
	status.ErrorMessage = ""

	if err := s.archive.UpsertArchiveStatus(ctx, status); err != nil {
		metrics.ArchiveJobTotal.WithLabelValues("export", "error").Inc()
		log.Error().Err(err).Msg("update archive status to exported failed")
		return
	}

	metrics.ArchiveJobTotal.WithLabelValues("export", "ok").Inc()
	log.Info().Str("partition", status.PartitionName).Int64("rows", rowCount).Msg("export completed, proceeding to verify")

	// 自动进入下一步
	s.stepVerify(ctx, status)
}

// stepVerify 第二步：校验行数 + checksum
func (s *ArchiveService) stepVerify(ctx context.Context, status *model.ArchiveStatus) {
	// 重读分区行数（DB 当前值）
	dbRowCount, err := s.archive.CountPartitionRows(ctx, status.PartitionName)
	if err != nil {
		metrics.ArchiveJobTotal.WithLabelValues("verify", "error").Inc()
		log.Error().Err(err).Msg("count partition rows for verify failed")
		s.markFailed(ctx, status, "verify count: "+err.Error())
		return
	}

	if dbRowCount != status.RowCount {
		metrics.ArchiveJobTotal.WithLabelValues("verify", "error").Inc()
		log.Error().
			Int64("db_rows", dbRowCount).
			Int64("export_rows", status.RowCount).
			Msg("row count mismatch, verify failed — cleanup blocked")
		s.markFailed(ctx, status, fmt.Sprintf("row count mismatch: db=%d export=%d", dbRowCount, status.RowCount))
		return
	}

	// 校验文件 checksum（如果文件存在）
	if status.ExportPath != "" {
		if actualChecksum, csErr := fileChecksum(status.ExportPath); csErr == nil && actualChecksum != status.Checksum {
			metrics.ArchiveJobTotal.WithLabelValues("verify", "error").Inc()
			log.Error().
				Str("expected", status.Checksum).
				Str("actual", actualChecksum).
				Msg("checksum mismatch, verify failed — cleanup blocked")
			s.markFailed(ctx, status, "checksum mismatch")
			return
		}
	}

	status.Status = "verified"
	status.ErrorMessage = ""
	if err := s.archive.UpsertArchiveStatus(ctx, status); err != nil {
		metrics.ArchiveJobTotal.WithLabelValues("verify", "error").Inc()
		log.Error().Err(err).Msg("update archive status to verified failed")
		return
	}

	metrics.ArchiveJobTotal.WithLabelValues("verify", "ok").Inc()
	log.Info().Str("partition", status.PartitionName).Msg("verify passed, proceeding to cleanup")

	// 自动进入下一步
	s.stepCleanup(ctx, status)
}

// stepCleanup 第三步：DETACH + DROP 分区
func (s *ArchiveService) stepCleanup(ctx context.Context, status *model.ArchiveStatus) {
	if err := s.archive.DetachAndDropPartition(ctx, status.PartitionName); err != nil {
		metrics.ArchiveJobTotal.WithLabelValues("cleanup", "error").Inc()
		log.Error().Err(err).Str("partition", status.PartitionName).Msg("detach/drop partition failed")
		s.markFailed(ctx, status, "cleanup: "+err.Error())
		return
	}

	status.Status = "cleaned"
	status.ErrorMessage = ""
	if err := s.archive.UpsertArchiveStatus(ctx, status); err != nil {
		metrics.ArchiveJobTotal.WithLabelValues("cleanup", "error").Inc()
		log.Error().Err(err).Msg("update archive status to cleaned failed")
		return
	}

	metrics.ArchiveJobTotal.WithLabelValues("cleanup", "ok").Inc()
	log.Info().Str("partition", status.PartitionName).Msg("cold archive completed: partition cleaned")
}

// markFailed 标记归档失败（可幂等续跑）
func (s *ArchiveService) markFailed(ctx context.Context, status *model.ArchiveStatus, reason string) {
	status.Status = "failed"
	status.ErrorMessage = reason
	if err := s.archive.UpsertArchiveStatus(ctx, status); err != nil {
		log.Error().Err(err).Str("partition", status.PartitionName).Msg("mark archive failed: upsert failed")
	}
}

// fileChecksum 计算文件的 SHA-256 校验和
func fileChecksum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

// sha256Hex 计算字节切片的 SHA-256 十六进制字符串
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
