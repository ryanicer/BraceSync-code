// T728 设备验签密钥的服务端专用端点：单台下载 / 批量下载 / 重新生成（仅 ROLE_ADMIN）。
//
// 三条端点的共同形状（PM 红线 1/4）：
//   - 取密钥只有这一条通路，明文在服务端解密后一次性交给调用方，密文与解密逻辑都不出后端；
//   - 门禁排在方法体最前、先于 JSON 解析与任何仓储访问 ⇒ doctor/tech 直调必吃 403，
//     且 X-Role 缺失（直连绕网关、异常链路）同样 403：判定是 allow-list 单值 ROLE_ADMIN，
//     不是 deny-list（T387 那族放行 admin+tech 的形状在这里不适用——能取密钥=能伪造设备上报）；
//   - 下载必留痕（红线 3）：留痕写不进就不发文件，披露的前置条件不是可选项。
//
// 路由登记三处（缺一处就是「前端 403、直连畅通」或「网关默认拒绝」）：
//
//	gateway cmd/server/rbac.go adminOnlyPatterns + proxy_services.go deviceManageRoutes + 本文件的服务端判定。
//
// 与需求 4「注册后回显仅一次，丢失走重新生成」的关系：回显那一次只在注册响应里出现（handler.register
// 的一次性字段，刷新即无）；本文件的下载端点是常态取用面（每次都要留痕），重生成是密钥疑似外泄
// 或从没拿到时的纠正面。三者互不替代。
package handler

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/repo"
	"github.com/bracesync/bracesync/services/device-service/internal/service"
)

// 留痕描述文案：落进 detail->>'description'（admin 审计页只渲染这一句），必须自带设备号或台数。
// 🔴 三句都不带 secret：留痕面只有设备 ID（红线 3），repo 那两个入参结构体也没有装 secret 的字段。
const (
	secretAuditDescDownloadSingle = "下载设备 %s 的验签密钥文件（.env，含 DEVICE_SECRET 明文）"
	secretAuditDescDownloadBatch  = "批量下载设备验签密钥文件：交付 %d 台 / 跳过 %d 台"
	secretAuditDescRegenerate     = "重新生成设备 %s 的验签密钥（secret_version %d→%d，旧值即时失效）"
)

// secretEnvContentType .env 文件体：与告警报表 CSV 同族，按附件下发。
const secretEnvContentType = "text/plain; charset=utf-8"

// assertAdminRole 仅 ROLE_ADMIN 放行；其余（含身份缺失/未知）403 已写出。
//
// action 只描述动作本身，不带 deviceId —— 带着就等于把「这个号在库里存不存在」变成可辨信息，
// 而拒绝判定排在仓储访问之前，本可以逐字同形（口径同 assertDeviceWriteRole）。
func (h *Handler) assertAdminRole(c *gin.Context, action string) bool {
	role := c.GetHeader(headerRole)
	if role == roleAdmin {
		return true
	}
	fail(c, model.ErrForbidden("role %q may not %s", role, action))
	return false
}

// secretExportRequest POST /api/v1/admin/device-secrets/export 入参。
// 只有设备号白名单这一格：服务端从不自己扩名单，「全部设备」不是一个合法请求（红线 5）。
type secretExportRequest struct {
	DeviceIDs []string `json:"deviceIds"`
}

// downloadDeviceSecretFile GET /api/v1/admin/devices/:deviceId/secret-file —— 单台 .env 下载。
// 响应是文件本体（两行 DEVICE_ID / DEVICE_SECRET），不是统一 JSON 信封；失败仍回信封
// （前端据此分辨「拿到了」与「没拿到」，口径同告警报表 CSV 导出）。
func (h *Handler) downloadDeviceSecretFile(c *gin.Context) {
	if !h.assertAdminRole(c, "download a device secret") {
		return
	}
	deviceID := c.Param("deviceId")
	file, appErr := h.svc.DeviceSecretEnv(c.Request.Context(), deviceID)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	if err := h.svc.AuditDeviceSecretRead(c.Request.Context(), repo.DeviceSecretReadAuditInput{
		DeviceIDs:    []string{deviceID},
		OperatorID:   operatorID(c),
		OperatorRole: c.GetHeader(headerRole),
		IP:           c.ClientIP(),
		Description:  fmt.Sprintf(secretAuditDescDownloadSingle, deviceID),
	}); err != nil {
		// 留痕是披露的前置条件：写不进就不发文件（与 T485「主写已落库所以只 WARN」相反）
		fail(c, model.ErrInternal("record device secret download audit: %v", err))
		return
	}
	writeSecretEnv(c, deviceID, file.Body)
}

