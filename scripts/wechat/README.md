# scripts/wechat · 小程序构建 target 自证与冒烟脚本

本目录是 `scripts/wechat/` 的说明。核心目标：**构建/冒烟的目标永远显式、可见、缺省安全**。

## 1. 构建目标怎么指定（两端一致）

`npm -w apps/<app> run build:mp-weixin` 的缺省目标 = **staging**（安全默认，绝不静默落到生产）。
构建期会打印本次解析结果，例如：

```
[build] target=staging file=.env.staging API_BASE_URL=http://hbksd.com.cn:81 USE_MOCK=false
[build] 未显式指定 TARGET，缺省=staging（安全默认）
```

要显式指定目标，用环境变量 `TARGET`（`staging` | `prod`）：

- bash：
  ```bash
  TARGET=staging npm -w apps/patient-miniapp run build:mp-weixin
  TARGET=prod   npm -w apps/patient-miniapp run build:mp-weixin
  ```
- PowerShell：
  ```powershell
  $env:TARGET='staging'; npm -w apps/patient-miniapp run build:mp-weixin
  $env:TARGET='prod';   npm -w apps/patient-miniapp run build:mp-weixin
  ```

目标源文件（入库）：`.env.staging` → `http://hbksd.com.cn:81`（联调）；`.env.production` → `https://api.hbksd.com.cn`（正式）。
`.env.local` 始终最高优先（官方出包脚本 / 临时注入用）。

> 正式发布请走官方出包脚本（docs 仓 `scripts/miniapp-build/build-miniapp.mjs … prod`），后者本身就是显式 target。

## 2. 冒烟脚本自证 target（`smoke-patient.js` / `smoke-tech.js`）

连接已构建产物运行前，脚本会从 `dist/build/mp-weixin` **产物本身**解析实际打进去的后端地址并打印：

```
==================================================
[TARGET] API_BASE_URL=https://…  USE_MOCK=false
[TARGET] 产物命中 staging=3 处 / prod=0 处 → target=staging
==================================================
```

- 解析为 **prod**：默认**拒绝运行**（`process.exit(1)`），需显式 `ALLOW_PRODUCTION=1` 才放行并告警继续。
- 解析为 **staging**：正常继续。
- 未命中任何已知地址：告警后继续（结果谨慎判定）。

## 3. artifacts 与 debug 目录不入库

`artifacts/`（截图/日志等每次运行产生）与 `debug/` 属于本机运行产物、非源码，
已由根目录 `.gitignore` 忽略，**不入库**。如有要保留的经验，请写进本 README 或文档，
不要把截图/png 提交进仓库。