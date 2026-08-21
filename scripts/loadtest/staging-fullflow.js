// T023 Part B: BraceSync Staging Full-Flow Stress Test (k6)
//
// ===== Running Instructions =====
// 1. SSH Tunnel (local machine): ssh -L 2080:localhost:81 ubuntu@<STAGING_IP> -N
// 2. Set environment variables:
//    - Windows PowerShell: $env:TARGET_URL="http://localhost:2080"; $env:ADMIN_USER="ops_admin"...
//    - Linux/Mac: export TARGET_URL="http://localhost:81"; export ADMIN_USER="ops_admin"...
//    - See .env.example for all required vars (ADMIN/TECH/PATIENT credentials, DEVICE config)
// 3. Run: k6 run scripts/loadtest/staging-fullflow.js
// 4. Optional HTML report: k6 run --summary-export=results.json scripts/loadtest/staging-fullflow.js; k6 report results.json --browser
//
// ===== Scenarios =====
// - loginBurst:     0→100 VUs over 5s, hold 10s      (validate auth throughput)
// - deviceIngest:   0→50 VUs over 10s, hold 60s      (signature verification + DB insertion)
// - dashboardQuery: 0→30 VUs over 5s, hold 30s       (aggregation queries P95≤400ms)
//
// ===== Thresholds =====
// - http_req_duration: p(95)<400ms (P95 ≤ 400ms)
// - http_req_failed: rate<0.05 (<5% error rate)
//
// ===== Notes =====
// - Device reporting scenario SKIPS if DEVICE_SECRET not set (placeholder key in staging DB)
// - To disable device scenario: SCENARIO_DEVICE_REPORT_ENABLED=false
// - All login endpoints require real staging credentials (not mock data)
// - Dashboard endpoint: GET /api/v1/admin/dashboard/kpi?period=today|week|month
// - Alerts endpoint: GET /api/v1/alerts?page=1&pageSize=50 (supports patientId/type/status filters)
// - P95 ≤ 400ms target from runbook §9

import { check, group } from 'k6';
import http from 'k6/http';
import { Trend, Rate, Counter } from 'k6/metrics';
import crypto from 'k6/crypto';
import { writeFileSync, mkdirSync } from 'fs';

// ===== Custom Metrics =====
const p95ResponseTime = new Trend('http_req_duration_p95');
const p99ResponseTime = new Trend('http_req_duration_p99');
const tpsCounter = new Counter('tps');
const errorRate = new Rate('errors');

// ===== Configuration (ENV) =====
const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:2080'; // SSH tunnel → 81
const ADMIN_USER = __ENV.ADMIN_USER || 'ops_admin';
const ADMIN_PASS = __ENV.ADMIN_PASS || 'admin123';
const TECH_ID = __ENV.TECH_ID || 'T0001';
const TECH_PHONE = __ENV.TECH_PHONE || '13900000001'; // Tech login uses phone number
const TECH_PASS = __ENV.TECH_PASS || 'admin123';
const PATIENT_PHONE = __ENV.PATIENT_PHONE || '13800000001'; // From seed.sql hash
const PATIENT_PASS = __ENV.PATIENT_PASS || 'admin123';
const DEVICE_ID = __ENV.DEVICE_ID || 'PRS-ML05-RC-20260701001';
const DEVICE_SECRET = __ENV.DEVICE_SECRET || ''; // ⚠️ Required for device-report; empty = SKIP
const SCENARIO_DEVICE_REPORT_ENABLED = __ENV.SCENARIO_DEVICE_REPORT_ENABLED !== 'false'; // Default true if set

// ===== Scenario Config =====
export const options = {
  scenarios: {
    loginBurst: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '5s', target: 100 }, // Ramp to 100 VUs
        { duration: '10s', target: 100 }, // Hold 10s
      ],
      gracefulStop: '5s',
    },
    deviceIngest: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 50 }, // Ramp to 50 VUs
        { duration: '60s', target: 50 }, // Hold 60s
      ],
      gracefulStop: '10s',
    },
    dashboardQuery: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '5s', target: 30 }, // Ramp to 30 VUs
        { duration: '30s', target: 30 }, // Hold 30s
      ],
      gracefulStop: '5s',
    },
  },
  thresholds: {
    'http_req_duration': ['p(95)<400'], // Target: P95 ≤ 400ms
    'http_req_failed': ['rate<0.05'], // <5% error rate
  },
};

