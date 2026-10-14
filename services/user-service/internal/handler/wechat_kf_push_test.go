// T690 微信客服进入事件推送（user_enter_tempsession 身份透传）实现侧测试。
//
// 覆盖：URL 验签挑战 GET / 验签失败 / token 未配置 fail-closed；POST XML/JSON 进入事件
// 身份下发（SessionFrom 首段 → openid 兜底两条解析链）、拒收非进入事件、发送失败 500、
// 患者解析不到不下发；以及 session-from 解析 / 身份文案纯函数。
package handler

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// fakeKFSender 记录下发（可注入失败）
type fakeKFSender struct {
	toUsers  []string
	contents []string
	err      error
}

func (f *fakeKFSender) SendKfText(_ context.Context, toUser, content string) error {
	f.toUsers = append(f.toUsers, toUser)
	f.contents = append(f.contents, content)
	return f.err
}

// kfTestSignature 独立实现验签算法（不复用被测 wxPushSignatureOK，防同错假绿）
func kfTestSignature(token, timestamp, nonce string) string {
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

type kfPushEnv struct {
	h      *Handler
	store  *fakeStore
	sender *fakeKFSender
	engine *gin.Engine
	token  string
}

func newKFPushEnv(t *testing.T) *kfPushEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := &fakeStore{}
	sender := &fakeKFSender{}
	h := &Handler{store: store, kfSender: sender}
	engine := gin.New()
	engine.GET("/api/v1/wechat/kf-push", h.wxKfPush)
	engine.POST("/api/v1/wechat/kf-push", h.wxKfPush)
	const token = "kf-token-abc"
	t.Setenv(kfPushEnvToken, token)
	return &kfPushEnv{h: h, store: store, sender: sender, engine: engine, token: token}
}

func (e *kfPushEnv) do(t *testing.T, method, query, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, "/api/v1/wechat/kf-push?"+query, nil)
	} else {
		req = httptest.NewRequest(method, "/api/v1/wechat/kf-push?"+query, strings.NewReader(body))
		req.Header.Set("Content-Type", "text/xml")
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// signedQuery 标准三参验签 query
func (e *kfPushEnv) signedQuery(timestamp, nonce string) string {
	return "signature=" + kfTestSignature(e.token, timestamp, nonce) +
		"&timestamp=" + timestamp + "&nonce=" + nonce
}

const kfEnterXML = `<xml>
<ToUserName><![CDATA[gh_x]]></ToUserName>
<FromUserName><![CDATA[oUSER123]]></FromUserName>
<CreateTime>1729000000</CreateTime>
<MsgType><![CDATA[event]]></MsgType>
<Event><![CDATA[user_enter_tempsession]]></Event>
<SessionFrom><![CDATA[P20260001|张晓宇|DEV-001]]></SessionFrom>
</xml>`

func TestT690_KFPush_VerifyChallenge(t *testing.T) {
	e := newKFPushEnv(t)

	t.Run("GET_验签通过原样回echostr", func(t *testing.T) {
		q := e.signedQuery("1729000001", "n1") + "&echostr=challenge-123"
		rec := e.do(t, http.MethodGet, q, "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "challenge-123", rec.Body.String())
	})

	t.Run("GET_签名错误403", func(t *testing.T) {
		rec := e.do(t, http.MethodGet, "signature=deadbeef&timestamp=1&nonce=n", "")
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("GET_缺signature403", func(t *testing.T) {
		rec := e.do(t, http.MethodGet, "timestamp=1&nonce=n", "")
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestT690_KFPush_TokenUnset_FailClosed(t *testing.T) {
	e := newKFPushEnv(t)
	t.Setenv(kfPushEnvToken, " ") // 覆盖为空白 → 整端点拒收
	rec := e.do(t, http.MethodPost, e.signedQuery("1", "n"), kfEnterXML)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Empty(t, e.sender.toUsers)
}

func TestT690_KFPush_EnterEvent_XML_SessionFrom解析成功(t *testing.T) {
	e := newKFPushEnv(t)
	dev := "DEV-001"
	e.store.patient = &repo.PatientRow{PatientID: "P20260001", Name: "张晓宇", DeviceID: &dev}

	rec := e.do(t, http.MethodPost, e.signedQuery("2", "n2"), kfEnterXML)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "success", rec.Body.String())
	require.Len(t, e.sender.toUsers, 1)
	assert.Equal(t, "oUSER123", e.sender.toUsers[0])
	assert.Equal(t, "患者档案：张晓宇（P20260001）· 设备 DEV-001", e.sender.contents[0])
	assert.Equal(t, "P20260001", e.store.lastPatientQuery, "SessionFrom 首段应作为患者ID查询")
}

func TestT690_KFPush_EnterEvent_SessionFrom异常_openid兜底(t *testing.T) {
	e := newKFPushEnv(t)
	// SessionFrom 缺患者段 → 走 wx_openid 反查（PRD 原文兜底）
	dev := "DEV-9"
	e.store.patient = &repo.PatientRow{PatientID: "P20260009", Name: "李患者", DeviceID: &dev}
	e.store.wxPatientByOpenID = map[string]*repo.PatientLoginRow{
		"oUSER123": {PatientID: "P20260009", Name: "李患者"},
	}
	body := strings.Replace(kfEnterXML, "P20260001|张晓宇|DEV-001", "|只有分隔符", 1)

	rec := e.do(t, http.MethodPost, e.signedQuery("3", "n3"), body)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, e.sender.toUsers, 1)
	assert.Equal(t, "oUSER123", e.sender.toUsers[0])
	assert.Equal(t, "患者档案：李患者（P20260009）· 设备 DEV-9", e.sender.contents[0])
}

func TestT690_KFPush_EnterEvent_患者解析不到_不下发但200(t *testing.T) {
	e := newKFPushEnv(t)
	// store.patient 置空：SessionFrom 患者查无、openid 反查也无 → 无可对上人，不下发
	rec := e.do(t, http.MethodPost, e.signedQuery("4", "n4"), kfEnterXML)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "success", rec.Body.String())
	assert.Empty(t, e.sender.toUsers, "解析不到患者时不得下发任何身份消息")
}

func TestT690_KFPush_NonEnterEvent_忽略(t *testing.T) {
	e := newKFPushEnv(t)
	e.store.patient = &repo.PatientRow{PatientID: "P20260001", Name: "张晓宇"}
	body := strings.Replace(kfEnterXML, "user_enter_tempsession", "kf_msg_or_other", 1)

	rec := e.do(t, http.MethodPost, e.signedQuery("5", "n5"), body)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, e.sender.toUsers, "非进入事件不得触发下发")
}

func TestT690_KFPush_JSONBody_同样受理(t *testing.T) {
	e := newKFPushEnv(t)
	e.store.patient = &repo.PatientRow{PatientID: "P20260001", Name: "张晓宇"}
	body := `{"ToUserName":"gh_x","FromUserName":"oUSER123","MsgType":"event",` +
		`"Event":"user_enter_tempsession","SessionFrom":"P20260001"}`

	rec := e.do(t, http.MethodPost, e.signedQuery("6", "n6"), body)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, e.sender.toUsers, 1)
	assert.Equal(t, "患者档案：张晓宇（P20260001）", e.sender.contents[0], "无设备号时不拼设备段")
}

func TestT690_KFPush_加密体_不处理回success(t *testing.T) {
	e := newKFPushEnv(t)
	body := `<xml><ToUserName><![CDATA[gh_x]]></ToUserName><MsgType><![CDATA[event]]></MsgType>` +
		`<Encrypt><![CDATA[abcdef]]></Encrypt></xml>`

	rec := e.do(t, http.MethodPost, e.signedQuery("7", "n7"), body)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "success", rec.Body.String())
	assert.Empty(t, e.sender.toUsers, "安全模式加密体不解密不下发")
}

func TestT690_KFPush_签名错误_POST同样403(t *testing.T) {
	e := newKFPushEnv(t)
	e.store.patient = &repo.PatientRow{PatientID: "P20260001", Name: "张晓宇"}

	rec := e.do(t, http.MethodPost, "signature=bad&timestamp=1&nonce=n", kfEnterXML)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, e.sender.toUsers, "验签失败不得触发下发")
}

