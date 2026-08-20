// BraceSync k6 压测 — 场景 4：设备并发上报（稳态）
// 对齐：docs/ §6 场景 4
// 用法：k6 run device-concurrent.js -e BASE_URL=http://localhost:8080

import http from 'k6/http';
import { check, sleep } from 'k6';

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

export default function () {
  const deviceNum = Math.floor(Math.random() * 1000);
  const payload = JSON.stringify({
    device_id: `DEV_${String(deviceNum).padStart(4, '0')}`,
    timestamp: new Date().toISOString(),
    pressures: Array.from({ length: 20 }, () => Math.floor(Math.random() * 25) + 8),
    battery: Math.floor(Math.random() * 40) + 60,
    wearing: true,
  });

  const res = http.post(`${BASE_URL}/api/v1/device/report`, payload, {
    headers: {
      'Content-Type': 'application/json',
      'X-Device-Id': `DEV_${String(deviceNum).padStart(4, '0')}`,
      'X-Timestamp': `${Math.floor(Date.now() / 1000)}`,
      'X-Signature': 'test_signature_placeholder',
    },
  });
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
  sleep(0.5);
}
