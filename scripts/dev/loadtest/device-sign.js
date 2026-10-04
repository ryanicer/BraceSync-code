// BraceSync k6 压测 — 设备上报签名工具
// 口径来源（三处必须一致，T566 格 3）：
//   路径：scripts/dev/device-simulator/cmd/simulator.go 的 PathSingle / PathBatch
//         （= services/gateway/cmd/server/proxy_services.go 的 deviceReportRoutes 挂上 /api/v1 后）
//   签名串：services/gateway/internal/auth/types.go 的 BuildSignString
//         = scripts/dev/device-simulator/cmd/sign.go 的 SignDeviceRequest（6 行 \n，无尾换行）
// 用法：k6 run <脚本>.js -e BASE_URL=http://localhost:8080 -e DEVICE_SECRET=<设备密钥>
import crypto from 'k6/crypto';

export const PATH_SINGLE = '/api/v1/device/records';
export const PATH_BATCH = '/api/v1/device/records/batch';

// DEFAULT_SECRET 与模拟器 flag -secret 的默认值同值（仓内已有的占位串，不是真实密钥）；
// 真实密钥一律用 -e DEVICE_SECRET=... 注入，不写进仓。
export const DEFAULT_SECRET = 'test_secret';

export function deviceSecret() {
  return __ENV.DEVICE_SECRET || DEFAULT_SECRET;
}

// randomNonceHex 返回 32 字符 hex（16 随机字节，T067 硬件清单 §2.2）。
export function randomNonceHex() {
  let s = '';
  while (s.length < 32) s += Math.floor(Math.random() * 16).toString(16);
  return s;
}

// signDeviceRequest = HMAC-SHA256(secret, "METHOD\npath\ndevice_id\nts_unix\nnonce\nsha256_hex(body)")
export function signDeviceRequest(method, path, deviceID, nonce, body, tsUnix) {
  const signStr = [method, path, deviceID, `${tsUnix}`, nonce, crypto.sha256(body, 'hex')].join('\n');
  return crypto.hmac('sha256', deviceSecret(), signStr, 'hex');
}

// reportHeaders 组装网关设备验签组要求的五个头。body 必须是实际发出的那一串字节。
export function reportHeaders(deviceID, path, body, tsUnix, nonce) {
  return {
    'Content-Type': 'application/json',
    'X-Device-Id': deviceID,
    'X-Timestamp': `${tsUnix}`,
    'X-Nonce': nonce,
    'X-Signature': signDeviceRequest('POST', path, deviceID, nonce, body, tsUnix),
  };
}

// POINT_COUNT 与云端 model.PointCount 同值：points 长度 != 20 整帧被 20400 拒。
export const POINT_COUNT = 20;

// DEFAULT_FIRMWARE 与模拟器 defaultFirmware 同值（firmware 为上报体必填字段）。
export const DEFAULT_FIRMWARE = 'v1.2.0';

// normalPoints 生成 20 点压力（单位 mN，云端入口 ÷1000 归一为 N），1000–2200 mN 带 ±200 抖动。
export function normalPoints() {
  const out = [];
  for (let i = 0; i < POINT_COUNT; i++) {
    let v = 1000 + Math.floor(Math.random() * 1200) + (Math.floor(Math.random() * 401) - 200);
    if (v < 0) v = 0;
    out.push(v);
  }
  return out;
}