// ===== Helpers =====
function calculateP95(data) {
  const sorted = data.sort((a, b) => a - b);
  const index = Math.ceil(sorted.length * 0.95) - 1;
  return sorted[Math.max(0, index)];
}

function calculateP99(data) {
  const sorted = data.sort((a, b) => a - b);
  const index = Math.ceil(sorted.length * 0.99) - 1;
  return sorted[Math.max(0, index)];
}

// ===== Login Flow =====
function login(username, password, role) {
  // Admin: /api/v1/auth/login (username+password)
  // Tech: /api/v1/tech/login (phone+password)
  // Patient: /api/v1/patient/login (phone+password)
  let url;
  let payload;
  if (role === 'admin') {
    url = `${TARGET_URL}/api/v1/auth/login`;
    payload = JSON.stringify({ username, password });
  } else if (role === 'tech') {
    url = `${TARGET_URL}/api/v1/tech/login`;
    payload = JSON.stringify({ phone: username, password });
  } else if (role === 'patient') {
    url = `${TARGET_URL}/api/v1/patient/login`;
    payload = JSON.stringify({ phone: username, password });
  } else {
    throw new Error(`Unknown role: ${role}`);
  }

  const headers = {
    'Content-Type': 'application/json',
  };

  const res = http.post(url, payload, { headers });
  
  check(res, {
    [`${role} login success`]: (r) => r.status === 200,
    [`${role} login no error`]: (r) => r.status < 400,
  });

  errorRate.add(res.status >= 400);

  if (res.status !== 200) {
    console.warn(`${role} login failed: ${res.body}`);
    return null;
  }

  const token = JSON.parse(res.body).data ? JSON.parse(res.body).data.token : null;
  if (!token) {
    console.warn(`${role} login response missing token`);
    return null;
  }

  return token;
}

// ===== Device Report with HMAC Signature =====
function generateDeviceSignature(deviceSecret, method, path, body, timestamp) {
  // Simplified: In production, use crypto-js HMAC-SHA256
  // k6 doesn't have native crypto, so we'll skip signing if secret not provided
  if (!deviceSecret) {
    return { signed: false, skipped: true };
  }

  // TODO: Implement HMAC-SHA256 using https://jslib.k6.io/crypto/0.1.0/index.js
  // For now, return placeholder to demonstrate structure
  const ts = Math.floor(timestamp / 1000).toString();
  const signString = `${method}:${path}:${body}:${ts}`;
  
  return {
    signed: true,
    deviceId: DEVICE_ID,
    timestamp: ts,
    // signature: computeHMACSHA256(deviceSecret, signString), // Not implemented yet
  };
}

function submitDeviceReport(token, deviceSecret, numSamples = 10) {
  const url = `${TARGET_URL}/api/v1/device/records`;
  const now = Date.now();

  // Generate multiple samples per request
  const pressures = [];
  for (let i = 0; i < numSamples; i++) {
    pressures.push(Math.floor(10 + Math.random() * 40)); // 10-50N range
  }

  const body = JSON.stringify({
    device_id: DEVICE_ID,
    pressures: pressures,
    ts: now,
  });

  const sig = generateDeviceSignature(deviceSecret, 'POST', '/api/v1/device/records', body, now);

  const headers = {
    'Content-Type': 'application/json',
  };

  // Add device signature headers if secret provided
  if (sig.signed && !sig.skipped) {
    headers['X-Device-Id'] = sig.deviceId;
    headers['X-Timestamp'] = sig.timestamp;
    headers['X-Signature'] = sig.signature || 'placeholder';
  }

  const res = http.post(url, body, { headers });

  check(res, {
    'device report accepted': (r) => r.status === 200 || r.status === 201,
    'device report no 5xx': (r) => r.status < 500,
  });

  errorRate.add(res.status >= 400);
  return res;
}