// exportDeviceSecrets POST /api/v1/admin/device-secrets/export —— 批量勾选打包（每台一个 .env）。
// 逐台降级：拿不出来的那几台进 skipped（zip 里多一个 _skipped.txt 明示，审计 detail 同记两份名单），
// 全批都拿不出来才回 404 —— 一次勾选里混着 seed 遗留设备（T573）时，不能让能烧的机器陪绑。
func (h *Handler) exportDeviceSecrets(c *gin.Context) {
	if !h.assertAdminRole(c, "export device secrets") {
		return
	}
	var req secretExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	files, skips, appErr := h.svc.DeviceSecretEnvFiles(c.Request.Context(), req.DeviceIDs)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	delivered := make([]string, 0, len(files))
	for _, f := range files {
		delivered = append(delivered, f.DeviceID)
	}
	skipped := make([]string, 0, len(skips))
	for _, sk := range skips {
		skipped = append(skipped, sk.DeviceID)
	}
	if err := h.svc.AuditDeviceSecretRead(c.Request.Context(), repo.DeviceSecretReadAuditInput{
		DeviceIDs:    delivered,
		Skipped:      skipped,
		OperatorID:   operatorID(c),
		OperatorRole: c.GetHeader(headerRole),
		IP:           c.ClientIP(),
		Description:  fmt.Sprintf(secretAuditDescDownloadBatch, len(delivered), len(skipped)),
	}); err != nil {
		fail(c, model.ErrInternal("record device secret export audit: %v", err))
		return
	}
	if len(files) == 0 {
		fail(c, model.ErrNotFound("no device secret available for the %d requested device(s)", len(skipped)))
		return
	}
	writeSecretZip(c, files, skips)
}

// regenerateDeviceSecret POST /api/v1/admin/devices/:deviceId/secret-regenerate —— 重生成密钥。
// 新明文一次性回显（与注册同形：库里此后只有密文，关掉弹窗就再也取不到这一版，只能再重生一次）。
//
// 旧值失效：库里那一行的密文被整写覆盖 + secret_version 自增；网关那份内存缓存由
// gateway 侧的失效通道一并清掉（见 cmd/server/secret_provider.go），否则旧值还能在 TTL 内签成功。
func (h *Handler) regenerateDeviceSecret(c *gin.Context) {
	if !h.assertAdminRole(c, "regenerate a device secret") {
		return
	}
	deviceID := c.Param("deviceId")
	before, appErr := h.svc.GetDevice(c.Request.Context(), deviceID)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	secret, version, appErr := h.svc.RegenerateDeviceSecret(c.Request.Context(), deviceID)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	if err := h.svc.AuditDeviceSecretRegenerate(c.Request.Context(), repo.DeviceSecretRegenerateAuditInput{
		DeviceID:     deviceID,
		OperatorID:   operatorID(c),
		OperatorRole: c.GetHeader(headerRole),
		IP:           c.ClientIP(),
		OldVersion:   before.SecretVersion,
		NewVersion:   version,
		Description:  fmt.Sprintf(secretAuditDescRegenerate, deviceID, before.SecretVersion, version),
	}); err != nil {
		// 密钥已经换掉了，回滚不在本卡射程 ⇒ 只 WARN，不把成功动作报成 500（口径同 T485）
		log.Warn().Err(err).
			Str("request_id", requestIDOf(c)).
			Str("target_type", "device").
			Str("target_id", deviceID).
			Msg("device secret regenerate audit write failed")
	}
	// 网关缓存失效靠这条路由的响应钩子（gateway 侧按 method+path 认出它），
	// 所以响应体里的字段名也是契约的一部分：secret / secretVersion 两个键不得漂形。
	ok(c, gin.H{"deviceId": deviceID, "secret": secret, "secretVersion": version})
}

