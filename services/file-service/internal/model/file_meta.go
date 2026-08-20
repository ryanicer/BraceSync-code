package model

import "time"

// FileType 文件类型（任务需求 1：签名图/安装照片/沟通图/矫形日志图）
type FileType string

const (
	FileTypeSignature    FileType = "signature"     // 电子签名图（install_records.signature_url）
	FileTypeInstallPhoto FileType = "install_photo" // 安装照片
	FileTypeCommPhoto    FileType = "comm_photo"    // 沟通图片
	FileTypeLogPhoto     FileType = "log_photo"     // 矫形日志图片
)

// FileStatus represents upload status
type FileStatus string

const (
	FileStatusPending  FileStatus = "pending"  // 签发预签名时登记，尚未上传
	FileStatusUploaded FileStatus = "uploaded" // 直传 COS 成功并回调登记
	FileStatusFailed   FileStatus = "failed"   // 上传失败/过期（预留态）
)

// ValidFileType 文件类型合法性校验（handler 参数校验与 service 签发共用同一口径）
func ValidFileType(ft FileType) bool {
	switch ft {
	case FileTypeSignature, FileTypeInstallPhoto, FileTypeCommPhoto, FileTypeLogPhoto:
		return true
	}
	return false
}

// FileMetadata represents file metadata stored in database
type FileMetadata struct {
	FileID      string     `db:"file_id" json:"file_id"`           // UUID format, primary key
	Bucket      string     `db:"bucket" json:"bucket"`             // COS bucket name
	ObjectKey   string     `db:"object_key" json:"object_key"`     // COS object path
	URL         string     `db:"url" json:"url"`                   // Public/CDN URL after upload
	FileType    FileType   `db:"file_type" json:"file_type"`       // signature/install_photo/comm_photo/log_photo
	OwnerType   string     `db:"owner_type" json:"owner_type"`     // Entity type: InstallRecord, Patient, etc.
	OwnerID     string     `db:"owner_id" json:"owner_id"`         // Parent entity ID
	Size        int64      `db:"size" json:"size"`                 // File size in bytes
	ContentType string     `db:"content_type" json:"content_type"` // MIME type
	Status      FileStatus `db:"status" json:"status"`             // pending/uploaded/failed
	UploadedAt  *time.Time `db:"uploaded_at" json:"uploaded_at"`   // Timestamp when upload completed (nullable)
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`     // When record was created (pre-upload)
	UpdatedAt   time.Time  `db:"updated_at" json:"updated_at"`     // Last update timestamp
}
