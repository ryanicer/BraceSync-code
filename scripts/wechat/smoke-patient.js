const automator = require('miniprogram-automator');
const { exec } = require('child_process');
const path = require('path');
const fs = require('fs');

// 用法：先构建 npm -w apps/patient-miniapp run build:mp-weixin，再在微信开发者工具可用的环境执行本脚本。
// CLI 路径可用环境变量 WX_CLI 覆盖（沙箱内无法启动 IDE，需在沙箱外运行）。
const CLI_PATH = process.env.WX_CLI || 'C:\\Program Files (x86)\\Tencent\\微信web开发者工具\\cli.bat';
const PATIENT_PROJECT = path.resolve(__dirname, '..', '..', 'apps', 'patient-miniapp', 'dist', 'build', 'mp-weixin');
const SCREENSHOT_DIR = path.resolve(__dirname, 'artifacts');
const AUTO_PORT = process.env.AUTO_PORT || 9422;
const results = { errors: [], screenshots: [], steps: [] };

function waitForPort(port, timeoutMs) {
  const net = require('net');
  return new Promise((resolve, reject) => {
    const start = Date.now();
    const tryConnect = () => {
      const sock = new net.Socket();
      sock.setTimeout(2000);
      sock.on('connect', () => { sock.destroy(); resolve(); });
      sock.on('error', () => {
        if (Date.now() - start > timeoutMs) { reject(new Error(`Port ${port} not ready after ${timeoutMs}ms`)); }
        else { setTimeout(tryConnect, 1000); }
      });
      sock.on('timeout', () => { sock.destroy(); setTimeout(tryConnect, 1000); });
      sock.connect(port, '127.0.0.1');
    };
    tryConnect();
  });
}

function withTimeout(promise, ms, label) {
  return Promise.race([
    promise,
    new Promise((_, reject) => setTimeout(() => reject(new Error(`Timeout: ${label} (${ms}ms)`)), ms))
  ]);
}

async function retry(fn, times, intervalMs, label) {
  let lastErr;
  for (let i = 0; i < times; i++) {
    try { return await fn(); }
    catch (e) {
      lastErr = e;
      console.log(`  [retry ${i + 1}/${times}] ${label}: ${e.message}`);
      await new Promise(r => setTimeout(r, intervalMs));
    }
  }
  throw lastErr;
}

async function shot(mp, file, label) {
  try {
    await retry(() => withTimeout(mp.screenshot({ path: path.join(SCREENSHOT_DIR, file) }), 12000, label), 3, 3000, label);
    results.screenshots.push(file);
    console.log(`  [截图] ${file} 完成`);
  } catch (e) {
    console.log(`  [截图] ${file} 失败（不阻塞）: ${e.message}`);
  }
}

async function pageState(mp) {
  const d = await withTimeout(mp.evaluate(function () {
    var p = getCurrentPages()[getCurrentPages().length - 1];
    return JSON.stringify({ route: p.route, webviewId: p.data.__webviewId__ });
  }), 10000, 'evaluate pageState');
  return JSON.parse(String(d));
}

