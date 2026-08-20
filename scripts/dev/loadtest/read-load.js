// BraceSync k6 压测 — 场景 1：1000 QPS 读场景
// 对齐：docs/ §6 场景 1
// 用法：k6 run read-load.js -e BASE_URL=http://localhost:8080

import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  scenarios: {
    read_load: {
      executor: 'constant-arrival-rate',
      rate: 1000,
      timeUnit: '1s',
      duration: '5m',
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<400'],  // P95 < 400ms（架构 §6.4 触发指标）
    http_req_failed: ['rate<0.01'],    // 错误率 < 1%
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  // 模拟后台 Dashboard KPI 读取
  const res = http.get(`${BASE_URL}/api/v1/admin/dashboard/kpi?period=today`, {
    headers: { 'Authorization': 'Bearer <TEST_TOKEN>' },
  });
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
  sleep(0.1);
}
