// Package testutil device-service 测试共享夹具：repo.Store 内存实现（复刻 PG 约束语义）
package testutil

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

// TestEncKey 测试用 AES-GCM 密钥（32 字节 hex）：仅测试用途，非生产密钥
const TestEncKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// FakeStore repo.Store 的内存实现：复刻绑定互斥/幂等/单调推进等 SQL 语义
type FakeStore struct {
	mu         sync.Mutex
	devices    map[string]*model.Device
	bindings   []model.Binding
	nextBindID int64
	installs   map[int64]*model.InstallRecord
	nextInstID int64
	baselines  map[int64]*model.Baseline
	nextBaseID int64
	patients   map[string]bool
	techs      map[string]bool
}

// NewFakeStore 创建空 FakeStore
func NewFakeStore() *FakeStore {
	return &FakeStore{
		devices:    map[string]*model.Device{},
		installs:   map[int64]*model.InstallRecord{},
		baselines:  map[int64]*model.Baseline{},
		patients:   map[string]bool{},
		techs:      map[string]bool{},
		nextBindID: 1, nextInstID: 1, nextBaseID: 1,
	}
}

// AddPatient / AddTech 注入用户域存在性（owner: user-service，测试桩）
func (f *FakeStore) AddPatient(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patients[id] = true
}

func (f *FakeStore) AddTech(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.techs[id] = true
}

func cloneDevice(d *model.Device) *model.Device {
	cp := *d
	return &cp
}

func (f *FakeStore) RegisterDevice(_ context.Context, d *model.Device) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.devices[d.DeviceID]; ok {
		return false, nil
	}
	now := time.Now()
	d.CreatedAt, d.UpdatedAt = now, now
	f.devices[d.DeviceID] = cloneDevice(d)
	return true, nil
}

