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

// TestDeriveProvisionKey_FirmwareVector 与固件共享的 HKDF 测试向量（T091 补录 A-3）。
//
// 固件约定：HKDF ikm = device_secret 的 64 字符 hex **ASCII 字节**（64B），
// 与云端 GetProvisionKey 调用 []byte(secret) 形态一致；不是 hex 解码后的 32B。
// 本用例断言 (device_secret_hex_str, device_id) → provision_key_hex 的已知值，
// 作为三端（固件/云端/未来复算工具）对齐铁证。
//
// TODO：向量待 PM 向小顾索取固件实测值后填入 wantHex 并删除 t.Skip。
func TestDeriveProvisionKey_FirmwareVector(t *testing.T) {
	t.Skip("await hardware test vector from PM (小顾固件实测值, device_secret+device_id→provision_key_hex)")

	// ikm 以 64 字符 hex 字符串的 ASCII 字节传入（对齐生产 []byte(secret)）
	const secretHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	const deviceID = "DEV-FIRMWARE-001"
	wantHex := "<待小顾固件实测值填入>"

	got, err := DeriveProvisionKey([]byte(secretHex), deviceID)
	require.NoError(t, err)
	assert.Equal(t, wantHex, hex.EncodeToString(got), "HKDF 派生须匹配固件实测向量")
}
