/**
 * T741 — 技师端配网/清除成功后主动断开 BLE 交接患者端
 *
 * Boss 实测：配网成功并清除 WiFi 后，技师端仍占着 BLE 连接，设备停在 BLE_SESSION
 * 不广播，患者端搜不到。设备侧已支持断开即回 BLE_WAIT。
 *
 * 为什么按源码结构断言：utils/ble.ts 在 vitest 里挂不起来（ble-log → logger 的
 * #ifdef 双声明，esbuild 不做条件编译），页面挂载成本高；先例见 b512-resubscribe.spec.ts。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'

const PAGE = fs.readFileSync(
  fileURLToPath(new URL('../src/pages/wifi-config/index.vue', import.meta.url)),
  'utf8'
)

/** 取顶层函数体：以下一个顶格 function/async function 声明为界 */
function fnBody(name: string): string {
  const start = PAGE.indexOf(`function ${name}(`)
  expect(start, `wifi-config 里找不到 function ${name}()——落点被挪走了？`).toBeGreaterThan(-1)
  const rest = PAGE.slice(start + name.length + 1)
  const next = rest.search(/\n(?:export )?(?:async )?function /)
  return next === -1 ? rest : rest.slice(0, next)
}

describe('T741 — 正常完成路径主动断开 BLE', () => {
  it('status 9 的 handleSuccess 与手动重试 retryClear 在 clear 成功后都走交接断开', () => {
    for (const name of ['handleSuccess', 'retryClear']) {
      const body = fnBody(name)
      const atSuccess = body.indexOf("clearState.value = 'success'")
      const atRelease = body.indexOf('await releaseDeviceForHandover()')
      const atTimer = body.indexOf('autoReturnTimer.value = setTimeout')
      expect(atSuccess, `${name} 缺 clear 成功分支`).toBeGreaterThan(-1)
      expect(atRelease, `${name} 清除成功后没有主动断开`).toBeGreaterThan(atSuccess)
      expect(atTimer, `${name} 自动返回定时器应排在断开之后`).toBeGreaterThan(atRelease)
    }
  })

  it('clear 失败分支不触发断开（还要用链路重试）', () => {
    for (const name of ['handleSuccess', 'retryClear']) {
      const body = fnBody(name)
      const atFailed = body.indexOf("clearState.value = 'failed'")
      expect(atFailed).toBeGreaterThan(-1)
      expect(body.slice(atFailed)).not.toContain('releaseDeviceForHandover')
    }
  })

  it('断开动作走 closeBLEConnection(deviceId)，成功后才置 handoverDone', () => {
    const body = fnBody('releaseDeviceForHandover')
    const atClose = body.indexOf('await closeBLEConnection(deviceId)')
    expect(atClose).toBeGreaterThan(-1)
    expect(body.indexOf('handoverDone.value = true', atClose)).toBeGreaterThan(atClose)
    expect(body).toContain('installStore.setBleDisconnected()')
  })

  it('主动断开不弹意外掉线误报', () => {
    expect(PAGE).toContain('if (!handoverClosing) uni.showToast({ title: BLE_LINK_DOWN_TOAST')
  })

  it('UI 在断开后给出指定交接文案', () => {
    expect(PAGE).toContain('class="handover-tip">设备已就绪，请使用患者端继续')
  })

  it('旧「不主动断开」口径已移除', () => {
    expect(PAGE).not.toContain('不主动 closeBLEConnection')
  })
})

describe('T741 — 与 T550 的 clear 等待器解耦', () => {
  it('doWifiClear 仍先装等待器再发 0x02', () => {
    const body = fnBody('doWifiClear')
    const atWait = body.indexOf('const clearWait = waitForWifiClear(10000)')
    const atSend = body.indexOf('await sendWifiClear(bleMac)')
    expect(atWait).toBeGreaterThan(-1)
    expect(atSend).toBeGreaterThan(atWait)
  })

  it('clear 逻辑内不掺入断开动作', () => {
    const body = fnBody('doWifiClear')
    expect(body).not.toContain('closeBLEConnection')
    expect(body).not.toContain('releaseDeviceForHandover')
  })
})