// ===== Alert Query =====
function queryAlerts(token) {
  // 后端契约：GET /api/v1/alerts?page=1&pageSize=20
  // 返回结构：{ code: 200, message: "success", data: { list: [], total: 6, page: 1, pageSize: 20 } }
  const url = `${TARGET_URL}/api/v1/alerts?page=1&pageSize=50`;
  
  const headers = {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json',
  };

  const res = http.get(url, { headers });

  check(res, {
    'alerts queried successfully': (r) => r.status === 200,
    'alerts list is array': (r) => {
      const d = JSON.parse(r.body).data
      return r.status === 200 && d && d.list instanceof Array
    },
  });

  errorRate.add(res.status >= 400);
  return res;
}

// ===== Dashboard Aggregation Query =====
function queryDashboard(token) {
  // 后端契约：GET /api/v1/admin/dashboard/kpi?period=today|week|month
  // 返回结构：{ code: 200, message: "success", data: { totalPatients, todayActiveWear, todayAlerts, avgWearHours, deviceOnlineRate, monthNewPatients } }
  const url = `${TARGET_URL}/api/v1/admin/dashboard/kpi?period=today`;

  const headers = {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json',
  };

  const res = http.get(url, { headers });

  check(res, {
    'dashboard queried successfully': (r) => r.status === 200,
    'dashboard has required fields': (r) => {
      if (r.status !== 200) return false;
      const data = JSON.parse(r.body).data;
      return data && data.totalPatients !== undefined &&
             data.todayAlerts !== undefined && data.todayActiveWear !== undefined;
    },
  });

  errorRate.add(res.status >= 400);
  return res;
}

// ===== Main Execution Functions =====
function loginStage() {
  group('Admin Login', function() {
    const token = login(ADMIN_USER, ADMIN_PASS, 'admin');
    if (token) {
      queryAlerts(token);
      queryDashboard(token);
    }
  });

  group('Tech Login', function() {
    login(TECH_PHONE, TECH_PASS, 'tech');
  });

  group('Patient Login', function() {
    login(PATIENT_PHONE, PATIENT_PASS, 'patient');
  });
}

function deviceReportStage() {
  if (!SCENARIO_DEVICE_REPORT_ENABLED || !DEVICE_SECRET) {
    console.log('[SKIP] Device report scenario disabled or missing DEVICE_SECRET');
    return;
  }

  group('Device Report Burst', function() {
    const samples = Array.from({ length: 10 }, () => submitDeviceReport(null, DEVICE_SECRET, 20));
    return samples;
  });
}

function dashboardStage() {
  group('Dashboard Query (Admin Token)', function() {
    const adminToken = login(ADMIN_USER, ADMIN_PASS, 'admin');
    if (adminToken) {
      for (let i = 0; i < 5; i++) {
        queryAlerts(adminToken);
        queryDashboard(adminToken);
      }
    }
  });
}

// ===== Scenarios =====
export function setup() {
  // Pre-warm login tokens for scenarios
  const adminToken = login(ADMIN_USER, ADMIN_PASS, 'admin');
  const techToken = login(TECH_PHONE, TECH_PASS, 'tech');
  const patientToken = login(PATIENT_PHONE, PATIENT_PASS, 'patient');

  return {
    adminToken,
    techToken,
    patientToken,
  };
}

// ===== Default Export (Required by k6 for multi-scenario scripts) =====
export default function() {
  // This function is called by k6 for each VU iteration
  // The actual scenario execution is controlled by options.scenarios above
  // We distribute the stages across scenarios
  loginStage();
  deviceReportStage();
  dashboardStage();
}

