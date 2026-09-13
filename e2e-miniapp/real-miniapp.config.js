// T054 小程序真实模式 E2E 共享配置（CommonJS）。
// 与 T053 的 e2e-real/playwright.real.config.ts 平级隔离：这里是 miniprogram-automator（Node 直驱）
// 面向 staging 真实后端（USE_MOCK=false）的方案，端口为微信开发者工具自动化端口。
const path = require('path')
const { STAGING_URL, PROD_URL } = require('../scripts/wechat/lib-target')

module.exports = {
  // 目标后端：staging（本仓安全默认绝不碰生产）
  staging: STAGING_URL,
  prod: PROD_URL,

  // 自动化端口（CONNECT_ONLY 下由 start-automation.ps1 / 人工启动的 devtools 提供）
  ports: {
    tech: Number(process.env.AUTO_PORT || 9420),
    patient: Number(process.env.AUTO_PORT_PATIENT || 9422),
  },

  // 应用构建产物路径（drivers 用 lib-target 做 target 自证）
  appDir: (name) => path.resolve(__dirname, '..', 'apps', name),
  distDir: (name) => path.resolve(__dirname, '..', 'apps', name, 'dist', 'build', 'mp-weixin'),

  // token 存储键（对齐 apps/*/src/utils/token.ts）
  storage: {
    tech: { token: 'bracesync_tech_token', techId: 'bracesync_tech_id' },
    patient: { token: 'bracesync_token', patientId: 'bracesync_patient_id' },
  },

  // 造数据前缀（T053 惯例：唯一定义命名，跑完不强求清理）
  prefix: 'T054测试',

  // 运行产物（本地不入库，.gitignore 已ignore test-results/ 同类）
  resultsDir: path.resolve(__dirname, 'test-results'),
}