func TestT690_KFPush_发送失败_500让微信重试(t *testing.T) {
	e := newKFPushEnv(t)
	e.store.patient = &repo.PatientRow{PatientID: "P20260001", Name: "张晓宇"}
	e.sender.err = assert.AnError

	rec := e.do(t, http.MethodPost, e.signedQuery("8", "n8"), kfEnterXML)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestT690_KFPush_Sender未配置_不panic回success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeStore{patient: &repo.PatientRow{PatientID: "P20260001", Name: "张晓宇"}}
	h := &Handler{store: store} // kfSender 未注入
	engine := gin.New()
	engine.POST("/api/v1/wechat/kf-push", h.wxKfPush)
	t.Setenv(kfPushEnvToken, "kf-token-abc")

	sig := kfTestSignature("kf-token-abc", "9", "n9")
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/wechat/kf-push?signature="+sig+"&timestamp=9&nonce=n9", strings.NewReader(kfEnterXML))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "success", rec.Body.String())
}

// ── 纯函数 ─────────────────────────────────────────────────────────

func TestT690_FirstSessionFromSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"P20260001|张晓宇|DEV-1", "P20260001"},
		{" P001 |名 ", "P001"},
		{"P001", "P001"},
		{"P001｜全角分隔", "P001"},
		{"", ""},
		{"   ", ""},
		{"|nodata", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, firstSessionFromSegment(c.in), "in=%q", c.in)
	}
}

func TestT690_KfIdentityText(t *testing.T) {
	dev := "BSYNC-701001"
	withDev := &repo.PatientRow{PatientID: "P20260001", Name: "林小雨", DeviceID: &dev}
	assert.Equal(t, "患者档案：林小雨（P20260001）· 设备 BSYNC-701001", kfIdentityText(withDev))

	emptyDev := ""
	noDev := &repo.PatientRow{PatientID: "P20260002", Name: "王小明", DeviceID: &emptyDev}
	assert.Equal(t, "患者档案：王小明（P20260002）", kfIdentityText(noDev))

	nilDev := &repo.PatientRow{PatientID: "P20260003", Name: "赵小红"}
	assert.Equal(t, "患者档案：赵小红（P20260003）", kfIdentityText(nilDev))
}
