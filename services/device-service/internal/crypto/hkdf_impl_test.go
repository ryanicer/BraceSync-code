// Package crypto — HKDF-SHA256 配网密钥派生测试（T067；独立预计算向量，非真实 secret）
package crypto

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeriveProvisionKey_KnownVector 独立预计算向量（Python hashlib/hmac HKDF-SHA256）。
// ikm=00112233...eeff(32B) salt=nil(→32 zeros) info="provision"+"DEV-TEST-001" length=16
// 期望 OKM = 5a4be7aaa70c9413ad06fcd2f94a2a9a
func TestDeriveProvisionKey_KnownVector(t *testing.T) {
	ikm, err := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	require.NoError(t, err)
	wantHex := "5a4be7aaa70c9413ad06fcd2f94a2a9a"

	got, err := DeriveProvisionKey(ikm, "DEV-TEST-001")
	require.NoError(t, err)
	assert.Equal(t, wantHex, hex.EncodeToString(got), "HKDF 派生须匹配独立预计算向量")
	assert.Len(t, got, 16, "provision key 必须 16 字节（→32 hex）")
}

func TestDeriveProvisionKey_Deterministic(t *testing.T) {
	ikm, err := hex.DecodeString("aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")
	require.NoError(t, err)

	k1, err := DeriveProvisionKey(ikm, "DEV-X")
	require.NoError(t, err)
	k2, err := DeriveProvisionKey(ikm, "DEV-X")
	require.NoError(t, err)

	assert.Equal(t, k1, k2, "相同输入应确定性输出")
}

func TestDeriveProvisionKey_DifferentDeviceID(t *testing.T) {
	ikm, err := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	require.NoError(t, err)

	k1, err := DeriveProvisionKey(ikm, "DEV-A")
	require.NoError(t, err)
	k2, err := DeriveProvisionKey(ikm, "DEV-B")
	require.NoError(t, err)

	assert.NotEqual(t, k1, k2, "不同 device_id 应派生不同密钥")
}

func TestDeriveProvisionKey_DifferentSecret(t *testing.T) {
	ikmA, _ := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	ikmB, _ := hex.DecodeString("ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100")

	k1, _ := DeriveProvisionKey(ikmA, "DEV-SAME")
	k2, _ := DeriveProvisionKey(ikmB, "DEV-SAME")
	assert.NotEqual(t, k1, k2, "不同 secret 应派生不同密钥")
}