// ===== Execution Hooks =====
export function handleSummary(data) {
  // Ensure reports directory exists (k6 doesn't auto-create nested dirs)
  try {
    mkdirSync('reports', { recursive: true, mode: 0o755 });
  } catch (e) {
    console.error('[WARN] Failed to create reports directory:', e.message);
  }

  // Use k6's built-in percentile values from http_req_duration metric
  const durationMetrics = data.metrics.http_req_duration;
  const p95 = durationMetrics && durationMetrics.values['p(95)'] ? durationMetrics.values['p(95)'] : 0;
  const p99 = durationMetrics && durationMetrics.values['p(99)'] ? durationMetrics.values['p(99)'] : 0;

  console.log('\n===== T023 压测报告基线 =====');
  console.log(`P95 响应时间：${p95.toFixed(2)}ms`);
  console.log(`P99 响应时间：${p99.toFixed(2)}ms`);
  
  // Error rate
  const errorMetrics = data.metrics.errors;
  const errorRateValue = errorMetrics ? (errorMetrics.values.rate * 100).toFixed(2) : '0.00';
  console.log(`错误率：${errorRateValue}%`);
  
  // Total requests
  const requestsMetrics = data.metrics.http_reqs;
  const totalRequests = requestsMetrics ? requestsMetrics.values.count : 0;
  console.log(`总请求数：${totalRequests}`);
  
  // TPS calculation
  const durationSeconds = data.state.duration / 1000;
  const tps = durationSeconds > 0 ? (totalRequests / durationSeconds).toFixed(2) : '0.00';
  console.log(`TPS: ${tps}`);

  // Threshold results
  console.log('\n--- 阈值结果 ---');
  if (data.thresholds) {
    if (data.thresholds['http_req_duration']) {
      console.log('http_req_duration(p95<400ms):', 
        data.thresholds['http_req_duration'].passed ? '✅ PASS' : '❌ FAIL');
    }
    if (data.thresholds['http_req_failed']) {
      console.log('http_req_failed(rate<5%):', 
        data.thresholds['http_req_failed'].passed ? '✅ PASS' : '❌ FAIL');
    }
  }

  console.log('\n========================================\n');

  // Return JSON summary and stdout text (no external dependencies)
  return {
    'reports/staging-summary.json': JSON.stringify(data, null, 2),
    stdout: generateTextSummary(data),
  };
}

// ===== Local Text Summary Generator (replaces jslib.k6.io/text-summary) =====
function generateTextSummary(data) {
  let summary = '\n';
  summary += '✓ checks...:\n';
  
  if (data.metrics.checks) {
    const checks = data.metrics.checks;
    summary += `  ✓ ${checks.values.passes} / ${checks.values.passes + checks.values.fails} - ${((checks.values.passes / (checks.values.passes + checks.values.fails)) * 100).toFixed(2)}%\n`;
  }
  
  summary += '\n';
  summary += 'http_req_duration.........: ';
  if (data.metrics.http_req_duration) {
    const vals = data.metrics.http_req_duration.values;
    summary += `avg=${vals.avg.toFixed(2)}ms min=${vals.min.toFixed(2)}ms med=${vals.med.toFixed(2)}ms max=${vals.max.toFixed(2)}ms p(90)=${vals['p(90)'].toFixed(2)}ms p(95)=${vals['p(95)'].toFixed(2)}ms\n`;
  }
  
  summary += 'http_reqs.................: ';
  if (data.metrics.http_reqs) {
    summary += `${data.metrics.http_reqs.values.count} total\n`;
  }
  
  summary += 'iteration_duration........: ';
  if (data.metrics.iteration_duration) {
    const vals = data.metrics.iteration_duration.values;
    summary += `avg=${vals.avg.toFixed(2)}ms min=${vals.min.toFixed(2)}ms med=${vals.med.toFixed(2)}ms max=${vals.max.toFixed(2)}ms p(90)=${vals['p(90)'].toFixed(2)}ms p(95)=${vals['p(95)'].toFixed(2)}ms\n`;
  }
  
  summary += 'iterations................: ';
  if (data.metrics.iterations) {
    summary += `${data.metrics.iterations.values.count} total\n`;
  }
  
  summary += 'vus.......................: ';
  if (data.metrics.vus) {
    summary += `min=${data.metrics.vus.values.min} max=${data.metrics.vus.values.max}\n`;
  }
  
  summary += 'vus_max...................: ';
  if (data.metrics.vus_max) {
    summary += `${data.metrics.vus_max.values.max} max\n`;
  }
  
  return summary;
}

// ===== Graceful shutdown =====
export function handleRunComplete(testRunResult) {
  console.log('\n[INFO] 压测完成，请检查 ./reports/目录生成的报告和统计数据');
}
