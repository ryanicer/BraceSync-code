package seed

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// 夹具派生口径（T586）：密钥、明文、nonce 全部由下面三颗种子串算出来，
// 复算路径见 scripts/db/seed/README.md。
const fixtureKeySeed = "bracesync:T586:seed-fixture-key"
const fixturePlainSeed = "bracesync:T586:dev-fixture:"
const fixtureNonceSeed = "bracesync:T586:nonce:"

// minCipherLen = 12 字节 nonce + 16 字节 tag；短于此的字节串任何密钥都解不出来。
const minCipherLen = 28

const placeholderLiteral = "'\\x00'::bytea"
const emptyByteaLiteral = "''::bytea"

var t586DeviceIDs = []string{
	"PRS-ML05-RC-20260701001",
	"PRS-ML05-RC-20260701002",
	"PRS-ML05-RC-20260701003",
	"PRS-ML05-RC-20260701004",
	"PRS-ML05-RC-20260701005",
}

var hexByteaRe = regexp.MustCompile(`'\\x([0-9a-fA-F]+)'::bytea`)
var anyHexByteaRe = regexp.MustCompile(`'\\x[0-9a-fA-F]+'::bytea`)

func seedSQLText(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 定位不到本文件")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "scripts", "db", "seed", "seed.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 seed.sql（%s）：%v", path, err)
	}
	return string(raw)
}

func splitLines(content string) []string {
	return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
}

// hexByteaLiterals 按文件出现顺序返回所有 '\x…'::bytea 字面量的十六进制体。
func hexByteaLiterals(content string) []string {
	found := hexByteaRe.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(found))
	for _, m := range found {
		out = append(out, strings.ToLower(m[1]))
	}
	return out
}

// byteaLiteralsShorterThanNonce 是这把尺的牙：抓「永远解不出来」的那些字节串。
func byteaLiteralsShorterThanNonce(content string) []string {
	var bad []string
	for _, h := range hexByteaLiterals(content) {
		raw, err := hex.DecodeString(h)
		if err != nil {
			bad = append(bad, "undecodable("+strconv.Itoa(len(h))+")")
			continue
		}
		if len(raw) < minCipherLen {
			bad = append(bad, "len="+strconv.Itoa(len(raw)))
		}
	}
	return bad
}

// byteaLiteralSource 把十六进制体还原成 SQL 文本里那一格字面量。
func byteaLiteralSource(hexBody string) string {
	return "'\\x" + hexBody + "'::bytea"
}

// locateLiteral 返回（行号, 该行, 全文件命中次数）；行号 0 表示不在场。
func locateLiteral(content, needle string) (int, string, int) {
	total := strings.Count(content, needle)
	for n, line := range splitLines(content) {
		if strings.Contains(line, needle) {
			return n + 1, line, total
		}
	}
	return 0, "", total
}

// injectOnePlaceholder 在内存副本上把第一颗密文换回 1 字节占位（注牙）。
func injectOnePlaceholder(t *testing.T, content string) string {
	t.Helper()
	bodies := hexByteaLiterals(content)
	if len(bodies) == 0 {
		t.Fatal("注牙目标不在场：文本里没有十六进制密文")
	}
	needle := byteaLiteralSource(bodies[0])
	if !strings.Contains(content, needle) {
		t.Fatalf("注牙目标串没能在文本里定位（长度 %d）", len(needle))
	}
	return strings.Replace(content, needle, placeholderLiteral, 1)
}

func fixtureKeyHex() string {
	sum := sha256.Sum256([]byte(fixtureKeySeed))
	return hex.EncodeToString(sum[:])
}

func fixturePlainHex(deviceID string) string {
	sum := sha256.Sum256([]byte(fixturePlainSeed + deviceID))
	return hex.EncodeToString(sum[:])
}

func fixtureNonce(deviceID string) []byte {
	sum := sha256.Sum256([]byte(fixtureNonceSeed + deviceID))
	return sum[:12]
}

func gcmFor(t *testing.T, keyHex string) cipher.AEAD {
	t.Helper()
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatalf("夹具密钥不是十六进制：%v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher：%v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM：%v", err)
	}
	return gcm
}

// decryptBytes 与 services/device-service/internal/crypto 的 AES-256-GCM 口径一致：
// 密文格 = nonce ‖ 密文 ‖ tag，nonce 取前 12 字节。
func decryptBytes(gcm cipher.AEAD, raw []byte) (string, error) {
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("密文短于 nonce，解不出来")
	}
	plain, err := gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func decryptHex(gcm cipher.AEAD, hexBody string) (string, error) {
	raw, err := hex.DecodeString(hexBody)
	if err != nil {
		return "", err
	}
	return decryptBytes(gcm, raw)
}

