// BraceSync k6 压测 — 场景 3：Dashboard 聚合查询
// 对齐：docs/ §6 场景 3
// 用法：k6 run dashboard-agg.js -e BASE_URL=http://localhost:8080

import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  scenarios: {
    dashboard_agg: {
      executor: 'constant-vus',
      vus: 20,
      duration: '3m',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<800'],  // 聚合查询 P95 < 800ms
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  // 周报聚合
  const weekly = http.get(`${BASE_URL}/api/v1/admin/reports/weekly?team_id=TEAM01`, {
    headers: { 'Authorization': 'Bearer <TEST_TOKEN>' },
  });
  check(weekly, { 'weekly 200': (r) => r.status === 200 });

  // 患者列表（分页）
  const patients = http.get(`${BASE_URL}/api/v1/admin/patients?page=1&size=20`, {
    headers: { 'Authorization': 'Bearer <TEST_TOKEN>' },
  });
  check(patients, { 'patients 200': (r) => r.status === 200 });

  sleep(1);
}
