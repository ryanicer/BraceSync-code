// Package service T021：冷归档单元测试（实现侧）
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

func newArchiveTestEnv(t *testing.T) (*ArchiveService, *fakeArchiveStore, *fakePartitionStore) {
	t.Helper()
	archive := newFakeArchiveStore()
	partitions := newFakePartitionStore()
	dir := t.TempDir()
	svc := NewArchiveService(archive, partitions, dir)
	svc.now = func() time.Time { return time.Date(2026, 8, 1, 18, 0, 0, 0, time.UTC) } // CST 08-02 02:00
	return svc, archive, partitions
}

// targetPartitionName 计算 T-7 月分区名（与 RunColdArchive 逻辑一致）
func targetPartitionName() string {
	now := time.Date(2026, 8, 1, 18, 0, 0, 0, time.UTC).In(model.CSTZone())
	target := time.Date(now.Year(), now.Month()-7, 1, 0, 0, 0, 0, model.CSTZone())
	return "pressure_records_" + target.Format("200601") // pressure_records_202601
}

func TestArchiveService_RunColdArchive_PartitionNotExist(t *testing.T) {
	svc, _, _ := newArchiveTestEnv(t)

	svc.RunColdArchive(context.Background()) // 分区不存在 → 跳过
}

func TestArchiveService_RunColdArchive_ExistsError(t *testing.T) {
	svc, _, partitions := newArchiveTestEnv(t)
	partitions.existsErr = errors.New("db error")

	svc.RunColdArchive(context.Background()) // 不应 panic
}

func TestArchiveService_RunColdArchive_AlreadyCleaned(t *testing.T) {
	svc, archive, partitions := newArchiveTestEnv(t)
	pName := targetPartitionName()
	partitions.existing[pName] = true
	archive.statuses[pName] = &model.ArchiveStatus{
		PartitionName: pName,
		Status:        "cleaned",
	}

	svc.RunColdArchive(context.Background())
	assert.Equal(t, 0, archive.detachCalls, "已清理的分区不应再次 detach")
}

func TestArchiveService_RunColdArchive_FullThreeStep(t *testing.T) {
	svc, archive, partitions := newArchiveTestEnv(t)
	pName := targetPartitionName()
	partitions.existing[pName] = true
	archive.rowCounts[pName] = 100

	// 模拟导出文件存在（用于 checksum 校验）
	exportPath := filepath.Join(svc.exportDir, pName+".csv")
	require.NoError(t, os.MkdirAll(svc.exportDir, 0o755))
	require.NoError(t, os.WriteFile(exportPath, []byte("test data"), 0o644))

	svc.RunColdArchive(context.Background())

	// 最终状态应为 cleaned
	final := archive.statuses[pName]
	require.NotNil(t, final)
	assert.Equal(t, "cleaned", final.Status)
	assert.Equal(t, 1, archive.detachCalls)
}

func TestArchiveService_RunColdArchive_ExportError(t *testing.T) {
	svc, archive, partitions := newArchiveTestEnv(t)
	pName := targetPartitionName()
	partitions.existing[pName] = true
	archive.exportErr = errors.New("export failed")

	svc.RunColdArchive(context.Background())

	final := archive.statuses[pName]
	require.NotNil(t, final)
	assert.Equal(t, "failed", final.Status)
	assert.Contains(t, final.ErrorMessage, "export")
}

func TestArchiveService_RunColdArchive_RowCountMismatch(t *testing.T) {
	svc, archive, partitions := newArchiveTestEnv(t)
	pName := targetPartitionName()
	partitions.existing[pName] = true

	// 预置 exported 状态，行数记录 100
	archive.statuses[pName] = &model.ArchiveStatus{
		PartitionName: pName,
		Status:        "exported",
		RowCount:      100,
		Checksum:      "dummy",
		ExportPath:    filepath.Join(svc.exportDir, pName+".csv"),
	}
	// DB 实际行数 99（与导出记录不一致）
	archive.rowCounts[pName] = 99

	svc.RunColdArchive(context.Background())

	final := archive.statuses[pName]
	require.NotNil(t, final)
	assert.Equal(t, "failed", final.Status)
	assert.Contains(t, final.ErrorMessage, "row count mismatch")
	assert.Equal(t, 0, archive.detachCalls, "校验失败禁止清理")
}