func TestT586_SeedTextHasNoUnsolvablePlaceholder(t *testing.T) {
	content := seedSQLText(t)

	placeholders := strings.Count(content, placeholderLiteral)
	short := byteaLiteralsShorterThanNonce(content)
	bodies := hexByteaLiterals(content)
	empties := strings.Count(content, emptyByteaLiteral)
	t.Logf("seed.sql 文本面：占位字面量 %d · 短于 nonce 的密文 %d · 十六进制密文 %d · 空 bytea %d",
		placeholders, len(short), len(bodies), empties)

	if placeholders != 0 {
		t.Fatalf("seed.sql 仍有 %d 处 1 字节占位密钥", placeholders)
	}
	if len(short) != 0 {
		t.Fatalf("seed.sql 有 %d 处密文短于 nonce：%s", len(short), strings.Join(short, " "))
	}

	injected := injectOnePlaceholder(t, content)
	if injected == content {
		t.Fatal("注牙没改到任何字节，这把尺没被真正跑过")
	}
	badAfter := byteaLiteralsShorterThanNonce(injected)
	phAfter := strings.Count(injected, placeholderLiteral)
	t.Logf("注牙副本：占位字面量 %d · 短于 nonce 的密文 %d", phAfter, len(badAfter))
	if phAfter != 1 || len(badAfter) != 1 {
		t.Fatalf("守卫没牙：注牙后应各读到 1，实得 占位 %d · 短密文 %d", phAfter, len(badAfter))
	}
}

func TestT586_SeedDeviceCiphersDecryptToDocumentedPlaintext(t *testing.T) {
	content := seedSQLText(t)
	bodies := hexByteaLiterals(content)
	if len(bodies) != len(t586DeviceIDs) {
		t.Fatalf("十六进制密文 %d 颗，期望 %d 颗（5 台夹具设备）", len(bodies), len(t586DeviceIDs))
	}

	gcm := gcmFor(t, fixtureKeyHex())
	for i, deviceID := range t586DeviceIDs {
		needle := byteaLiteralSource(bodies[i])
		lineNo, line, hits := locateLiteral(content, needle)
		if hits != 1 {
			t.Fatalf("%s 的密文格在文本里命中 %d 次", deviceID, hits)
		}
		if lineNo == 0 || !strings.Contains(line, deviceID) {
			t.Fatalf("第 %d 行的密文格不在 %s 这一行上", lineNo, deviceID)
		}
		raw, err := hex.DecodeString(bodies[i])
		if err != nil {
			t.Fatalf("%s 密文格不是合法十六进制：%v", deviceID, err)
		}
		if len(raw) != 92 {
			t.Fatalf("%s 密文字节长度 %d，期望 92（12 nonce + 64 密文 + 16 tag）", deviceID, len(raw))
		}
		plain, err := decryptHex(gcm, bodies[i])
		if err != nil {
			t.Fatalf("%s 用夹具钥解密失败：%v", deviceID, err)
		}
		want := fixturePlainHex(deviceID)
		if plain != want {
			t.Fatalf("%s 明文不符：得 %d 位，期望 %d 位", deviceID, len(plain), len(want))
		}
		// 确定性：同一把夹具钥 + 文档化明文 + 文档化 nonce 必须重算出文本里那一格字节
		// （密文格 = nonce ‖ 密文 ‖ tag，所以重算值要带上 nonce 再比）。
		nonce := fixtureNonce(deviceID)
		sealed := gcm.Seal(nil, nonce, []byte(want), nil)
		whole := append(append([]byte{}, nonce...), sealed...)
		if got := hex.EncodeToString(whole); got != bodies[i] {
			t.Fatalf("%s 重加密不等值：文本里那格不是夹具钥加文档化 nonce 算出来的", deviceID)
		}
	}
}

func TestT586_OnlyDeviceRowsCarryCiphertext(t *testing.T) {
	content := seedSQLText(t)

	empties := strings.Count(content, emptyByteaLiteral)
	bodies := hexByteaLiterals(content)
	t.Logf("空 bytea %d 处 · 十六进制密文 %d 颗（医生 3 + 技师 3 + 患者 5 = 11 处手机号列）", empties, len(bodies))
	if empties < 11 {
		t.Fatalf("空 bytea 只有 %d 处，少于既登记的 11 处手机号列", empties)
	}

	for n, line := range splitLines(content) {
		if anyHexByteaRe.MatchString(line) && !strings.Contains(line, "PRS-ML05-RC-") {
			t.Fatalf("第 %d 行在非设备行上带密文（手机号列必须写空 bytea）", n+1)
		}
		if strings.Contains(line, emptyByteaLiteral) && strings.Contains(line, "PRS-ML05-RC-") {
			t.Fatalf("第 %d 行：设备行的密钥列被写成了空 bytea", n+1)
		}
	}
}
