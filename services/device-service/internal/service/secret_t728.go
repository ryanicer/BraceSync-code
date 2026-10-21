// T728 设备验签密钥的回显 / 下载 / 重生成（service 层）。
//
// 卡面痛点：Boss 经 staging admin-web 注册设备后拿不到 secret，只能走 DB/API 导出经 andy 中转，
// 固件烧录阻塞数小时。本卡把「取密钥」收在服务端专用端点上（PM 红线 1：解密逻辑不下放前端），
// 于是这一层要回答三件事：注册那一次的一次性明文、按台取文件体、重生成并让旧值作废。
//
// 明文的生命周期（红线 2）：只存在于返回值里 —— 不进日志（T464 的 logTechnical 只在错误路径）、
// 不进审计 detail（repo/secret_t728.go 的两个入参结构体根本没有装 secret 的字段）、
// 不进 DTO（model.DeviceDTO 恒不含密钥列，本卡不动它）。
package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

// MaxSecretExportBatch 单次批量导出的设备数上限（PM 红线 5：批量必须有限额，且只限勾选面）。
// 取 50：一次烧录批次的量级，且 50 台的留痕 detail 仍是一行可读的 JSON；不是「全部设备」的委婉说法
// ——请求体只接受 ID 列表，服务端从不自己扩名单（红线 5 的「不得把全部设备当默认」）。
const MaxSecretExportBatch = 50

// 逐台降级的机器可读原因（批量下载里某一台拿不出来时给前端分流用）。
// 词形与 model 那侧同源：device_secret_unusable 直接取 model.ReasonDeviceSecretUnusable，
// 不另造同义词（T573 已把这个 reason 定为跨服务边界的判据）。
const (
	SecretSkipInvalidDeviceID = "invalid_device_id"
	SecretSkipNotRegistered   = "device_not_registered"
	SecretSkipUnusable        = model.ReasonDeviceSecretUnusable
	SecretSkipUnavailable     = "secret_unavailable"
)

// SecretEnvFile 一台设备的 .env 文件（文件名=设备 ID，卡面需求 3）。
type SecretEnvFile struct {
	DeviceID string
	Body     string
}

// SecretSkip 批量下载里被跳过的一台及其机器可读原因。
type SecretSkip struct {
	DeviceID string
	Reason   string
}

// SecretEnvBody 生成 .env 文件体：两行 DEVICE_ID / DEVICE_SECRET，与现网密道文件
// （stg-device-secret.txt 的前两行）逐字同口径，固件侧拿到即可直接读。
// 行尾用 LF：这两行是 ASCII 键值，CRLF 会让 sha256 对不上（自验 2 的对照口径）。
func SecretEnvBody(deviceID, secret string) string {
	return fmt.Sprintf("DEVICE_ID=%s\nDEVICE_SECRET=%s\n", deviceID, secret)
}