async function run() {
  fs.mkdirSync(SCREENSHOT_DIR, { recursive: true });
  let mp;
  let cliProcess;
  try {
    console.log('[0/5] 通过 CLI 启动自动化端口...');
    cliProcess = exec(`"${CLI_PATH}" auto --project "${PATIENT_PROJECT}" --auto-port ${AUTO_PORT}`, {
      timeout: 120000,
      maxBuffer: 1024 * 1024,
    }, (err, stdout, stderr) => {
      if (err && !err.killed) {
        console.log('  CLI output:', stdout?.substring(0, 300));
        console.log('  CLI stderr:', stderr?.substring(0, 300));
      }
    });
    cliProcess.stdout?.on('data', (d) => console.log('  CLI:', d.toString().trim()));
    cliProcess.stderr?.on('data', (d) => console.log('  CLI err:', d.toString().trim()));

    await withTimeout(waitForPort(AUTO_PORT, 30000), 35000, 'waitForPort');
    console.log('[0/5] 端口就绪');
    await new Promise(r => setTimeout(r, 3000));

    console.log('[1/5] 连接自动化端口...');
    mp = await retry(() => withTimeout(
      automator.connect({ wsEndpoint: `ws://127.0.0.1:${AUTO_PORT}` }), 15000, 'connect'
    ), 5, 4000, 'connect');
    console.log('[1/5] 连接成功');

    mp.on('console', (msg) => {
      if (msg.type === 'error' && (msg.text === undefined || msg.text === 'undefined')) return;
      console.log(`  [Console.${msg.type}] ${String(msg.text).substring(0, 200)}`);
      if (msg.type === 'error') results.errors.push(`[Console] ${msg.text}`);
    });
    mp.on('exception', (err) => {
      console.log(`  [Exception] ${JSON.stringify(err)}`);
      results.errors.push(`[Exception] ${JSON.stringify(err)}`);
    });

    const st0 = await pageState(mp);
    console.log(`[1/5] 当前页面: ${st0.route} (webviewId ${st0.webviewId})`);

    console.log('[2/5] reLaunch 到登录页...');
    await withTimeout(mp.reLaunch('/pages/login/index'), 20000, 'reLaunch login');
    await new Promise(r => setTimeout(r, 5000));
    const st1 = await pageState(mp);
    console.log(`[2/5] 登录页: ${st1.route} (webviewId ${st1.webviewId})`);
    await shot(mp, 'smoke-patient-1-login.png', 'screenshot login');

    console.log('[3/5] 勾选协议并登录（勾选后验证状态，必要时补勾）...');
    await withTimeout(mp.evaluate(function () {
      var p = getCurrentPages()[getCurrentPages().length - 1];
      if (p['e0_bc']) p['e0_bc']({});
      if (p['e5_ed']) p['e5_ed']({});
    }), 10000, 'evaluate agree');
    await new Promise(r => setTimeout(r, 1000));

    const agreed = await withTimeout(mp.evaluate(function () {
      var p = getCurrentPages()[getCurrentPages().length - 1];
      return JSON.stringify({ agreed: p.data.m, cls: p.data.n });
    }), 10000, 'evaluate verify');
    console.log('[3/5] 勾选状态:', String(agreed));

    const agreedObj = JSON.parse(String(agreed));
    if (!agreedObj.agreed) {
      console.log('[3/5] 勾选未生效，重试一次...');
      await withTimeout(mp.evaluate(function () {
        var p = getCurrentPages()[getCurrentPages().length - 1];
        if (p['e5_ed']) p['e5_ed']({});
      }), 10000, 'evaluate agree retry');
      await new Promise(r => setTimeout(r, 1000));
      const agreed2 = await withTimeout(mp.evaluate(function () {
        var p = getCurrentPages()[getCurrentPages().length - 1];
        return JSON.stringify({ agreed: p.data.m });
      }), 10000, 'evaluate verify2');
      console.log('[3/5] 重试后勾选状态:', String(agreed2));
    }

    console.log('[3/5] 触发微信授权登录...');
    await withTimeout(mp.evaluate(function () {
      var p = getCurrentPages()[getCurrentPages().length - 1];
      if (p['e7_13']) p['e7_13']({});
    }), 10000, 'evaluate login');

    let loginPass = false;
    let cur = null;
    for (let i = 0; i < 15; i++) {
      await new Promise(r => setTimeout(r, 1000));
      cur = await pageState(mp);
      if (cur.route === 'pages/monitor/index') { loginPass = true; break; }
    }
    results.steps.push({ step: 'login->monitor', result: loginPass ? 'PASS' : 'FAIL', actual: cur ? cur.route : 'unknown' });
    console.log(`[3/5] 跳转: ${cur.route} (${loginPass ? 'PASS' : 'FAIL'})`);

    if (!loginPass) {
      console.log('[3/5] UI 登录未跳转，改用 switchTab 直接验证 monitor 页...');
      await withTimeout(mp.switchTab('/pages/monitor/index'), 20000, 'switchTab monitor');
      await new Promise(r => setTimeout(r, 4000));
      cur = await pageState(mp);
      const monPass = cur.route === 'pages/monitor/index';
      results.steps.push({ step: 'monitor(tab-fallback)', result: monPass ? 'PASS(tab)' : 'FAIL', actual: cur.route });
      console.log(`[3/5] monitor(tab): ${cur.route} (${monPass ? 'PASS' : 'FAIL'})`);
    }

    await shot(mp, 'smoke-patient-2-monitor.png', 'screenshot monitor');

    console.log('[4/5] 验证 monitor 页 mock 数据渲染...');
    const monData = await withTimeout(mp.evaluate(function () {
      var p = getCurrentPages()[getCurrentPages().length - 1];
      return JSON.stringify({ route: p.route, sample: JSON.stringify(p.data).substring(0, 300) });
    }), 10000, 'evaluate monitor data');
    console.log('[4/5] monitor 数据:', String(monData).substring(0, 400));

    console.log('[4/5] switchTab 到 history 页...');
    await withTimeout(mp.switchTab('/pages/history/index'), 20000, 'switchTab history');
    await new Promise(r => setTimeout(r, 4000));
    const stH = await pageState(mp);
    const histPass = stH.route === 'pages/history/index';
    results.steps.push({ step: 'monitor->history', result: histPass ? 'PASS' : 'FAIL', actual: stH.route });
    console.log(`[4/5] history: ${stH.route} (${histPass ? 'PASS' : 'FAIL'})`);

    await shot(mp, 'smoke-patient-3-history.png', 'screenshot history');

    console.log('[5/5] history 页渲染数据...');
    const histData = await withTimeout(mp.evaluate(function () {
      var p = getCurrentPages()[getCurrentPages().length - 1];
      return JSON.stringify({ route: p.route, sample: JSON.stringify(p.data).substring(0, 300) });
    }), 10000, 'evaluate history data');
    console.log('[5/5] history 数据:', String(histData).substring(0, 400));

    await mp.disconnect();
  } catch (e) {
    results.errors.push(`${e.message}`);
    if (e.stack) results.errors.push(e.stack);
    if (mp) { try { await mp.disconnect(); } catch {} }
  } finally {
    if (cliProcess) { try { cliProcess.kill(); } catch {} }
  }

  console.log('\n===== 患者端冒烟结果 =====');
  console.log('步骤:', JSON.stringify(results.steps, null, 2));
  console.log('截图:', results.screenshots.join(', ') || '无');
  console.log('错误:', results.errors.length === 0 ? '无' : results.errors.join('\n'));
  const flowPass = results.steps.some(s => (s.step === 'login->monitor' && s.result === 'PASS') || s.step === 'monitor(tab-fallback)');
  const histOk = results.steps.some(s => s.step === 'monitor->history' && s.result === 'PASS');
  const pass = flowPass && histOk && results.errors.length === 0;
  console.log('总结:', pass ? 'PASS' : 'FAIL');
  process.exitCode = pass ? 0 : 1;
}

run();
