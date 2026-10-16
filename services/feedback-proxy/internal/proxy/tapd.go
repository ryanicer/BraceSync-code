package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// tapdClient TAPD 服务账号客户端（Basic Auth=NUMBER+TOKEN 模式，凭据只在容器 env）。
// 端点对齐 peter v3 设计 D-1/D-2：POST /bugs 建缺陷 · PUT /bugs 回填描述 ·
// POST /files/upload_attachment 挂附件（单请求一个文件、串行循环、不落盘）。
type tapdClient struct {
	base        string
	user        string
	pass        string
	workspaceID string
	attachType  string // 缺陷附件 type 枚举（D-2 注：联调期实测确认；env TAPD_ATTACHMENT_TYPE 可覆盖）
	http        *http.Client
}

func NewTapdClient(base, user, pass, workspaceID, attachType string) *tapdClient {
	return &tapdClient{
		base:        strings.TrimRight(base, "/"),
		user:        user,
		pass:        pass,
		workspaceID: workspaceID,
		attachType:  attachType,
		http:        &http.Client{Timeout: 60 * time.Second},
	}
}

// tapdResp TAPD 统一响应壳：status=1 成功（data 形态随端点变化）。
type tapdResp struct {
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
}

func (c *tapdClient) do(ctx context.Context, method, path string, body io.Reader, contentType string) (tapdResp, error) {
	var zero tapdResp
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return zero, err
	}
	req.SetBasicAuth(c.user, c.pass)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return zero, err
	}
	var out tapdResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, fmt.Errorf("tapd %s: non-json status=%d", path, resp.StatusCode)
	}
	if out.Status != 1 {
		// 不把请求体/凭据入错误文本；只留端点、状态码与 TAPD msg。
		return out, fmt.Errorf("tapd %s: status=%d http=%d msg=%s", path, out.Status, resp.StatusCode, out.Msg)
	}
	return out, nil
}

// CreateBug 建缺陷（D-2 步2）：标题 [运维反馈] 前缀 + triage/环境/分类标签（D-6）。
// 返回 TAPD bug id（受理号 NNN 的来源）。
func (c *tapdClient) CreateBug(ctx context.Context, title, description, labels string) (int64, error) {
	form := urlValues{
		"workspace_id": c.workspaceID,
		"title":        title,
		"description":  description,
		"labels":       labels,
	}
	resp, err := c.do(ctx, http.MethodPost, "/bugs", strings.NewReader(form.encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return 0, err
	}
	// 响应 data 形态：{"Bug":{"id":"300100023",...}}（id 可能是字符串或数字）。
	var data struct {
		Bug struct {
			ID json.RawMessage `json:"id"`
		} `json:"Bug"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return 0, fmt.Errorf("tapd /bugs: data shape: %w", err)
	}
	return parseID(data.Bug.ID)
}

// UpdateDescription 回填缺陷描述（受理号首行落位，D-2 步3：写入缺陷描述首段）。
func (c *tapdClient) UpdateDescription(ctx context.Context, bugID int64, description string) error {
	form := urlValues{
		"workspace_id": c.workspaceID,
		"id":           strconv.FormatInt(bugID, 10),
		"description":  description,
	}
	_, err := c.do(ctx, http.MethodPut, "/bugs", strings.NewReader(form.encode()), "application/x-www-form-urlencoded")
	return err
}

// UploadAttachment 逐文件挂附件（D-2 步4：单文件单请求串行；multipart 字段=
// workspace_id/entry_id/file/type/custom_field）。data 已在内存（无落盘）。
func (c *tapdClient) UploadAttachment(ctx context.Context, bugID int64, filename, contentType string, data []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("workspace_id", c.workspaceID)
	_ = w.WriteField("entry_id", strconv.FormatInt(bugID, 10))
	_ = w.WriteField("type", c.attachType)
	_ = w.WriteField("custom_field", "")
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="file"; filename=%q`, quoteEscaper.Replace(filename)))
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, "/files/upload_attachment", &buf, w.FormDataContentType())
	return err
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// urlValues 轻量表单编码（避免为一处使用引入 net/url.Values 的展开噪音）。
type urlValues map[string]string

func (v urlValues) encode() string {
	var b strings.Builder
	first := true
	for _, k := range []string{"workspace_id", "id", "title", "description", "labels"} {
		val, ok := v[k]
		if !ok {
			continue
		}
		if !first {
			b.WriteByte('&')
		}
		first = false
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(percentEncode(val))
	}
	return b.String()
}

// percentEncode form 编码（application/x-www-form-urlencoded：空格=+，保留 RFC3986 非保留）。
func percentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == '*', ch == '~':
			b.WriteByte(ch)
		case ch == ' ':
			b.WriteByte('+')
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// parseID TAPD id 字段双形态（"300100023" 或 300100023）。
func parseID(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("tapd: empty bug id")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("tapd: bug id %q: %w", s, err)
		}
		return id, nil
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("tapd: bug id shape: %w", err)
	}
	return n, nil
}
