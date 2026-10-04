// BraceSync k6 压测 — 场景 2：补传风暴（500 rps 突发）
// 对齐：docs/ §6 场景 2
// 用法：k6 run backfill-storm.js -e BASE_URL=http://localhost:8080 -e DEVICE_SECRET=<设备密钥>

import http from 'k6/http';
import { check } from 'k6';
import { PATH_BATCH, DEFAULT_FIRMWARE, normalPoints, randomNonceHex, reportHeaders } from './device-sign.js';

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
const DEVICE_ID = __ENV.DEVICE_ID || 'DEV_LOAD_TEST';
const FRAMES_PER_REQUEST = 50; // 整批 >100 帧会被云端按 model.MaxBatchFrames 以 20400 拒

// 批量补传请求体，对齐 data-service 的 BatchRequest/BatchFrame：
// 帧内不带 device_id、不带 firmware（批次顶层各一颗）、无 wearing（佩戴由云端推导），
// timestamp 为 Unix 秒（旧脚本这里是 ISO 串 + pressures 字段名，整批进不了库）
const batchPayload = JSON.stringify({
  device_id: DEVICE_ID,
  firmware: DEFAULT_FIRMWARE,
  frames: Array.from({ length: FRAMES_PER_REQUEST }, (_, i) => ({
    timestamp: Math.floor((Date.now() - i * 30 * 60 * 1000) / 1000),
    points: normalPoints(),
    battery: 80,
  })),
});

export default function () {
  const tsUnix = Math.floor(Date.now() / 1000);
  const res = http.post(`${BASE_URL}${PATH_BATCH}`, batchPayload, {
    headers: reportHeaders(DEVICE_ID, PATH_BATCH, batchPayload, tsUnix, randomNonceHex()),
  });
  check(res, {
    'status is 200 or 201': (r) => r.status === 200 || r.status === 201,
  });
}
