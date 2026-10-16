package proxy

import (
	"errors"
	"regexp"
	"strings"
)

// env 白名单（D-2 步1）：只收 prod/staging，其余 400 不触达 TAPD。
const (
	EnvProd    = "prod"
	EnvStaging = "staging"
)

// 分类清单（D-6 表单 6 选 1，服务端白名单校验；标签=「分类:」+此值）。
var allowedCategories = map[string]bool{
	"页面报错/白屏": true,
	"数据异常":    true,
	"功能不符预期":  true,
	"性能卡顿":    true,
	"登录/权限":   true,
	"其他":      true,
}

// 附件硬顶：单个 ≤150MB（TAPD upload_attachment 上限，D-1/D-2）；
// 前端页另按 10MB/张 UX 收紧，服务端只做防呆上限。
const maxFiles = 9

var maxFileBytes int64 = 150 << 20 // 150 MiB

// pagePath 允许留空；填了做基本形状校验（防把整段日志塞进「路径」格）。
var pagePathRe = regexp.MustCompile(`^[\x20-\x7E]{0,500}$`)

// submitReq 收单校验后的结构化请求（meta 部件 JSON 形态）。
type submitReq struct {
	Env         string `json:"env"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Submitter   string `json:"submitter"`
	PagePath    string `json:"pagePath"`
}

// err400 统一 400 语义（不落描述正文进错误文本，防日志带出问题正文）。
var (
	errEnv        = errors.New("env must be prod or staging")
	errTitle      = errors.New("title required (1-200)")
	errCategory   = errors.New("category not in whitelist")
	errDesc       = errors.New("description required (1-5000)")
	errSubmitter  = errors.New("submitter required (1-50)")
	errPagePath   = errors.New("pagePath must be printable ascii (<=500)")
	errFileCount  = errors.New("too many files (max 9)")
	errFileType   = errors.New("file type must be png/jpg/gif/mp4")
	errFileSize   = errors.New("file too large (max 150MB)")
	errTooMany400 = errors.New("file count exceeds limit")
)

func (r *submitReq) validate() error {
	r.Env = strings.TrimSpace(r.Env)
	r.Title = strings.TrimSpace(r.Title)
	r.Category = strings.TrimSpace(r.Category)
	r.Submitter = strings.TrimSpace(r.Submitter)
	if r.Env != EnvProd && r.Env != EnvStaging {
		return errEnv
	}
	if len(r.Title) < 1 || len([]rune(r.Title)) > 200 {
		return errTitle
	}
	if !allowedCategories[r.Category] {
		return errCategory
	}
	d := strings.TrimSpace(r.Description)
	if len(d) < 1 || len([]rune(d)) > 5000 {
		return errDesc
	}
	r.Description = d
	if len([]rune(r.Submitter)) < 1 || len([]rune(r.Submitter)) > 50 {
		return errSubmitter
	}
	if !pagePathRe.MatchString(r.PagePath) {
		return errPagePath
	}
	return nil
}

// envLabel 环境标签（D-6：2 枚，按 env 白名单自动，用户不选）。
func envLabel(env string) string {
	if env == EnvProd {
		return "环境:生产"
	}
	return "环境:预发(staging)"
}

// labelsFor 建单标签三件套：triage + 环境 + 分类（POST /bugs 传不存在标签 TAPD 自动创建）。
func labelsFor(env, category string) string {
	return "triage," + envLabel(env) + ",分类:" + category
}

// allowedFileExt 附件后缀白名单（页 spec：png/jpg/gif/mp4；jpg 兼容 .jpeg 与 image/jpeg）。
func allowedFileExt(name, contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	// content-type 非空 ⇒ 以它为准（不在白名单即拒，防 .png 挂 text/x-sh 混入）；
	// 浏览器偶发 application/octet-stream 时回落扩展名判定。
	if ct != "" && ct != "application/octet-stream" {
		if ct == "image/png" || ct == "image/gif" || ct == "video/mp4" || strings.HasPrefix(ct, "image/jpeg") {
			return true
		}
		return false
	}
	n := strings.ToLower(name)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".mp4"} {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	return false
}