func (f *FakeStore) GetDevice(_ context.Context, deviceID string) (*model.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.devices[deviceID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return cloneDevice(d), nil
}

func (f *FakeStore) PatientExists(_ context.Context, patientID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.patients[patientID], nil
}

func (f *FakeStore) TechExists(_ context.Context, techID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.techs[techID], nil
}

// activeBinding 当前有效绑定（uk_bindings_active 语义：至多一条）
func (f *FakeStore) activeBinding(deviceID string) *model.Binding {
	for i := range f.bindings {
		if f.bindings[i].DeviceID == deviceID && f.bindings[i].UnbindAt == nil {
			return &f.bindings[i]
		}
	}
	return nil
}

// patientOtherDevice 「一患者一设备」事实源（T299，同 uk_devices_active_patient 口径）：
// 返回患者已占用的、非 targetDeviceID 的那台设备 ID；无占用返回 ""。
func (f *FakeStore) patientOtherDevice(patientID, targetDeviceID string) string {
	for id, dev := range f.devices {
		if id == targetDeviceID {
			continue
		}
		if dev.PatientID != nil && *dev.PatientID == patientID {
			return id
		}
	}
	return ""
}

func (f *FakeStore) Bind(_ context.Context, p repo.BindParams) (*repo.BindOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dev, ok := f.devices[p.DeviceID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	var swappedFrom string
	if other := f.patientOtherDevice(p.PatientID, p.DeviceID); other != "" {
		if !p.ConfirmSwap {
			return nil, &repo.ErrPatientHasDevice{PatientID: p.PatientID, OtherDeviceID: other}
		}
		swappedFrom = f.releasePatientDevice(other, p.OperatorID) // T299 确认换绑：同一次调用内解旧绑新
	}

	var prevActive *model.Binding
	reason := model.ReasonInstall
	if active := f.activeBinding(p.DeviceID); active != nil {
		if active.PatientID == p.PatientID {
			return nil, nil // 幂等
		}
		// 自动换绑：关闭旧绑定（reason=rebind）
		now := time.Now()
		active.UnbindAt = &now
		active.Reason = strPtr(model.ReasonRebind)
		active.OperatorID = strPtr(p.OperatorID)
		cp := *active
		prevActive = &cp
		reason = model.ReasonRebind
	}

	f.bindings = append(f.bindings, model.Binding{
		BindingID:  f.nextBindID,
		DeviceID:   p.DeviceID,
		PatientID:  p.PatientID,
		BindAt:     time.Now(),
		Reason:     strPtr(reason),
		OperatorID: strPtr(p.OperatorID),
	})
	f.nextBindID++

	now := time.Now()
	dev.PatientID = strPtr(p.PatientID)
	dev.BindTime = &now
	dev.Status = model.NextStatusOnBind(dev.Status)
	dev.UpdatedAt = now
	return repo.NewBindOutcome(prevActive, swappedFrom), nil
}

func (f *FakeStore) Rebind(_ context.Context, p repo.BindParams) (*repo.BindOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dev, ok := f.devices[p.DeviceID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	active := f.activeBinding(p.DeviceID)
	if active == nil {
		return nil, repo.ErrConflict // 无 active binding，应先走 Bind
	}
	if active.PatientID == p.PatientID {
		return nil, nil // 幂等
	}
	var swappedFrom string
	if other := f.patientOtherDevice(p.PatientID, p.DeviceID); other != "" {
		if !p.ConfirmSwap {
			return nil, &repo.ErrPatientHasDevice{PatientID: p.PatientID, OtherDeviceID: other}
		}
		swappedFrom = f.releasePatientDevice(other, p.OperatorID) // T299 确认换绑：关闭本机旧绑定之前先解旧设备
	}

	now := time.Now()
	active.UnbindAt = &now
	active.Reason = strPtr(model.ReasonRebind)
	active.OperatorID = strPtr(p.OperatorID)
	cp := *active

	f.bindings = append(f.bindings, model.Binding{
		BindingID:  f.nextBindID,
		DeviceID:   p.DeviceID,
		PatientID:  p.PatientID,
		BindAt:     now,
		Reason:     strPtr(model.ReasonRebind),
		OperatorID: strPtr(p.OperatorID),
	})
	f.nextBindID++

	dev.PatientID = strPtr(p.PatientID)
	dev.BindTime = &now
	dev.UpdatedAt = now
	return repo.NewBindOutcome(&cp, swappedFrom), nil
}

// releasePatientDevice T299 确认换绑：解除该患者原设备的绑定（关闭 binding 写 reason=rebind + 设备归属清空），
// 返回被解除的设备号，与 PGStore.swapPatientOtherDevice 的两次 UPDATE 同语义。
func (f *FakeStore) releasePatientDevice(deviceID, operatorID string) string {
	if active := f.activeBinding(deviceID); active != nil {
		now := time.Now()
		active.UnbindAt = &now
		active.Reason = strPtr(model.ReasonRebind)
		active.OperatorID = strPtr(operatorID)
	}
	if dev, ok := f.devices[deviceID]; ok {
		now := time.Now()
		dev.PatientID = nil
		dev.BindTime = nil
		dev.Status = model.StatusUnbound
		dev.UpdatedAt = now
	}
	return deviceID
}

func (f *FakeStore) Unbind(_ context.Context, deviceID, operatorID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dev, ok := f.devices[deviceID]
	if !ok {
		return false, repo.ErrNotFound
	}
	active := f.activeBinding(deviceID)
	if active == nil {
		return false, nil // 幂等
	}
	now := time.Now()
	active.UnbindAt = &now
	active.Reason = strPtr(model.ReasonUnbind)
	active.OperatorID = strPtr(operatorID)

	dev.PatientID = nil
	dev.BindTime = nil
	dev.Status = model.StatusUnbound
	dev.UpdatedAt = now
	return true, nil
}

// Touch 单调推进：与 PG GREATEST/CASE 语义一致
func (f *FakeStore) Touch(_ context.Context, deviceID string, ts time.Time, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	dev, ok := f.devices[deviceID]
	if !ok {
		return repo.ErrNotFound
	}
	isNewest := dev.LastReportAt == nil || !ts.Before(*dev.LastReportAt)
	if isNewest {
		dev.LastReportAt = &ts
		dev.Status = status
	}
	dev.UpdatedAt = time.Now()
	return nil
}

func (f *FakeStore) ListBindings(_ context.Context, deviceID string) ([]model.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.Binding{}
	for _, b := range f.bindings {
		if b.DeviceID == deviceID {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BindingID > out[j].BindingID })
	return out, nil
}

func (f *FakeStore) CreateInstall(_ context.Context, rec *model.InstallRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec.InstallID = f.nextInstID
	rec.WifiStatus = "unconfigured"
	rec.CreatedAt = time.Now()
	cp := *rec
	f.installs[rec.InstallID] = &cp
	f.nextInstID++
	return rec.InstallID, nil
}

func (f *FakeStore) GetInstall(_ context.Context, installID int64) (*model.InstallRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.installs[installID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *rec
	cp.OffsetValues = nil // 对齐 PGStore LEFT JOIN：仅已校准才有偏移值
	if rec.BaselineID != nil {
		if b, has := f.baselines[*rec.BaselineID]; has {
			cp.OffsetValues = append([]float32(nil), b.OffsetValues...)
		}
	}
	return &cp, nil
}

func (f *FakeStore) SaveBaseline(_ context.Context, installID int64, offsets []float32, calibratorID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.installs[installID]
	if !ok {
		return 0, repo.ErrNotFound
	}
	if rec.BaselineID != nil {
		return 0, repo.ErrConflict
	}
	id := f.nextBaseID
	f.nextBaseID++
	f.baselines[id] = &model.Baseline{
		BaselineID:   id,
		InstallID:    installID,
		DeviceID:     rec.DeviceID,
		OffsetValues: offsets,
		CalibratorID: calibratorID,
		CreatedAt:    time.Now(),
	}
	rec.BaselineID = &id
	rec.CalibrateTime = time.Now()
	return id, nil
}

func (f *FakeStore) UpdateInstallMeta(_ context.Context, installID int64, notes, signatureURL *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.installs[installID]
	if !ok {
		return repo.ErrNotFound
	}
	if notes != nil {
		rec.Notes = notes
	}
	if signatureURL != nil {
		rec.SignatureURL = signatureURL
	}
	return nil
}

func (f *FakeStore) SetWifiSSID(_ context.Context, deviceID, ssid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	dev, ok := f.devices[deviceID]
	if !ok {
		return repo.ErrNotFound
	}
	dev.WifiSSID = strPtr(ssid)
	dev.UpdatedAt = time.Now()
	// 最近安装记录置 connected（与 PG 实现对齐）
	var latest *model.InstallRecord
	for _, rec := range f.installs {
		if rec.DeviceID == deviceID && (latest == nil || rec.InstallID > latest.InstallID) {
			latest = rec
		}
	}
	if latest != nil {
		latest.WifiStatus = "connected"
	}
	return nil
}

func strPtr(s string) *string { return &s }
