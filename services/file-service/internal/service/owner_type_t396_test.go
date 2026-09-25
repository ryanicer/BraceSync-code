// Package service T396 判据：签发层的 owner_type 枚举门。
//
// handler 已有同一道门（handler/owner_type_t396_test.go）；这里补的是**下一层**：
// GenerateUploadURL 是签发唯一入口，被 handler 之外（含未来的内部调用方）直接调用时，
// 未知 owner_type 也不能落到「先签出 COS URL、再在 INSERT 处撞迁移 000030 的 CHECK」那一步。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
)

func TestT396_GenerateUploadURL_OwnerTypeOutsideEnum_NoRegistration(t *testing.T) {
	store := newMemStore()
	p := newTestPresigner(store)

	for _, ot := range []string{"", " ", "Patient", "PATIENT", "review", "unknown_type", "patient "} {
		_, err := p.GenerateUploadURL(context.Background(), UploadRequest{
			FileType:    model.FileTypeCommPhoto,
			OwnerType:   ot,
			OwnerID:     "P1",
			ContentType: "image/png",
		})
		assert.ErrorIs(t, err, ErrInvalidRequest, "owner_type=%q 必须停在参数校验", ot)
	}
	assert.Empty(t, store.files, "被拒的签发不许留下 pending 行")
}

func TestT396_GenerateUploadURL_EnumValues_Registered(t *testing.T) {
	for _, ot := range []string{
		model.OwnerTypePatient,
		model.OwnerTypeAlert,
		model.OwnerTypeReviewTemplate,
		model.OwnerTypeInstallRecord,
	} {
		store := newMemStore()
		p := newTestPresigner(store)

		resp, err := p.GenerateUploadURL(context.Background(), UploadRequest{
			FileType:    model.FileTypeCommPhoto,
			OwnerType:   ot,
			OwnerID:     "P1",
			ContentType: "image/png",
		})
		require.NoError(t, err, "枚举内取值 %q 被签发层拦下 = 打断上传", ot)
		fm, err := store.GetFileByFileID(context.Background(), resp.FileID)
		require.NoError(t, err)
		assert.Equal(t, ot, fm.OwnerType, "落库 owner_type 必须与请求逐字相等")
		// object key 首段就是 owner_type（presigner 的命名口径）：值集收口后它不再有任意串
		assert.Contains(t, resp.ObjectKey, ot+"/P1/")
	}
}

// TestT396_ReviewTemplateConstSingleSource service 包对外仍导出 OwnerTypeReviewTemplate（T135 起前端口径），
// 收口后它必须是 model 枚举的别名而不是第二份字面量——两处各写一遍就会漂。
func TestT396_ReviewTemplateConstSingleSource(t *testing.T) {
	assert.Equal(t, model.OwnerTypeReviewTemplate, OwnerTypeReviewTemplate)
	assert.Equal(t, "ReviewTemplate", OwnerTypeReviewTemplate)
}
