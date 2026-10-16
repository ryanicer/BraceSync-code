package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ── 收据 ──────────────────────────────────────────────

func TestReceipt_BJTDateAndTail(t *testing.T) {
	// 北京 2026-10-16 23:59:59 = UTC 15:59:59 —— 切日必须按 BJ 不按 UTC。
	bj := time.Date(2026, 10, 16, 23, 59, 59, 0, time.FixedZone("CST", 8*3600))
	if got := Receipt(300100023, bj); got != "FB-20261016-023" {
		t.Fatalf("got %s", got)
	}
	// UTC 时间在 17 日 07:00 前仍是 BJ 16 日；换算依赖入参时刻本身（调用方传 BJ 时点）。
	utc := time.Date(2026, 10, 16, 16, 0, 0, 0, time.UTC) // = BJ 17 日 00:00
	if got := Receipt(19, utc); got != "FB-20261017-019" {
		t.Fatalf("tail padding got %s", got)
	}
	if got := Receipt(300100000, utc); got != "FB-20261017-000" {
		t.Fatalf("tail 000 got %s", got)
	}
}

// ── 校验 ──────────────────────────────────────────────

func validReq() submitReq {
	return submitReq{
		Env: "staging", Title: "分页白屏", Category: "页面报错/白屏",
		Description: "第 3 页白屏，复现两次。", Submitter: "王运维",
	}
}

func TestValidate_GoodPath(t *testing.T) {
	r := validReq()
	if err := r.validate(); err != nil {
		t.Fatalf("valid req rejected: %v", err)
	}
}

func TestValidate_Whitelist(t *testing.T) {
	cases := []struct {
		mut  func(*submitReq)
		fail bool
	}{
		{func(r *submitReq) { r.Env = "test" }, true},
		{func(r *submitReq) { r.Env = "" }, true},
		{func(r *submitReq) { r.Env = "prod" }, false},
		{func(r *submitReq) { r.Title = "" }, true},
		{func(r *submitReq) { r.Category = "安全问题" }, true}, // 不在 6 分类白名单
		{func(r *submitReq) { r.Description = "" }, true},
		{func(r *submitReq) { r.Submitter = "" }, true},
		{func(r *submitReq) { r.PagePath = "路径带中文" }, true}, // pagePath 仅 ASCII
		{func(r *submitReq) { r.Title = strings.Repeat("长", 201) }, true},
	}
	for i, c := range cases {
		r := validReq()
		c.mut(&r)
		err := r.validate()
		if c.fail && err == nil {
			t.Fatalf("case %d: expected error", i)
		}
		if !c.fail && err != nil {
			t.Fatalf("case %d: unexpected %v", i, err)
		}
	}
}

func TestAllowedFileExt(t *testing.T) {
	ok := [][2]string{{"a.png", "image/png"}, {"b.JPG", "image/jpeg"}, {"c.jpeg", ""}, {"d.gif", "image/gif"}, {"e.mp4", "video/mp4"}}
	for _, c := range ok {
		if !allowedFileExt(c[0], c[1]) {
			t.Fatalf("%s should pass", c[0])
		}
	}
	bad := [][2]string{{"a.pdf", "application/pdf"}, {"a.sh", ""}, {"a.png", "text/x-sh"}}
	for _, c := range bad {
		if allowedFileExt(c[0], c[1]) {
			t.Fatalf("%s should reject", c[0])
		}
	}
}

// ── 标签与描述模板 ────────────────────────────────────

func TestLabelsFor(t *testing.T) {
	if got := labelsFor("prod", "性能卡顿"); got != "triage,环境:生产,分类:性能卡顿" {
		t.Fatalf("prod: %s", got)
	}
	if got := labelsFor("staging", "其他"); got != "triage,环境:预发(staging),分类:其他" {
		t.Fatalf("staging: %s", got)
	}
}

func TestDescription_ReceiptFirstLine(t *testing.T) {
	r := validReq()
	r.PagePath = "/admin/patients?page=3"
	desc := buildDescription(r, describeFirstLine)
	final := replaceReceipt(desc, "FB-20261017-023")
	if !strings.HasPrefix(final, "受理号 FB-20261017-023（凭此号对应运维侧回执") {
		t.Fatalf("receipt not first line: %q", strings.SplitN(final, "\n", 2)[0])
	}
	if !strings.Contains(final, "提交人 王运维（运维侧）") || !strings.Contains(final, "问题描述\n第 3 页白屏") {
		t.Fatalf("template fields missing:\n%s", final)
	}
	if !strings.Contains(final, "出错页面路径 /admin/patients?page=3") {
		t.Fatalf("pagePath missing")
	}
	// 占位首行不得残留
	if strings.Contains(final, describeFirstLine) {
		t.Fatalf("placeholder leaked")
	}
}

