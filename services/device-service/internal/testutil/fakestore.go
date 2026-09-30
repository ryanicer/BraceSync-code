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
	audits     []repo.WifiClearAuditInput // T448 清除留痕（audit_logs 的内存替身）
	instAudits []repo.InstallAuditInput   // T485 安装记录写留痕（同一张表的另一类对象）
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
	// T418：对齐 PGStore 新增的 LEFT JOIN devices —— 设备行在 ⇒ 型号带出，设备行缺失 ⇒ nil
	cp.DeviceModel = nil
	if d, has := f.devices[rec.DeviceID]; has {
		m := d.Model
		cp.DeviceModel = &m
	}
	return &cp, nil
}

// AddInstallJoinNames T418 测试注入：详情 LEFT JOIN patients/technicians 的姓名两列。
//
// 本夹具对 users 域只存存在性（AddPatient/AddTech）不存名字，姓名由用例显式塞入；
// 不调用本方法的用例两列为 nil —— 与 PG 里「关联行缺失」同一形状，别当默认值铺开
// （会抹掉「未注入必为 null」这一格覆盖）。
func (f *FakeStore) AddInstallJoinNames(installID int64, patientName, techName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.installs[installID]
	if !ok {
		return
	}
	pn, tn := patientName, techName
	rec.PatientName = &pn
	rec.TechName = &tn
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

// GetLatestBaselineByDevice 复刻「规矩 A」：同设备多条基线取 baseline_id 最大的一条（T407）
func (f *FakeStore) GetLatestBaselineByDevice(_ context.Context, deviceID string) (*model.Baseline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *model.Baseline
	for _, b := range f.baselines {
		if b.DeviceID != deviceID {
			continue
		}
		if latest == nil || b.BaselineID > latest.BaselineID {
			cp := *b
			latest = &cp
		}
	}
	if latest == nil {
		return nil, repo.ErrNotFound
	}
	return latest, nil
}

func (f *FakeStore) UpdateInstallMeta(_ context.Context, installID int64, notes, signatureURL *string, wifiStatus *string) error {
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
	// T447：与 PGStore 的 COALESCE 同语义 —— nil 不改该列。这里刻意**不**校验四值：
	// 校验在 service 层（用例要覆盖的就是「脏值到不到得了 store」）。
	if wifiStatus != nil {
		rec.WifiStatus = *wifiStatus
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

// SetContactArea T508：整写 devices.contact_area_cm2。与 PG 实现对齐——
// 查无设备返回 ErrNotFound，其余情况覆盖整列值（无「清空回 NULL」通路）。
func (f *FakeStore) SetContactArea(_ context.Context, deviceID string, areaCm2 float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	dev, ok := f.devices[deviceID]
	if !ok {
		return repo.ErrNotFound
	}
	dev.ContactAreaCm2 = &areaCm2
	dev.UpdatedAt = time.Now()
	return nil
}

func (f *FakeStore) WriteWifiClearAudit(_ context.Context, in repo.WifiClearAuditInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.devices[in.DeviceID]; !ok {
		return repo.ErrNotFound
	}
	f.audits = append(f.audits, in)
	return nil
}

// WifiClearAudits 读取已落的清除留痕（T448 用例判据）
func (f *FakeStore) WifiClearAudits() []repo.WifiClearAuditInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]repo.WifiClearAuditInput, len(f.audits))
	copy(out, f.audits)
	return out
}

// WriteInstallRecordAudit T485：安装记录写留痕的内存替身。
// 与真库实现同口径：audit_logs 没有任何外键指向 install_records，
// 所以桩同样不校验 install_id 是否存在 —— 「脏号不得编造审计行」那条判据
// 落在调用方（写通路本身先 404），别在这里替库编约束。
func (f *FakeStore) WriteInstallRecordAudit(_ context.Context, in repo.InstallAuditInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instAudits = append(f.instAudits, in)
	return nil
}

// InstallRecordAudits 读取已落的安装记录留痕（T485 用例判据）
func (f *FakeStore) InstallRecordAudits() []repo.InstallAuditInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]repo.InstallAuditInput, len(f.instAudits))
	copy(out, f.instAudits)
	return out
}

func strPtr(s string) *string { return &s }