// writeSecretEnv 下发单台 .env。设备号已过 ValidDeviceID（4-48 位 alnum/-/_），
// 再过一次 safeFilenameToken 是为了让「响应头不注入」这个性质写在写出处，
// 而不是靠人记住上游有一道正则（同 services/alert-service 的 reportFilename）。
func writeSecretEnv(c *gin.Context, deviceID, body string) {
	filename := safeSecretFilenameToken(deviceID, 48) + ".env"
	c.Header("Content-Type", secretEnvContentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Cache-Control", "no-store") // 密钥文件不进浏览器/代理缓存（红线 2 的「一次性」侧）
	c.String(http.StatusOK, body)
}

// writeSecretZip 下发批量 zip：每台一个 <deviceId>.env；有跳过才多一个 _skipped.txt
// （全绿时 zip 里恰好只有勾选那几台的 .env，与验收 3「内含 3 个 .env」逐字对得上）。
func writeSecretZip(c *gin.Context, files []service.SecretEnvFile, skips []service.SecretSkip) {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for _, f := range files {
		w, err := zw.Create(safeSecretFilenameToken(f.DeviceID, 48) + ".env")
		if err != nil {
			fail(c, model.ErrInternal("build secret zip entry: %v", err))
			return
		}
		if _, err = w.Write([]byte(f.Body)); err != nil {
			fail(c, model.ErrInternal("write secret zip entry: %v", err))
			return
		}
	}
	if len(skips) > 0 {
		w, err := zw.Create(secretSkippedEntryName)
		if err != nil {
			fail(c, model.ErrInternal("build secret skip manifest: %v", err))
			return
		}
		if _, err = w.Write([]byte(secretSkipManifest(skips))); err != nil {
			fail(c, model.ErrInternal("write secret skip manifest: %v", err))
			return
		}
	}
	if err := zw.Close(); err != nil {
		fail(c, model.ErrInternal("close secret zip: %v", err))
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", secretZipFilename(time.Now())))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}

// secretSkippedEntryName 跳过名单在 zip 里的文件名（下划线前缀排在设备号条目前，翻开先看得到）。
const secretSkippedEntryName = "_skipped.txt"

// secretSkipManifest 跳过名单文本：每台一行「设备号<TAB>机器可读原因」。
// 只写设备号与原因码，不写后端 message 技术文本（那里面可能带设备号以外的上下文，
// 而且这个文件会被随手转发到群里）。
func secretSkipManifest(skips []service.SecretSkip) string {
	var b bytes.Buffer
	b.WriteString("以下设备未能导出验签密钥（原因码见右列；device_secret_unusable 表示库里密文不成立，需在设备页重新生成）\n")
	for _, sk := range skips {
		fmt.Fprintf(&b, "%s\t%s\n", sk.DeviceID, sk.Reason)
	}
	return b.String()
}

// secretZipFilename 批量包名带 UTC 时刻，让「哪一次下载拿到的哪一批」在烧录现场也分得清。
// 字符集仍是 [A-Za-z0-9._-]：时间戳格式里没有需要转义的字节。
func secretZipFilename(now time.Time) string {
	return fmt.Sprintf("device-secrets-%s.zip", now.UTC().Format("20060102T150405Z"))
}

// safeSecretFilenameToken 收敛文件名片段：只留 [A-Za-z0-9_-]，其余字符丢弃，限长 max。
// 防的是响应头注入（CR/LF/引号逃逸）与 zip 条目路径穿越（'/' 与 '..' 在这里都留不下来）。
// 白名单里**故意不含点号**：设备号本体（model.ValidDeviceID）从来不含 '.'，留着它等于把
// "DEV/../A" 洗成 "DEV..A" —— 斜杠没了、点段还在，穿越写法换一副面孔就过关。扩展名由调用方拼。
// 全被过滤光时回 "device"，不交出空文件名（空名会让浏览器落到无扩展名）。
func safeSecretFilenameToken(s string, max int) string {
	out := make([]rune, 0, max)
	for _, c := range s {
		if len(out) >= max {
			break
		}
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "device"
	}
	return string(out)
}
