package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSubmit_EndToEnd 假 TAPD 全流程（卡验收判据的可本地复算形）：
// meta+2 图提交 → 建单（[运维反馈]前缀+triage/环境/分类标签）→ 受理号回填首行 →
// 两附件挂上 → 返受理号；同键重复提交=同受理号（幂等，零新建单）。
func TestSubmit_EndToEnd(t *testing.T) {
	type rec struct {
		method, path, title, labels, desc string
		uploads                           []string
	}
	var got rec
	mux := http.NewServeMux()
	mux.HandleFunc("/bugs", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Method == http.MethodPost {
			got.method, got.path, got.title, got.labels = "POST", "/bugs", r.Form.Get("title"), r.Form.Get("labels")
			got.desc = r.Form.Get("description")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": 1,
				"data": map[string]any{"Bug": map[string]any{"id": "300100023"}}})
			return
		}
		got.desc = r.Form.Get("description") // PUT 回填后覆盖（首行=受理号）
		_ = json.NewEncoder(w).Encode(map[string]any{"status": 1})
	})
	mux.HandleFunc("/files/upload_attachment", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(4 << 20)
		f, hdr, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(400)
			return
		}
		b, _ := io.ReadAll(f)
		_ = f.Close()
		got.uploads = append(got.uploads, hdr.Filename+"="+string(b))
		_ = json.NewEncoder(w).Encode(map[string]any{"status": 1})
	})
	fake := httptest.NewServer(mux)
	defer fake.Close()

	h, err := NewHandler(NewTapdClient(fake.URL, "n", "t", "33005753", "bug_attachment"), Page(), slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if err != nil {
		t.Fatal(err)
	}

	send := func(dup bool) (*httptest.ResponseRecorder, string) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		meta, _ := json.Marshal(submitReq{
			Env: "prod", Title: "患者列表白屏", Category: "页面报错/白屏",
			Description: "第 3 页白屏，复现两次。", Submitter: "王运维",
		})
		_ = mw.WriteField("meta", string(meta))
		if !dup { // 幂等重复投模拟：首投的两图不再重发（服务端命中缓存不应触达 TAPD）
			for _, f := range [][2]string{{"a.png", "pngdata"}, {"b.mp4", "mp4data"}} {
				fw, _ := mw.CreateFormFile("file", f[0])
				_, _ = fw.Write([]byte(f[1]))
			}
		}
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/feedback", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		var out struct {
			Code int `json:"code"`
			Data struct {
				Receipt string   `json:"receipt"`
				BugID   int64    `json:"bugId"`
				Failed  []string `json:"failedFiles"`
			} `json:"data"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		if out.Code != 0 {
			t.Fatalf("submit code!=0: %s", rr.Body.String())
		}
		return rr, out.Data.Receipt
	}

	rr, receipt := send(false)
	if rr.Code != 200 || receipt != "FB-20261017-023" {
		t.Fatalf("first: http=%d receipt=%s", rr.Code, receipt)
	}
	if got.title != "[运维反馈] 患者列表白屏" {
		t.Fatalf("title=%q", got.title)
	}
	if !strings.Contains(got.labels, "triage") || !strings.Contains(got.labels, "环境:生产") || !strings.Contains(got.labels, "分类:页面报错/白屏") {
		t.Fatalf("labels=%q", got.labels)
	}
	if !strings.HasPrefix(got.desc, "受理号 FB-20261017-023（凭此号对应运维侧回执") {
		t.Fatalf("receipt not backfilled first line: %q", strings.SplitN(got.desc, "\n", 2)[0])
	}
	if len(got.uploads) != 2 || got.uploads[0] != "a.png=pngdata" || got.uploads[1] != "b.mp4=mp4data" {
		t.Fatalf("uploads=%v", got.uploads)
	}

	// 幂等重投（同 env+标题+提交人）：同受理号、零新建单、零附件重传。
	bugsBefore := got.method
	_, receipt2 := send(true)
	if receipt2 != receipt {
		t.Fatalf("idem receipt %s != %s", receipt2, receipt)
	}
	if got.method != bugsBefore || len(got.uploads) != 2 {
		t.Fatalf("idem hit must not touch tapd again: uploads=%d", len(got.uploads))
	}
}

// TestSubmit_Validation400 不触达 TAPD（D-2 步1：不通过直接 400）。
func TestSubmit_Validation400(t *testing.T) {
	var hit bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer fake.Close()
	h, err := NewHandler(NewTapdClient(fake.URL, "n", "t", "33005753", "bug_attachment"), Page(), slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("meta", `{"env":"test","title":"x","category":"其他","description":"y","submitter":"z"}`) // env 越白名单
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/feedback", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || hit {
		t.Fatalf("http=%d tapdTouched=%v", rr.Code, hit)
	}
}

// TestStaticPage_Served 静态页与 healthz 托管。
func TestStaticPage_Served(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer fake.Close()
	h, err := NewHandler(NewTapdClient(fake.URL, "n", "t", "33005753", "bug_attachment"), Page(), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "问题反馈") {
		t.Fatalf("page http=%d", rr.Code)
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr2.Code != 200 {
		t.Fatalf("healthz http=%d", rr2.Code)
	}
}