// RegisterWithOneTimeSecret 注册并交出「本次新建」那一版的密钥明文。
//
// 为什么只有 created==true 才有明文：repo.RegisterDevice 是 INSERT … ON CONFLICT DO NOTHING
// （幂等注册不覆盖既有密钥），所以 created==false 时本次生成的那串随机数**根本没有落库**，
// 把它回给调用方等于回一个与库里不同的密钥 —— 设备按它烧录后就再也验不过签名。
// 这一支恒回 ""，前端据此不弹回显窗（要取已存在设备的密钥走下载端点，每次都留痕）。
func (s *DeviceService) RegisterWithOneTimeSecret(ctx context.Context, deviceID, deviceModel string) (*model.Device, bool, string, *model.AppError) {
	if !model.ValidDeviceID(deviceID) {
		return nil, false, "", model.ErrInvalidParam("invalid device_id %q (4-48 chars, alnum/-/_)", deviceID)
	}
	if deviceModel == "" {
		deviceModel = model.DefaultModel
	}

	secret, err := crypto.RandomSecret(secretBytes)
	if err != nil {
		return nil, false, "", model.ErrInternal("generate device secret: %v", err)
	}
	// A-1（T091 补录）：防御性契约校验——device_secret 必须为 64 字符 hex，
	// 与固件 HKDF ikm 约定一致（64 ASCII 字节）。当前 RandomSecret 恒合规，
	// 此处防止未来 RandomSecret 改动静默破坏配网密钥对齐。
	if !model.ValidDeviceSecret(secret) {
		return nil, false, "", model.ErrInvalidParam("device_secret must be 64-char hex, got %q", secret)
	}
	encSecret, err := s.enc.Encrypt([]byte(secret))
	if err != nil {
		return nil, false, "", model.ErrInternal("encrypt device secret: %v", err)
	}

	dev := &model.Device{
		DeviceID:        deviceID,
		Model:           deviceModel,
		DeviceSecretEnc: encSecret,
		SecretVersion:   1, // 一期固定=1（架构 §4.3，预留双密钥轮换）；T728 重生成后由 UpdateDeviceSecret 自增
		Status:          model.StatusUnbound,
	}
	created, err := s.store.RegisterDevice(ctx, dev)
	if err != nil {
		return nil, false, "", model.ErrInternal("register device: %v", err)
	}

	stored, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return nil, false, "", mapRepoErr(err, model.ErrNotFound("device %q not found after register", deviceID))
	}
	if !created {
		return stored, false, "", nil
	}
	return stored, true, secret, nil
}

// DeviceSecretEnv 取一台设备的 .env 文件体（单台下载，卡面需求 2）。
// 错误面沿用 GetDeviceSecret：未注册 20404；密文不成立 90001 + data.reason=device_secret_unusable
// （T573 那一支——seed 里那批设备的密钥列是 1 字节占位，永远解不开，前端据此引导走重生成）。
func (s *DeviceService) DeviceSecretEnv(ctx context.Context, deviceID string) (SecretEnvFile, *model.AppError) {
	if !model.ValidDeviceID(deviceID) {
		return SecretEnvFile{}, model.ErrInvalidParam("invalid device_id %q (4-48 chars, alnum/-/_)", deviceID)
	}
	secret, appErr := s.GetDeviceSecret(ctx, deviceID)
	if appErr != nil {
		return SecretEnvFile{}, appErr
	}
	return SecretEnvFile{DeviceID: deviceID, Body: SecretEnvBody(deviceID, secret)}, nil
}

// DeviceSecretEnvFiles 批量下载（卡面需求 3）：只按调用方给的设备号取，逐台降级不整批失败。
//
// 为什么允许部分成功：现网一次勾选里混着「注册时正常入库」与「seed 遗留不可解」两类设备
// （T573 实测），整批 500 会让能拿到密钥的那几十台也陪绑，而烧录要的是那几台。
// 跳过项连同原因回给调用方，并由 handler 落进同一条下载留痕（detail 只有设备号，红线 3）。
//
// 设备号去重保序、格式非法不进仓储（不拿脏号去库里撞存在性）。
func (s *DeviceService) DeviceSecretEnvFiles(ctx context.Context, deviceIDs []string) ([]SecretEnvFile, []SecretSkip, *model.AppError) {
	unique := make([]string, 0, len(deviceIDs))
	seen := make(map[string]bool, len(deviceIDs))
	var skips []SecretSkip
	for _, raw := range deviceIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue // 空串不是设备号，直接丢弃（不占跳过位，免得留痕里出现空 target）
		}
		if seen[id] {
			continue // 重复勾选：只出一个文件
		}
		if !model.ValidDeviceID(id) {
			seen[id] = true
			skips = append(skips, SecretSkip{DeviceID: id, Reason: SecretSkipInvalidDeviceID})
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	if len(unique)+len(skips) == 0 {
		return nil, nil, model.ErrInvalidParam("device_ids is empty")
	}
	if len(unique) > MaxSecretExportBatch {
		return nil, nil, model.ErrInvalidParam("too many devices in one export: max %d, got %d",
			MaxSecretExportBatch, len(unique))
	}

	files := make([]SecretEnvFile, 0, len(unique))
	for _, id := range unique {
		file, appErr := s.DeviceSecretEnv(ctx, id)
		if appErr != nil {
			skips = append(skips, SecretSkip{DeviceID: id, Reason: secretSkipReason(appErr)})
			continue
		}
		files = append(files, file)
	}
	return files, skips, nil
}

