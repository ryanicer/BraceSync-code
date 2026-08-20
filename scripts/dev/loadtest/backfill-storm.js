// BraceSync k6 压测 — 场景 2：补传风暴（500 rps 突发）
// 对齐：docs/ §6 场景 2
// 用法：k6 run backfill-storm.js -e BASE_URL=http://localhost:8080

import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    backfill_storm: {
      executor: 'ramping-arrival-rate',
      startRate: 50,
      timeUnit: '1s',
      preAllocatedVUs: 30,
      maxVUs: 100,
      stages: [
        { duration: '30s', target: 200 },  // 预热
        { duration: '1m', target: 500 },   // 突发峰值
        { duration: '2m', target: 500 },   // 持续峰值
        { duration: '30s', target: 0 },    // 回落
      ],
    },
  },
  thresholds: {
    http_req_duration: ['p(99)<2000'],   // P99 < 2s
    http_req_failed: ['rate<0.05'],      // 错误率 < 5%（突发容忍）
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// 模拟设备批量补传（7 天 × 48 帧/天 = 336 帧）
const batchPayload = JSON.stringify({
  device_id: 'DEV_LOAD_TEST',
  frames: Array.from({ length: 50 }, (_, i) => ({
    device_id: 'DEV_LOAD_TEST',
    timestamp: new Date(Date.now() - i * 30 * 60 * 1000).toISOString(),
    pressures: Array.from({ length: 20 }, () => Math.floor(Math.random() * 30) + 5),
    battery: 80,
    wearing: true,
  })),
});

export default function () {
  const res = http.post(`${BASE_URL}/api/v1/device/report/batch`, batchPayload, {
    headers: {
      'Content-Type': 'application/json',
      'X-Device-Id': 'DEV_LOAD_TEST',
      'X-Timestamp': `${Math.floor(Date.now() / 1000)}`,
      'X-Signature': 'test_signature_placeholder',
    },
  });
  check(res, {
    'status is 200 or 201': (r) => r.status === 200 || r.status === 201,
  });
}
