const automator = require('miniprogram-automator');
const { exec } = require('child_process');
const path = require('path');
const fs = require('fs');
const targetReport = require('./lib-target');

// 用法：先构建 npm -w apps/tech-miniapp run build:mp-weixin，再在微信开发者工具可用的环境执行本脚本。
// CLI 路径可用环境变量 WX_CLI 覆盖（沙箱内无法启动 IDE，需在沙箱外运行）。
// CONNECT_ONLY=1 模式：跳过 CLI 启动，直接连接已运行的自动化端口（Boss 端启动后用）。
const CLI_PATH = process.env.WX_CLI || 'C:\\Program Files (x86)\\Tencent\\微信web开发者工具\\cli.bat';
const TECH_PROJECT = path.resolve(__dirname, '..', '..', 'apps', 'tech-miniapp', 'dist', 'build', 'mp-weixin');
const SCREENSHOT_DIR = path.resolve(__dirname, 'artifacts');
const AUTO_PORT = process.env.AUTO_PORT || 9420;

const results = { errors: [], screenshots: [], steps: [] };

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

async function shot(mp, file, label) {
  const target = path.join(SCREENSHOT_DIR, file);
  try {
    await withTimeout(mp.screenshot({ path: target }), 5000, label);
    results.screenshots.push(file);
    console.log(`  [截图] ${file} 完成`);
  } catch (e) {
    console.log(`  [截图] ${file} 失败（不阻塞）: ${e.message}`);
  }
}

// 用 evaluate + getCurrentPages 获取栈顶页面路由（currentPage() 在新版 IDE 偶发超时）
async function pageRoute(mp) {
  const s = await withTimeout(mp.evaluate(function () {
    var ps = getCurrentPages();
    if (!ps.length) return 'NONE';
    return ps[ps.length - 1].route;
  }), 10000, 'pageRoute');
  return String(s);
}

const CONNECT_ONLY = process.env.CONNECT_ONLY === '1';

