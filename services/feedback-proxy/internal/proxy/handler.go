package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Handler 代理 service HTTP 处理器（静态页 + /api/feedback + /healthz）。
type Handler struct {
	tapd  *tapdClient
	idem  *idemCache
	page  []byte
	clock func() time.Time
	log   *slog.Logger
}

// NewHandler 组装 Handler；allowLoopback 仅单测指向本地 fake TAPD。
func NewHandler(tapd *tapdClient, page []byte, logger *slog.Logger, allowLoopback bool) (*Handler, error) {
	if err := validateTapdBase(tapd.base, allowLoopback); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{tapd: tapd, idem: newIdemCache(), page: page, clock: time.Now, log: logger}, nil
}

// jsonResp 统一响应壳（与全系服务 code/message/data 同形）。
type jsonResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, httpStatus int, body jsonResp) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(body)
}

// fail400 只回短因，不回显请求正文（日志纪律同：不落描述/图片内容）。
func fail400(w http.ResponseWriter, reason string) {
	writeJSON(w, http.StatusBadRequest, jsonResp{Code: 400, Message: reason})
}

// ServeHTTP 路由：GET /healthz · GET /（静态页）· POST /api/feedback。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	case r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/index.html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(h.page)
	case r.Method == http.MethodPost && r.URL.Path == "/api/feedback":
		h.submit(w, r)
	default:
		http.NotFound(w, r)
	}
}

// feedbackResp 建单成功响应：受理号 + bug id + 部分附件失败清单（仍返号，D-2 步5）。
type feedbackResp struct {
	Receipt string   `json:"receipt"`
	BugID   int64    `json:"bugId"`
	Failed  []string `json:"failedFiles,omitempty"`
}

// submit 收单主流程（D-2 时序）：校验 → 幂等 → 建缺陷 → 回填受理号 → 逐附件转发。
func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	// 总量闸：9 张 × 150MB + meta 1MB（前端页另按 10MB/张 UX 收紧）。
	r.Body = http.MaxBytesReader(w, r.Body, maxFiles*maxFileBytes+(1<<20))

	mr, err := r.MultipartReader()
	if err != nil {
		fail400(w, "multipart 格式错误")
		return
	}

	// 第一部件必须是 meta（流式顺序依赖：建单后才收附件，附件需 bug id）。
	metaPart, err := mr.NextPart()
	if err != nil || metaPart.FormName() != "meta" {
		fail400(w, "第一部件必须是 meta")
		return
	}
	var req submitReq
	if err := json.NewDecoder(io.LimitReader(metaPart, 1<<20)).Decode(&req); err != nil {
		fail400(w, "meta JSON 解析失败")
		return
	}
	_ = metaPart.Close()
	if err := req.validate(); err != nil {
		fail400(w, err.Error())
		return
	}

	// 幂等（D-2：同标题+提交人+数分钟窗 → 同受理号；命中不再触达 TAPD）。
	key := idemKey(req)
	if e, ok := h.idem.get(key); ok {
		writeJSON(w, http.StatusOK, jsonResp{Code: 0, Message: "duplicate", Data: feedbackResp{Receipt: e.receipt, BugID: e.bugID}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// 步2 建缺陷（受理号未知 ⇒ 先占位首行，步3 回填）。
	bugID, err := h.tapd.CreateBug(ctx, bugTitle(req.Title), buildDescription(req, describeFirstLine), labelsFor(req.Env, req.Category))
	if err != nil {
		h.log.Error("create bug failed", "err", err.Error()) // 无远端残留：建单失败整体失败
		writeJSON(w, http.StatusBadGateway, jsonResp{Code: 502, Message: "TAPD 建单失败，请重试（内容在前端保留）"})
		return
	}
	receipt := Receipt(bugID, h.clock())

	// 步3 受理号回填缺陷描述首段；失败重试一次，仍失败只记日志（受理号已可返，按 bug id 可定位）。
	desc := replaceReceipt(buildDescription(req, describeFirstLine), receipt)
	if err := h.tapd.UpdateDescription(ctx, bugID, desc); err != nil {
		if retryErr := h.tapd.UpdateDescription(ctx, bugID, desc); retryErr != nil {
			h.log.Warn("receipt backfill failed", "bugId", bugID, "receipt", receipt, "err", retryErr.Error())
		}
	}
	h.idem.put(key, receipt, bugID)
	// 日志纪律（D-2）：只出 bug id 与受理号，不落描述正文/提交人/环境正文。
	h.log.Info("bug created", "bugId", bugID, "receipt", receipt)

	// 步4 逐文件串行转发（内存单件 ≤150MB，失败文件记名继续，其余照传）。
	var failed []string
	var fileCount int
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 半途读坏：已有远端单，返回受理号+失败标记（前端凭号可补图）。
			h.log.Warn("multipart read aborted", "bugId", bugID, "err", err.Error())
			failed = append(failed, "(读取中断)")
			break
		}
		name := p.FormName()
		if name != "file" {
			_ = p.Close()
			continue
		}
		fileCount++
		if fileCount > maxFiles {
			_ = p.Close()
			failed = append(failed, fmt.Sprintf("%s(超出9张上限未传)", p.FileName()))
			continue
		}
		fname := sanitizeName(p.FileName())
		data, err := readCap(p, maxFileBytes)
		_ = p.Close()
		if err != nil {
			failed = append(failed, fname+"(读取失败或超150MB)")
			continue
		}
		if err := h.tapd.UploadAttachment(ctx, bugID, fname, p.Header.Get("Content-Type"), data); err != nil {
			h.log.Warn("attach failed", "bugId", bugID, "file", fname, "bytes", len(data), "err", err.Error())
			failed = append(failed, fname+"(上传失败)")
			continue
		}
		h.log.Info("attached", "bugId", bugID, "file", fname, "bytes", len(data))
	}

	writeJSON(w, http.StatusOK, jsonResp{Code: 0, Message: "success", Data: feedbackResp{Receipt: receipt, BugID: bugID, Failed: failed}})
}

// idemKey 幂等键：env+标题+提交人（sha256 截段，不把正文直接当 map 键）。
func idemKey(r submitReq) string {
	sum := sha256.Sum256([]byte(r.Env + "\x00" + r.Title + "\x00" + r.Submitter))
	return hex.EncodeToString(sum[:16])
}

// readCap 读部件到内存，超上限即中（单件 150MB 防内存无界）。
func readCap(r io.Reader, cap int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, cap+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > cap {
		return nil, errors.New("over size cap")
	}
	return data, nil
}

// sanitizeName 文件名净化：去路径分隔符/控制符，截长（不改扩展名判定用的原名已在前）。
func sanitizeName(n string) string {
	n = strings.ReplaceAll(n, "\\", "_")
	n = strings.ReplaceAll(n, "/", "_")
	n = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, n)
	if len(n) > 100 {
		n = n[:100]
	}
	if n == "" {
		n = "file"
	}
	return n
}
