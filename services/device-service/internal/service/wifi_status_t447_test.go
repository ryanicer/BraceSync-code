// T447：service 层的 wifi_status 枚举校验「排在写库之前」用例。
//
// 与 handler 那份（internal/handler/wifi_status_t447_test.go）的分工：
// handler 那份测的是 HTTP 形状（400/403/落库值），本份测的是这一条机制本身 ——
// 校验必须在 store.UpdateInstallMeta 之前，否则脏值会穿到 SQL、由
// install_records_wifi_status_check 报 23514，再经 mapRepoErr 变成 500：
// 一个「请求写错了」的客户端问题被伪装成服务端故障（T396 同一坑，同一修法）。
//
// 计数字段是本文件的全部价值：断言「返回 400」在任何 store 上都能过，
// 只有「UpdateInstallMeta 调用次数为 0」才分得开「校验前移」与「校验没接上」。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/crypto"
	"github.com/bracesync/bracesync/services/device-service/internal/model"
	"github.com/bracesync/bracesync/services/device-service/internal/testutil"
)

// t447CountStore 只加一个计数器，其余方法全部走 FakeStore 原实现（含「不校验四值」那条）
type t447CountStore struct {
	*testutil.FakeStore
	metaCalls int
	lastArgs  []string
}

func (s *t447CountStore) UpdateInstallMeta(ctx context.Context, installID int64, notes, signatureURL, wifiStatus *string) error {
	s.metaCalls++
	arg := []string{"", "", ""}
	if notes != nil {
		arg[0] = *notes
	}
	if signatureURL != nil {
		arg[1] = *signatureURL
	}
	if wifiStatus != nil {
		arg[2] = *wifiStatus
	}
	s.lastArgs = arg
	return s.FakeStore.UpdateInstallMeta(ctx, installID, notes, signatureURL, wifiStatus)
}

func t447Svc(t *testing.T) (*DeviceService, *t447CountStore, int64) {
	t.Helper()
	enc, err := crypto.NewEncryptor(testutil.TestEncKey)
	require.NoError(t, err)
	store := &t447CountStore{FakeStore: testutil.NewFakeStore()}
	svc := NewDeviceService(store, enc)

	registerAndPatient(t, svc, store.FakeStore, "DEV-T447", "P-T447")
	store.AddTech("TECH-T447")
	ctx := context.Background()
	_, appErr := svc.Bind(ctx, "DEV-T447", "P-T447", "TECH-T447", false)
	require.Nil(t, appErr)
	rec, appErr := svc.CreateInstall(ctx, &CreateInstallRequest{
		DeviceID: "DEV-T447", PatientID: "P-T447", TechID: "TECH-T447",
	})
	require.Nil(t, appErr)
	return svc, store, rec.InstallID
}

// ── 1. 集合外取值：400 且仓储一次没被调用 ─────────────────────────────

func TestT447_UpdateInstallMeta_OutsideEnumRejectsBeforeStore(t *testing.T) {
	for _, bad := range []string{"CONNECTED", "Skipped", "skip", "configuring", "已连接", "unknown", " ", " failed"} {
		t.Run(bad, func(t *testing.T) {
			svc, store, id := t447Svc(t)
			v := bad
			notes := "同请求里的备注"

			appErr := svc.UpdateInstallMeta(context.Background(), id, &notes, nil, &v)

			require.NotNil(t, appErr, "wifiStatus=%q 竟被当成合法值", v)
			assert.Equal(t, model.CodeInvalidParam, appErr.Code)
			assert.Equal(t, 400, appErr.HTTPStatus)
			assert.Zero(t, store.metaCalls, "校验必须排在 store.UpdateInstallMeta 之前：落一次就把脏值写进库里，"+
				"现网会由 CHECK 报 23514 并被兜成 500")
			assert.Equal(t, model.WifiStatusUnconfigured, mustInstall(t, store, id).WifiStatus)
		})
	}
}

// ── 2. 反证：枚举内四值恰好落一次，且参数原样透传到仓储 ────────────────

func TestT447_UpdateInstallMeta_EnumValuesReachStoreOnce(t *testing.T) {
	for _, good := range []string{
		model.WifiStatusConnected,
		model.WifiStatusUnconfigured,
		model.WifiStatusFailed,
		model.WifiStatusSkipped,
	} {
		t.Run(good, func(t *testing.T) {
			svc, store, id := t447Svc(t)
			v := good

			appErr := svc.UpdateInstallMeta(context.Background(), id, nil, nil, &v)

			require.Nil(t, appErr, "枚举内取值 %q 被拦 = 打断四态通路", v)
			assert.Equal(t, 1, store.metaCalls, "应恰好透传一次，不在 service 层重复落库")
			assert.Equal(t, []string{"", "", v}, store.lastArgs, "指针必须原样下传，service 层不改写取值")
			assert.Equal(t, v, mustInstall(t, store, id).WifiStatus)
		})
	}
}

// ── 3. nil = 该列不改：透传的是 nil，不是默认值 ────────────────────────

func TestT447_UpdateInstallMeta_NilPointerPassedThrough(t *testing.T) {
	svc, store, id := t447Svc(t)
	notes := "只回填备注"

	require.Nil(t, svc.UpdateInstallMeta(context.Background(), id, &notes, nil, nil))

	require.Equal(t, 1, store.metaCalls)
	assert.Equal(t, []string{notes, "", ""}, store.lastArgs, "wifiStatus 为 nil 时不能替它填任何默认值，"+
		"否则 COALESCE 语义会变成「每次都把该列写回 unconfigured」")
	assert.Equal(t, model.WifiStatusUnconfigured, mustInstall(t, store, id).WifiStatus)
}

func mustInstall(t *testing.T, store *t447CountStore, id int64) *model.InstallRecord {
	t.Helper()
	rec, err := store.GetInstall(context.Background(), id)
	require.NoError(t, err)
	return rec
}