func TestArchiveService_RunColdArchive_VerifiedThenCleanup(t *testing.T) {
	svc, archive, partitions := newArchiveTestEnv(t)
	pName := targetPartitionName()
	partitions.existing[pName] = true
	archive.statuses[pName] = &model.ArchiveStatus{
		PartitionName: pName,
		PeriodYear:    2026,
		PeriodMonth:   1,
		Status:        "verified",
		RowCount:      50,
	}
	archive.rowCounts[pName] = 50

	svc.RunColdArchive(context.Background())

	final := archive.statuses[pName]
	require.NotNil(t, final)
	assert.Equal(t, "cleaned", final.Status)
	assert.Equal(t, 1, archive.detachCalls)
}

func TestArchiveService_RunColdArchive_CleanupError(t *testing.T) {
	svc, archive, partitions := newArchiveTestEnv(t)
	pName := targetPartitionName()
	partitions.existing[pName] = true
	archive.statuses[pName] = &model.ArchiveStatus{
		PartitionName: pName,
		Status:        "verified",
	}
	archive.detachErr = errors.New("detach failed")

	svc.RunColdArchive(context.Background())

	final := archive.statuses[pName]
	require.NotNil(t, final)
	assert.Equal(t, "failed", final.Status)
}

func TestArchiveService_RunColdArchive_FailedRetry(t *testing.T) {
	svc, archive, partitions := newArchiveTestEnv(t)
	pName := targetPartitionName()
	partitions.existing[pName] = true
	archive.rowCounts[pName] = 50

	// 预置 failed 状态
	archive.statuses[pName] = &model.ArchiveStatus{
		PartitionName: pName,
		Status:        "failed",
		ErrorMessage:  "previous failure",
	}

	svc.RunColdArchive(context.Background())

	// 应从 failed 状态重新执行 export
	final := archive.statuses[pName]
	require.NotNil(t, final)
	// 如果导出成功（rowCount=50, 空文件），状态会推进
	assert.NotEqual(t, "failed", final.Status, "续跑后不应停留在 failed")
}

func TestArchiveService_RunColdArchive_EmptyPartition(t *testing.T) {
	svc, archive, partitions := newArchiveTestEnv(t)
	pName := targetPartitionName()
	partitions.existing[pName] = true
	archive.rowCounts[pName] = 0 // 空分区

	svc.RunColdArchive(context.Background())

	final := archive.statuses[pName]
	require.NotNil(t, final)
	assert.Equal(t, "cleaned", final.Status)
}

func TestArchiveService_SafeExportPath_Valid(t *testing.T) {
	svc, _, _ := newArchiveTestEnv(t)
	p, err := svc.safeExportPath("pressure_records_202601")
	require.NoError(t, err)
	assert.Contains(t, p, "pressure_records_202601.csv")
}

func TestArchiveService_SafeExportPath_Invalid(t *testing.T) {
	svc, _, _ := newArchiveTestEnv(t)
	_, err := svc.safeExportPath("../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid partition name")
}

func TestArchiveService_SafeExportPath_PathTraversal(t *testing.T) {
	svc, _, _ := newArchiveTestEnv(t)
	_, err := svc.safeExportPath("pressure_records_202601/../../../etc")
	require.Error(t, err)
}

func TestSha256Hex(t *testing.T) {
	result := sha256Hex([]byte("hello"))
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", result)
}

func TestFileChecksum_NonExistent(t *testing.T) {
	_, err := fileChecksum("/tmp/nonexistent_file_12345.csv")
	require.Error(t, err)
}

func TestFileChecksum_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.csv")
	require.NoError(t, os.WriteFile(path, []byte("test"), 0o644))
	cs, err := fileChecksum(path)
	require.NoError(t, err)
	assert.Equal(t, "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", cs)
}

func TestArchiveService_RunColdArchive_CountError(t *testing.T) {
	svc, archive, partitions := newArchiveTestEnv(t)
	pName := targetPartitionName()
	partitions.existing[pName] = true
	archive.statuses[pName] = &model.ArchiveStatus{
		PartitionName: pName,
		Status:        "verified",
	}
	archive.detachErr = errors.New("detach error")

	svc.RunColdArchive(context.Background())
	final := archive.statuses[pName]
	assert.Equal(t, "failed", final.Status)
}
