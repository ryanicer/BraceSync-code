# T101 自报 — 技师端 Android BLE 权限重检失败修复

## 基本信息
- **执行人**：Iris
- **分支**：`feat/iris-T101-android-ble-permission-fix`
- **提交**：`3c51d6d` feat(tech-miniapp): fix Android BLE location permission re-check after openSetting
- **PR**：https://github.com/ryanicer/BraceSync-code/pull/37

## git fsck 结果
```
git fsck --full
仅 dangling commit / blob（无 corrupt 或 missing 对象），仓库健康。
```

## 改动文件清单

| 文件 | 修复点 |
|------|--------|
| `apps/tech-miniapp/src/utils/ble.ts` | 1) `ensureLocationPermission` 前置 `wx.getSystemSetting` 检查系统位置服务开关；2) openSetting 返回后延迟 300ms 重新 `wx.getSetting` 取最新授权状态（不再依赖 openSetting 自身 authSetting）；3) `discoverDevices` 扫描失败时若为权限类错误，重新触发 `ensureLocationPermission` 引导授权 |
| `apps/tech-miniapp/src/manifest.json` | 新增 `mp-weixin.permission.scope.userLocation.desc` 权限描述声明 |

## 本地验证结果

| 检查项 | 结果 |
|--------|------|
| `npm -w apps/tech-miniapp run build:mp-weixin` | ✅ Build complete |
| `npm -w apps/tech-miniapp run test` | ✅ 13 tests passed |
| `npm run lint` | ✅ 通过 |
| CI (ci-fe.yml) Frontend Lint + Vitest | ✅ pass |
| CI Tech MiniApp E2E (Playwright, H5) | ✅ pass |
| CI Patient MiniApp E2E (Playwright, H5) | ✅ pass |
| CI Admin Web E2E (Playwright) | ✅ pass |

## 真机验证（待完成）

**场景 A（核心修复场景）**：首次拒绝位置权限 → 弹窗引导 → openSetting 开启位置权限 → 返回小程序 → 点击扫描 → 应直接进入扫描，不再弹权限拦截。

**场景 B（系统位置服务关闭）**：关闭手机位置服务 → 点击扫描 → 应提示「请开启手机位置服务」（非 openSetting 引导）。

**场景 C（已授权）**：已授权状态下点击扫描 → 正常扫描附近 BLE 设备。

**场景 D（iOS 回归）**：iOS 设备扫描流程不受影响（`getSystemSetting.locationEnabled` 在 iOS 通常不为 false）。

> 需小顾 1 台 + Iris 自测 1 台 Android 机型验证，或录屏覆盖「拒绝→设置授权→返回→扫描成功」完整路径。

## 患者端影响判断

**不影响患者端**。两端 BLE 代码完全独立：
- 技师端：`apps/tech-miniapp/src/utils/ble.ts`（含权限逻辑）
- 患者端：`apps/patient-miniapp/src/utils/ble.ts`（**无任何权限逻辑**，无 getSetting/authorize/openSetting）

本次仅修改技师端 2 个文件，未触碰患者端代码。

## 红线自查
- ✅ 未把权限判断写死为「必须全部权限同时 granted」才允许进入设置页
- ✅ 未以 mock 扫描结果绕过权限
- ✅ 未修改后端接口或 device-protocol，纯前端修复
- ✅ 未使用 stash/rebase/merge
- ✅ 未修改测试文件（仅 `test/login-validation.spec.ts`，与 BLE 无关）