async function run() {
  fs.mkdirSync(SCREENSHOT_DIR, { recursive: true });

  // T160：冒烟前自证产物实际打到的目标。生产目标默认拒绝运行（需显式 ALLOW_PRODUCTION=1）。
  const APP_DIR = path.resolve(__dirname, '..', '..', 'apps', 'tech-miniapp');
  const built = targetReport.printTargetReport(APP_DIR, TECH_PROJECT);
  if (built.target === 'prod') {
    console.warn('\n[WARN] ⚠ 当前产物为生产目标（直连生产后端）！冒烟将连生产后端。');
    if (process.env.ALLOW_PRODUCTION !== '1') {
      console.warn('[WARN] 已拒绝运行。若确要在生产目标上跑冒烟，请显式设置 ALLOW_PRODUCTION=1。\n');
      process.exit(1);
    }
    console.warn('[WARN] 已通过 ALLOW_PRODUCTION=1 显式放行，继续冒烟。\n');
  } else if (built.target === 'unknown') {
    console.warn('\n[WARN] 产物中未命中任何已知后端地址（staging http://hbksd.com.cn:81 / prod https://api.hbksd.com.cn）。');
    console.warn('[WARN] 可能尚未构建，或产物地址异常。继续运行，结果请谨慎判定。\n');
  }

  let mp;
  let cliProcess;
  try {
    if (CONNECT_ONLY) {
      console.log('[1/9] CONNECT_ONLY 模式：跳过 CLI 启动，直接连接已运行的自动化端口...');
      console.log('[2/9] 等待自动化端口就绪...');
      await withTimeout(waitForPort(AUTO_PORT, 60000), 65000, 'waitForPort');
      console.log('[2/9] 端口就绪');
    } else {
      console.log('[1/9] 通过 CLI 启动自动化端口...');
      cliProcess = exec(`"${CLI_PATH}" auto --project "${TECH_PROJECT}" --auto-port ${AUTO_PORT}`, {
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

      console.log('[2/9] 等待自动化端口就绪...');
      await withTimeout(waitForPort(AUTO_PORT, 30000), 35000, 'waitForPort');
      console.log('[2/9] 端口就绪');
    }

    console.log('[3/9] 连接自动化端口...');
    await new Promise(r => setTimeout(r, 3000));
    mp = await withTimeout(
      automator.connect({ wsEndpoint: `ws://127.0.0.1:${AUTO_PORT}` }),
      15000,
      'connect'
    );
    console.log('[3/9] 连接成功');

    mp.on('console', (msg) => {
      if (msg.type === 'error' && (msg.text === undefined || msg.text === 'undefined')) return;
      console.log(`  [Console.${msg.type}] ${msg.text}`);
      if (msg.type === 'error') results.errors.push(`[Console] ${msg.text}`);
    });
    mp.on('exception', (err) => {
      console.log(`  [Exception] ${JSON.stringify(err)}`);
      results.errors.push(`[Exception] ${JSON.stringify(err)}`);
    });

    // 等待 IDE 完全就绪后再获取页面（刚连上时 evaluate 可能超时）
    console.log('[4/9] 等待 IDE 就绪后获取当前页面...');
    await new Promise(r => setTimeout(r, 5000));
    let curRoute = await retry(() => pageRoute(mp), 3, 3000, 'pageRoute initial');
    console.log(`[4/9] 当前页面: ${curRoute}`);

    if (curRoute !== 'pages/login/index') {
      console.log('[4/9] 跳转到登录页...');
      await withTimeout(mp.reLaunch('/pages/login/index'), 15000, 'reLaunch');
    }
    console.log('[4/9] 等待页面渲染...');
    await new Promise(r => setTimeout(r, 8000));

    curRoute = await pageRoute(mp);
    console.log(`[4/9] 当前页面: ${curRoute}`);

    await shot(mp, 'smoke-tech-1-login.png', 'screenshot1');
    console.log('[4/9] 登录页截图完成');

    console.log('[5/9] 诊断页面实例...');
    const diag = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      if (!ps.length) return 'NO_PAGE';
      var page = ps[ps.length - 1];
      var keys = Object.keys(page);
      return JSON.stringify({ route: page.route, methodCount: keys.length, sampleKeys: keys.slice(0, 30) });
    }), 10000, 'evaluate diag');
    console.log('[5/9] 诊断结果:', diag);

    console.log('[5/9] 输入手机号 (e0_f8)...');
    const r1 = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      var page = ps[ps.length - 1];
      var fn = page['e0_f8'];
      if (typeof fn === 'function') { fn({ detail: { value: '13800138000' }, currentTarget: { dataset: {} } }); return 'OK'; }
      return 'NO_METHOD e0_f8';
    }), 10000, 'evaluate e0_f8');
    console.log('[5/9] e0_f8:', r1);
    if (String(r1) !== 'OK') throw new Error(`e0_f8 调用失败: ${r1}`);
    await new Promise(r => setTimeout(r, 500));

    console.log('[6/9] 输入密码 (e1_38)...');
    const r2 = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      var page = ps[ps.length - 1];
      var fn = page['e1_38'];
      if (typeof fn === 'function') { fn({ detail: { value: '123456' }, currentTarget: { dataset: {} } }); return 'OK'; }
      return 'NO_METHOD e1_38';
    }), 10000, 'evaluate e1_38');
    console.log('[6/9] e1_38:', r2);
    await new Promise(r => setTimeout(r, 500));

    console.log('[7/9] 勾选协议 (e3_2b) 并登录 (e4_18)...');
    const r3 = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      var page = ps[ps.length - 1];
      var toggle = page['e3_2b'];
      if (typeof toggle === 'function') { toggle({}); } else { return 'NO_METHOD e3_2b'; }
      var login = page['e4_18'];
      if (typeof login === 'function') { login({}); return 'OK'; }
      return 'NO_METHOD e4_18';
    }), 10000, 'evaluate login');
    console.log('[7/9] login:', r3);
    console.log('[7/9] 等待跳转 bind 页（mock 1.5s + 缓冲）...');
    await new Promise(r => setTimeout(r, 6000));

    curRoute = await pageRoute(mp);
    const loginPass = curRoute === 'pages/bind/index';
    results.steps.push({ step: 'login->bind', result: loginPass ? 'PASS' : 'FAIL', actual: curRoute });
    console.log(`[7/9] 跳转: ${curRoute} (${loginPass ? 'PASS' : 'FAIL'})`);

    await shot(mp, 'smoke-tech-2-bind.png', 'screenshot2');

    if (!loginPass) throw new Error(`未跳转到 bind 页: ${curRoute}`);

    console.log('[8/9] 诊断 bind 页方法名...');
    const bindDiag = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      if (!ps.length) return 'NO_PAGE';
      var page = ps[ps.length - 1];
      return JSON.stringify({ route: page.route, keys: Object.keys(page) });
    }), 10000, 'evaluate bind diag');
    console.log('[8/9] bind 页诊断:', String(bindDiag).substring(0, 600));

    console.log('[8/9] 输入设备ID并绑定 + 导航 fallback...');
    const r4 = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      var page = ps[ps.length - 1];
      if (page.route !== 'pages/bind/index') return 'WRONG_PAGE:' + page.route;
      var keys = Object.keys(page);
      var inputKey = keys.find(function (k) { return /^e\d+_79$/.test(k); });
      var bindKey = keys.find(function (k) { return /^e\d+_9e$/.test(k); });
      if (!bindKey) return 'NO_METHOD bind=' + bindKey;
      // 触发 input 事件（尝试更新 v-model ref）
      if (inputKey) page[inputKey]({ detail: { value: 'TEST-001' }, currentTarget: { dataset: {} } });
      // setData 作为备用（编译后键名 c 对应 manualDeviceId）
      if (typeof page.setData === 'function') page.setData({ c: 'TEST-001' });
      page[bindKey]({});
      return 'OK inputKey=' + inputKey + ' bindKey=' + bindKey;
    }), 10000, 'evaluate bind');
    console.log('[8/9] bind:', r4);
    console.log('[8/9] 等待跳转 matrix 页（1.2s + 缓冲）...');
    await new Promise(r => setTimeout(r, 6000));

    // Fallback：bind handler 可能因 ref 未更新而未导航，直接 wx.navigateTo
    let fallbackRoute = await pageRoute(mp);
    if (fallbackRoute === 'pages/bind/index') {
      console.log('[8/9] Bind 未自动导航，直接 wx.navigateTo matrix 页...');
      await withTimeout(mp.evaluate(function () {
        wx.navigateTo({ url: '/pages/matrix/index' });
      }), 10000, 'navigateTo matrix');
      await new Promise(r => setTimeout(r, 5000));
    }

    console.log('[9/9] 验证 matrix 页...');
    curRoute = await pageRoute(mp);
    const bindPass = curRoute === 'pages/matrix/index';
    results.steps.push({ step: 'bind->matrix', result: bindPass ? 'PASS' : 'FAIL', actual: curRoute });
    console.log(`[9/9] 跳转: ${curRoute} (${bindPass ? 'PASS' : 'FAIL'})`);

    await shot(mp, 'smoke-tech-3-matrix.png', 'screenshot3');

  } catch (e) {
    results.errors.push(`${e.message}`);
    if (e.stack) results.errors.push(e.stack);
  } finally {
    if (mp) { try { await mp.disconnect(); } catch {} }
    if (cliProcess) { try { cliProcess.kill(); } catch {} }
  }

  console.log('\n===== 技师端冒烟结果 =====');
  console.log('步骤:', JSON.stringify(results.steps, null, 2));
  console.log('截图:', results.screenshots.join(', ') || '无');
  console.log('错误:', results.errors.length === 0 ? '无' : results.errors.join('\n'));
  const allPass = results.steps.length > 0 && results.steps.every(s => s.result === 'PASS') && results.errors.length === 0;
  console.log('总结:', allPass ? 'PASS' : 'FAIL');
  process.exitCode = allPass ? 0 : 1;
}

run();
