const automator = require('miniprogram-automator');
const { exec } = require('child_process');
const path = require('path');
const fs = require('fs');

// 用法：先构建 npm -w apps/tech-miniapp run build:mp-weixin，再在微信开发者工具可用的环境执行本脚本。
// CLI 路径可用环境变量 WX_CLI 覆盖（沙箱内无法启动 IDE，需在沙箱外运行）。
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
    for (let i = 0; i < 3; i++) {
      try {
        await withTimeout(mp.screenshot({ path: target }), 12000, label);
        results.screenshots.push(file);
        console.log(`  [截图] ${file} 完成`);
        return;
      } catch (e) {
        console.log(`  [截图重试 ${i + 1}/3] ${e.message}`);
        await new Promise(r => setTimeout(r, 3000));
      }
    }
  } catch (e) { /* ignore */ }
  console.log(`  [截图] ${file} 失败（不阻塞）`);
}

async function run() {
  fs.mkdirSync(SCREENSHOT_DIR, { recursive: true });
  let mp;
  let cliProcess;
  try {
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

    console.log('[4/9] 获取当前页面...');
    let page = await withTimeout(mp.currentPage(), 10000, 'currentPage');
    console.log(`[4/9] 当前页面: ${page.path}`);

    if (page.path !== 'pages/login/index') {
      console.log('[4/9] 跳转到登录页...');
      await withTimeout(mp.reLaunch('/pages/login/index'), 15000, 'reLaunch');
    }
    console.log('[4/9] 等待页面渲染...');
    await new Promise(r => setTimeout(r, 8000));

    page = await withTimeout(mp.currentPage(), 10000, 'currentPage after wait');
    console.log(`[4/9] 当前页面: ${page.path}`);

    await shot(mp, 'smoke-tech-1-login.png', 'screenshot1');
    console.log('[4/9] 登录页截图完成');

    console.log('[5/9] 诊断页面实例...');
    const diag = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      if (!ps.length) return 'NO_PAGE';
      var page = ps[0];
      var keys = Object.keys(page);
      return JSON.stringify({ route: page.route, methodCount: keys.length, sampleKeys: keys.slice(0, 30) });
    }), 10000, 'evaluate diag');
    console.log('[5/9] 诊断结果:', diag);

    console.log('[5/9] 输入手机号 (e0_f8)...');
    const r1 = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      var page = ps[0];
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
      var page = ps[0];
      var fn = page['e1_38'];
      if (typeof fn === 'function') { fn({ detail: { value: '123456' }, currentTarget: { dataset: {} } }); return 'OK'; }
      return 'NO_METHOD e1_38';
    }), 10000, 'evaluate e1_38');
    console.log('[6/9] e1_38:', r2);
    await new Promise(r => setTimeout(r, 500));

    console.log('[7/9] 勾选协议 (e3_2b) 并登录 (e4_18)...');
    const r3 = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      var page = ps[0];
      var toggle = page['e3_2b'];
      if (typeof toggle === 'function') { toggle({}); } else { return 'NO_METHOD e3_2b'; }
      var login = page['e4_18'];
      if (typeof login === 'function') { login({}); return 'OK'; }
      return 'NO_METHOD e4_18';
    }), 10000, 'evaluate login');
    console.log('[7/9] login:', r3);
    console.log('[7/9] 等待跳转 bind 页（mock 1.5s + 缓冲）...');
    await new Promise(r => setTimeout(r, 6000));

    page = await withTimeout(mp.currentPage(), 10000, 'currentPage after login');
    const loginPass = page.path === 'pages/bind/index';
    results.steps.push({ step: 'login->bind', result: loginPass ? 'PASS' : 'FAIL', actual: page.path });
    console.log(`[7/9] 跳转: ${page.path} (${loginPass ? 'PASS' : 'FAIL'})`);

    await shot(mp, 'smoke-tech-2-bind.png', 'screenshot2');

    if (!loginPass) throw new Error(`未跳转到 bind 页: ${page.path}`);

    console.log('[8/9] 诊断 bind 页方法名...');
    const bindDiag = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      if (!ps.length) return 'NO_PAGE';
      var page = ps[0];
      return JSON.stringify({ route: page.route, keys: Object.keys(page) });
    }), 10000, 'evaluate bind diag');
    console.log('[8/9] bind 页诊断:', String(bindDiag).substring(0, 600));

    console.log('[8/9] 输入设备ID并绑定（自动匹配方法名）...');
    const r4 = await withTimeout(mp.evaluate(function () {
      var ps = getCurrentPages();
      var page = ps[0];
      if (page.route !== 'pages/bind/index') return 'WRONG_PAGE:' + page.route;
      var keys = Object.keys(page);
      var inputKey = keys.find(function (k) { return /^e\d+_79$/.test(k); });
      var bindKey = keys.find(function (k) { return /^e\d+_9e$/.test(k); });
      if (!inputKey || !bindKey) return 'NO_METHOD input=' + inputKey + ' bind=' + bindKey;
      page[inputKey]({ detail: { value: 'TEST-001' }, currentTarget: { dataset: {} } });
      page[bindKey]({});
      return 'OK';
    }), 10000, 'evaluate bind');
    console.log('[8/9] bind:', r4);
    console.log('[8/9] 等待跳转 matrix 页（1.2s + 缓冲）...');
    await new Promise(r => setTimeout(r, 5000));

    console.log('[9/9] 验证 matrix 页...');
    page = await withTimeout(mp.currentPage(), 10000, 'currentPage after bind');
    const bindPass = page.path === 'pages/matrix/index';
    results.steps.push({ step: 'bind->matrix', result: bindPass ? 'PASS' : 'FAIL', actual: page.path });
    console.log(`[9/9] 跳转: ${page.path} (${bindPass ? 'PASS' : 'FAIL'})`);

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
  console.log('截图:', results.screenshots.join(', '));
  console.log('错误:', results.errors.length === 0 ? '无' : results.errors.join('\n'));
  const allPass = results.steps.length > 0 && results.steps.every(s => s.result === 'PASS') && results.errors.length === 0;
  console.log('总结:', allPass ? 'PASS' : 'FAIL');
  process.exitCode = allPass ? 0 : 1;
}

run();