// ── 幂等 ──────────────────────────────────────────────

func TestIdemCache_WindowAndExpiry(t *testing.T) {
	c := newIdemCache()
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	c.put("k", "FB-20261017-001", 1)
	if e, ok := c.get("k"); !ok || e.receipt != "FB-20261017-001" {
		t.Fatalf("hit expected")
	}
	now = now.Add(idemWindow + time.Second) // 过窗
	if _, ok := c.get("k"); ok {
		t.Fatalf("expired must miss")
	}
}

// ── TAPD 客户端（假 TAPD 端点）─────────────────────────

func fakeTapd(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	mux := http.NewServeMux()
	mux.HandleFunc("/bugs", func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "num123" || p != "token456" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = r.ParseForm()
		calls = append(calls, r.Method+" /bugs id="+r.Form.Get("id")+" title="+r.Form.Get("title"))
		// TAPD /bugs 同端点双语义：带 id=更新，不带=创建（POST；PUT 实测 405）
		if r.Method == http.MethodPost && r.Form.Get("id") == "" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": 1,
				"data":   map[string]any{"Bug": map[string]any{"id": "300100023"}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": 1})
	})
	mux.HandleFunc("/files/upload_attachment", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = f.Close()
		calls = append(calls, "upload entry="+r.FormValue("entry_id")+" type="+r.FormValue("type"))
		_ = json.NewEncoder(w).Encode(map[string]any{"status": 1})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestTapdClient_CreateUpdateUpload(t *testing.T) {
	srv, calls := fakeTapd(t)
	c := NewTapdClient(srv.URL, "num123", "token456", "33005753", "bug_attachment")

	id, err := c.CreateBug(context.Background(), "[运维反馈] x", "desc", "triage,环境:生产")
	if err != nil || id != 300100023 {
		t.Fatalf("create: id=%d err=%v", id, err)
	}
	if err := c.UpdateDescription(context.Background(), id, "受理号 FB-…"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := c.UploadAttachment(context.Background(), id, "a.png", "image/png", []byte("png!")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	joined := strings.Join(*calls, " | ")
	if !strings.Contains(joined, "POST /bugs id= title=[运维反馈] x") ||
		!strings.Contains(joined, "POST /bugs id=300100023") || // 更新同走 POST 带 id（PUT 405 实测）
		!strings.Contains(joined, "upload entry=300100023 type=bug_attachment") {
		t.Fatalf("calls=%s", joined)
	}
}

func TestTapdClient_BadAuthNoRetryLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":0,"msg":"unauthorized"}`))
	}))
	defer srv.Close()
	c := NewTapdClient(srv.URL, "u", "wrong", "33005753", "bug_attachment")
	if _, err := c.CreateBug(context.Background(), "t", "d", "triage"); err == nil {
		t.Fatal("expected error on bad auth")
	} else if strings.Contains(err.Error(), "wrong") {
		t.Fatalf("credential leaked into error: %v", err)
	}
}

// ── SSRF 闸 ──────────────────────────────────────────

func TestValidateTapdBase(t *testing.T) {
	if err := validateTapdBase("https://api.tapd.cn", false); err != nil {
		t.Fatalf("allowlist rejected: %v", err)
	}
	bad := []string{
		"http://api.tapd.cn",       // 非 https
		"https://evil.com",         // 域外
		"https://tapd.cn.evil.com", // 后缀伪装
		"https://127.0.0.1",        // 环回（非测试路径）
		"https://10.0.0.5",         // 私网
		"https://169.254.169.254",  // 元数据
		"https://100.64.0.1",       // CGNAT
	}
	for _, b := range bad {
		if err := validateTapdBase(b, false); err == nil {
			t.Fatalf("should reject %s", b)
		}
	}
	if err := validateTapdBase("https://127.0.0.1:1", true); err != nil {
		t.Fatalf("loopback test path should pass: %v", err)
	}
}