// secretSkipReason 把取密钥失败的 AppError 归成机器可读的跳过原因。
// 「按设备、永久、复跑不变」（密文不成立）与「全站临时」（其余 90001）必须分两支，
// 判据仍挂在 data.reason 上（T573 的同一条），不看 message 文本。
func secretSkipReason(appErr *model.AppError) string {
	switch {
	case appErr.Code == model.CodeNotFound:
		return SecretSkipNotRegistered
	case appErr.Data != nil:
		if data, ok := appErr.Data.(map[string]any); ok {
			if reason, _ := data[model.ReasonKey].(string); reason == model.ReasonDeviceSecretUnusable {
				return SecretSkipUnusable
			}
		}
	}
	return SecretSkipUnavailable
}

// RegenerateDeviceSecret 重新生成一台设备的验签密钥，返回新明文（一次性）与新版本号。
//
// 旧值失效的机制：这一行 device_secret_enc 被整写覆盖，网关/固件此后按新串验签 ⇒ 旧串签不上。
// 版本号自增（卡面红线 6）既给留痕反查用，也是 UpdateDeviceSecret 的乐观锁键。
// 网关侧那份内存缓存（T032）不在本方法射程内，失效通道见 gateway cmd/server/secret_provider.go。
func (s *DeviceService) RegenerateDeviceSecret(ctx context.Context, deviceID string) (string, int, *model.AppError) {
	if !model.ValidDeviceID(deviceID) {
		return "", 0, model.ErrInvalidParam("invalid device_id %q (4-48 chars, alnum/-/_)", deviceID)
	}
	dev, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return "", 0, mapRepoErr(err, model.ErrNotFound("device %q not registered", deviceID))
	}
	secret, err := crypto.RandomSecret(secretBytes)
	if err != nil {
		return "", 0, model.ErrInternal("generate device secret: %v", err)
	}
	if !model.ValidDeviceSecret(secret) {
		return "", 0, model.ErrInvalidParam("device_secret must be 64-char hex, got %q", secret)
	}
	encSecret, err := s.enc.Encrypt([]byte(secret))
	if err != nil {
		return "", 0, model.ErrInternal("encrypt device secret: %v", err)
	}
	next, err := s.store.UpdateDeviceSecret(ctx, deviceID, encSecret, dev.SecretVersion)
	if err != nil {
		return "", 0, mapRepoErr(err, model.ErrNotFound("device %q not registered", deviceID))
	}
	return secret, next, nil
}

// AuditDeviceSecretRead T728：下载密钥文件的留痕，action=sensitive_read。
// 与 AuditInstallRecord 的「只 WARN」相反：留痕是披露的前置条件，失败必须由调用方拒绝发包。
func (s *DeviceService) AuditDeviceSecretRead(ctx context.Context, in repo.DeviceSecretReadAuditInput) error {
	return s.store.WriteDeviceSecretReadAudit(ctx, in)
}

// AuditDeviceSecretRegenerate T728：重生成本身的留痕（谁在何时作废了哪一版密钥）。
// 这一条跟在主写之后，失败由调用方记 WARN——密钥已经换掉了，回滚不在本卡射程，
// 让运维面对一个「库里是新值、界面上报 500」的设备比漏一行留痕更糟（口径同 T485）。
func (s *DeviceService) AuditDeviceSecretRegenerate(ctx context.Context, in repo.DeviceSecretRegenerateAuditInput) error {
	return s.store.WriteDeviceSecretRegenerateAudit(ctx, in)
}
