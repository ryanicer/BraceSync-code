// BraceSync k6 压测 — 场景 4：设备并发上报（稳态）
// 对齐：docs/ §6 场景 4
// 用法：k6 run device-concurrent.js -e BASE_URL=http://localhost:8080 -e DEVICE_SECRET=<设备密钥>

import http from 'k6/http';
import { check, sleep } from 'k6';
import { PATH_SINGLE, DEFAULT_FIRMWARE, normalPoints, randomNonceHex, reportHeaders } from './device-sign.js';

export const options = {
  scenarios: {
    device_concurrent: {
      executor: 'constant-arrival-rate',
      rate: 100,           // 100 台设备 × 30min 间隔 ≈ 稳态 <2 rps，此处加压到 100 rps
      timeUnit: '1s',
      duration: '5m',
      preAllocatedVUs: 20,
      maxVUs: 50,
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500'],
    http_req_failed: ['rate<0.01'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const DEVICE_COUNT = 1000; // DEV_0000 … DEV_0999，须与压测环境的注册/绑定/密钥一致

export default function () {
  const deviceID = `DEV_${String(Math.floor(Math.random() * DEVICE_COUNT)).padStart(4, '0')}`;
  // tsUnix 只取一次：签名串里的时刻与 X-Timestamp 头必须是同一个数，两次取秒会跨界造成 20401
  const tsUnix = Math.floor(Date.now() / 1000);
  // 单帧上报请求体，对齐 data-service 的 SingleFrameRequest：points 单位 mN、
  // timestamp 为 Unix 秒、firmware 必填、无 wearing（佩戴由云端按 max(points) 推导）
  const payload = JSON.stringify({
    device_id: deviceID,
    timestamp: tsUnix,
    points: normalPoints(),
    battery: Math.floor(Math.random() * 40) + 60,
    firmware: DEFAULT_FIRMWARE,
  });

  const res = http.post(`${BASE_URL}${PATH_SINGLE}`, payload, {
    headers: reportHeaders(deviceID, PATH_SINGLE, payload, tsUnix, randomNonceHex()),
  });
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
  sleep(0.5);
}
