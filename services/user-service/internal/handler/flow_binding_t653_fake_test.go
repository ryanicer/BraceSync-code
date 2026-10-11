// T653 类型绑定 fake 方法（补 repo.Store 新接口；供绑定 CRUD 与 auto-start 单测使用）
package handler

import (
	"context"
	"sort"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

func (f *fakeStore) ListFlowTypeBindings(_ context.Context) ([]repo.FlowTypeBindingRow, error) {
	out := make([]repo.FlowTypeBindingRow, 0, len(f.flow.bindings))
	for _, b := range f.flow.bindings {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AlertType < out[j].AlertType })
	return out, nil
}

func (f *fakeStore) GetFlowTypeBinding(_ context.Context, alertType string) (*repo.FlowTypeBindingRow, error) {
	if b, ok := f.flow.bindings[alertType]; ok {
		r := b
		return &r, nil
	}
	return nil, nil
}

func (f *fakeStore) ReplaceFlowTypeBindings(_ context.Context, items []repo.FlowTypeBindingSet) error {
	if f.flow.failReplace {
		return repo.ErrFlowTemplateNotFound
	}
	if f.flow.bindings == nil {
		f.flow.bindings = map[string]repo.FlowTypeBindingRow{}
	}
	f.flow.replacedItems = append(f.flow.replacedItems, items...)
	for _, it := range items {
		if it.TemplateID == "" {
			delete(f.flow.bindings, it.AlertType)
			continue
		}
		f.flow.bindings[it.AlertType] = repo.FlowTypeBindingRow{
			AlertType:  it.AlertType,
			TemplateID: it.TemplateID,
			UpdatedBy:  it.UpdatedBy,
		}
	}
	return nil
}
