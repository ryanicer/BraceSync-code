import { test, expect, type Page } from '@playwright/test'
import { appendFileSync } from 'node:fs'
import { realLogin, getAuthToken, uniqueName } from '../real-helpers'

/**
 * T574 · 链 D 流程图配置全链路（真实模式 / staging）
 *
 * 六格按派发单 A1-A6 逐格给读数，判据 D1-D5 对应到 24.1-24.7。口径来源（全部现读，不采信转述）：
 *   路由表      services/user-service/internal/handler/flow_t274.go:7-9（接口清单：模板有 DELETE / 实例无）
 *   图校验      services/user-service/internal/handler/flow_t274.go:114-176（parseFlowGraph）
 *   端点键名    flow_t274.go:86-91（LogicFlow 2.x 的 sourceNodeId / targetNodeId）
 *   状态机      flow_t274.go:663-770（confirm 推进 / reject 置 skipped 不推进 / 无后继则 completed）
 *   分流口径    flow_t274.go:820-842（nextNodeIds 给出则只走列出的，且必须是该节点真实出边，否则 400）
 *   实例唯一    flow_t274.go:575-599（一条告警一个实例，重复启动回 409）
 *   入边为 0 的节点才是入口（flow_t274.go:169-174）⇒ 启动后 current 落在 trigger
 *   错误信封    handler.go:361-373（fail 出口）+ model/user_text.go:14-15 + model/model.go:17-27
 *   模板在用    repo/flow.go:282-305（被实例引用的模板 DELETE 回 ErrFlowTemplateInUse → 409）
 *
 * 🔴 T464 双通道（本文件判据口径的直接来源，不是我的取舍）：错误响应体 message 只承载
 *   model.UserText(code) 的中文用户面短句，handler 里的英文技术文本只写服务端日志
 *   （handler.go:362-366 注释与 logTechnical 调用是原文），data 恒为 null（handler.go:370），
 *   定位坐标改由 trace.errorCode + trace.requestId 回传（handler.go:353-354、371）。
 *   ⇒ 所有负对照的承重判据 = HTTP status + 业务 code + trace 两枚 + 逐形配对正对照 + 零落地反证；
 *     不许把「message 里能看到那一格」当判据（那会把设计好的双通道读成缺陷）。
 *     实测到的 message 原文逐条打进 stdout，作为现象交（见 D2 与 PR 正文）。
 *
 * 为什么这几条要串成一条链，而不是各端点单测：
 *   Go 侧 flow_t274_test.go 已把「建模板 / 坏边 / 403」按单元级别验过，但 e2e-real 侧对流程图
 *   **一条写请求都没发过**（03-alerts.spec.ts:447、450、459 全是 GET）。骨架的「配进去 → 跑起来 →
 *   回得来」这一段在真实环境里从没被走过 —— 本卡补的就是这一段。
 *
 * A2 是本卡的核心负对照：后端唯一的业务校验就是「节点 id 唯一 + 边端点必须指向存在的节点 + 禁自环」
 *   （flow_t274.go:126-165）。这一格若被改坏，图会静默落库、运行期才炸，所以四形
 *   （target 不存在 / source 不存在 / 自环 / 节点 id 重复）都要真的被拒，并且当场反证「没落库」
 *   （列表 total 与那颗名都没多出来）。**逐形配对正对照**（不只文末来一发）：同一张图只把被拒
 *   的那一处改对就必须收 —— 否则分不清「被这条业务校验拒了」与「这一形恰好还撞了别的锁」。
 *
 * 写纪律（快照-读数-还原-报备，四条都是断言不是承诺）：
 *   快照  建模板前取 templates total 与本前缀名册，并现读「无实例引用」的告警号；
 *   读数  每笔写后 GET 详情逐字段对平；
 *   还原  afterAll 独立于用例结果逐颗 DELETE /admin/flow/templates/:id，并逐颗定性删除结果；
 *   报备  删不掉的颗数、每颗的 HTTP/code/message 与 instanceCount 都打进 stdout。
 *   🔴 三处「还原做不到」是契约事实，本文件把它们钉在证据面上而不是藏起来：
 *      ① 实例侧没有删除端点（flow_t274.go:7-9 的接口清单里 DELETE 只有 templates 一条，instances 三条都是读/操作）⇒ 对已知实例发 DELETE
 *         应 404（24.7 断言），实例行只能推到终态、不能删行；
 *      ② 被实例引用的模板不可删（repo/flow.go:284-291 先数 flow_instance 引用，cnt>0 直接
 *         ErrFlowTemplateInUse → handler.go:507 转 409）⇒ 凡跑过实例的模板都留在架上。
 *      ③ T647 丙案起，24.0 前置会自清往轮留下的空壳模板行（详情 instanceCount=0 的那几颗）：这些行
 *         不是本 run 造的，本文件没有把它们建回去的通路，快照面只留 templateId/name/instanceCount，
 *         nodes 与 edges 不落证据面 ⇒ 这一笔删除不可还原，逐颗定性与颗数进 stdout 与 job summary。
 *      ⇒ 因此本文件的守恒律写成「收尾 total = 基线 total + 本轮在用残留颗数」，而不是「total 回到基线」；
 *        基线里本来就带着往轮的在用残留（跑过一次就永久留架，删不掉），所以 24.0 的基线判据不是
 *        「本前缀 0 颗」而是「在册的每一颗详情 instanceCount≥1」——都在用 = 可以接着跑，
 *        出现 instanceCount=0 的颗才是真漏删（要先把那几颗删掉再跑）。
 *        残留的每一颗都要有 instanceCount≥1 的读数作背书（漏删与在用可区分），
 *        而 instanceCount 只能取**详情面**：列表面那一列是 SQL 里的字面量 0
 *        （repo/flow.go:178-180，与 nodes/edges 写死 '[]' 同一处投影；详情面才走 :148-150 的真 COUNT）。
 *        实测背书：4 颗实例 + 两颗模板 DELETE 回 409「在用」，同一时刻名册里这两颗仍报 instanceCount=0。
 *        ⇒ 这条写进现象交（PR 正文 §残留 与卡内 D5）：任何按名册 instanceCount 判「能不能删」的消费方都会读空。
 *
 * 生产零写：入口只读 E2E_STAGING_URL，命中生产域名/生产 IP 直接抛（同 22 号用例口径）。
 * 不动既有面：不改 real-helpers.ts、不改 config、不动 03-alerts 的断言语义；callApi/callOk
 *   有意在本文件内复制 21/22 号用例的写法而不提进 helper（提出去要动已交件的 helper）。
 */

const ENTRY = process.env.E2E_STAGING_URL ?? 'http://localhost:2080'
if (/api\.hbksd\.com\.cn/.test(ENTRY)) { // 49.235.137.217 自 2026-10-11 起为 TST（Boss 口径，TST 写段经批 A 授权）；生产针保留 api.hbksd.com.cn
  throw new Error(`T574 链 D 命中生产入口，红线拒绝：${ENTRY}`)
}

/** 造数名前缀（与真实数据区分；派发单 §五 的 T053 口径） */
const FLOW_TPL_PREFIX = 'T053流程'
/**
 * T647 丙案：24.0 前置自清的单轮颗数上限。这枚数还没有现读背书（红发都停在断言那一行之前，
 * 名册颗数的一手读数取不到），所以首拍把在册颗数打进 summary 交接班席校准；超上限一律不改数据、照旧抛停手句。
 */
const SELF_CLEAN_CAP = 20
/** 全仓未注册的路径：负对照用（缺了它，「JSON 信封 code=10400」分不清是路由在架还是网关兜底） */
const NOT_REGISTERED_PATH = '/api/v1/zzz-t574-chain-d-flow-not-registered-9c2f'
/** A6 的两个非 admin 角色账号（staging 既有测试账号，口径同 03-alerts.spec.ts 与 21 号用例） */
const DOCTOR_ACCOUNT = 'doctor_li'
const TECH_ACCOUNT = 'cs_wang'

interface FlowNode {
  id: string
  type: string
  x: number
  y: number
  text: { value: string }
  properties: { kind: string }
}
interface FlowEdge {
  id: string
  type: string
  sourceNodeId: string
  targetNodeId: string
}
interface TemplateDTO {
  templateId: string
  name: string
  nodes: FlowNode[]
  edges: FlowEdge[]
  version: number
  instanceCount: number
  createdAt: string
  updatedAt: string
}
interface InstanceDTO {
  instanceId: string
  templateId: string
  templateName: string
  alertId: string
  currentNodeId: string | null
  status: string
  startedAt: string
  endedAt: string | null
}
interface NodeStateDTO {
  nodeId: string
  status: string
  nextNodeIds: string[]
  operator: string | null
  remark: string | null
  assignee: string | null
}
interface ActionDTO {
  actionId: string
  nodeId: string
  nodeName: string | null
  action: string
  actionLabel: string
}
/** T464 定位通道：错误响应必带 trace{errorCode, requestId}，成功响应无该键 */
interface ErrorTrace {
  errorCode: number | null
  requestId: string | null
}
interface Envelope {
  status: number
  contentType: string
  code: number | null
  message: string
  data: unknown
  trace: ErrorTrace | null
  /** 原始响应体文本（非 JSON 时用它证明「不是业务信封」，负对照的第三枚坐标） */
  rawHead: string
}

async function callApi(
  p: Page,
  method: string,
  urlPath: string,
  opts: { token?: string; body?: unknown } = {},
): Promise<Envelope> {
  const headers: Record<string, string> = {}
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await p.request.fetch(urlPath, {
    method,
    headers,
    data: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  })
  const text = await res.text().catch(() => '')
  let code: number | null = null
  let message = ''
  let data: unknown = null
  let trace: ErrorTrace | null = null
  try {
    const env = JSON.parse(text) as {
      code?: unknown
      message?: unknown
      data?: unknown
      trace?: { errorCode?: unknown; requestId?: unknown } | null
    }
    if (typeof env.code === 'number') code = env.code
    if (typeof env.message === 'string') message = env.message
    data = env.data ?? null
    if (env.trace !== null && typeof env.trace === 'object') {
      trace = {
        errorCode: typeof env.trace.errorCode === 'number' ? env.trace.errorCode : null,
        requestId: typeof env.trace.requestId === 'string' ? env.trace.requestId : null,
      }
    }
  } catch {
    /* 非 JSON：code 留 null，正是负对照要的形态 */
  }
  return {
    status: res.status(),
    contentType: (res.headers()['content-type'] ?? '').split(';')[0],
    code,
    message,
    data,
    trace,
    rawHead: text.slice(0, 120),
  }
}

async function callOk<T>(
  p: Page,
  method: string,
  urlPath: string,
  opts: { token?: string; body?: unknown; why: string },
): Promise<T> {
  const r = await callApi(p, method, urlPath, opts)
  expect(
    r.code,
    `${opts.why}：${method} ${urlPath} 应回 code=0，实得 status=${r.status} code=${r.code} message=${r.message}`,
  ).toBe(0)
  return r.data as T
}

/**
 * T582 格一：把「只作自证、不作门禁」的快照送进 job summary。
 * GITHUB_STEP_SUMMARY 由 runner 注入给每一步；本地跑没有它，此时只留 stdout（那句会当场说明，不留静默）。
 * 写失败只登记、不判红：这一格是读数出口，不是被测行为。
 */
function appendJobSummary(text: string): void {
  const file = process.env.GITHUB_STEP_SUMMARY ?? ''
  if (!file) {
    console.log('[t574-chain-d][快照] 未设 GITHUB_STEP_SUMMARY（本地跑）⇒ 快照只在 stdout，不进 job summary')
    return
  }
  try {
    appendFileSync(file, `- ${text}\n`, 'utf8')
  } catch (e) {
    console.log(`[t574-chain-d][快照] 写 job summary 失败（不影响用例结论）：${String(e)}`)
  }
}

/** LogicFlow 2.x 图元：type 是画布形状，properties.kind 是 8 类节点（前端 kinds.ts 的口径，后端不校验） */
const fn = (id: string, kind: string, shape: string, label: string, x: number): FlowNode => ({
  id,
  type: shape,
  x,
  y: 120,
  text: { value: label },
  properties: { kind },
})
const fe = (id: string, from: string, to: string): FlowEdge => ({
  id,
  type: 'polyline',
  sourceNodeId: from,
  targetNodeId: to,
})

/** 线性图：trigger → process → archive（派发单 A1 要求的最小三节点形状） */
const LIN = {
  n1: 't574-l-trigger',
  n2: 't574-l-process',
  n3: 't574-l-archive',
  nodes: (): FlowNode[] => [
    fn('t574-l-trigger', 'trigger', 'circle', '触发', 120),
    fn('t574-l-process', 'process', 'rect', '处理', 320),
    fn('t574-l-archive', 'archive', 'rect', '归档', 520),
  ],
  edges: (): FlowEdge[] => [fe('t574-l-e1', 't574-l-trigger', 't574-l-process'), fe('t574-l-e2', 't574-l-process', 't574-l-archive')],
}

/** 分叉图：trigger → condition →（通过 / 不通过），A4 的两组输入就是在这两个后继之间选 */
const BR = {
  nodes: (): FlowNode[] => [
    fn('t574-b-trigger', 'trigger', 'circle', '触发', 120),
    fn('t574-b-cond', 'condition', 'diamond', '判断', 320),
    fn('t574-b-yes', 'process', 'rect', '通过分支', 520),
    fn('t574-b-no', 'process', 'rect', '不通过分支', 520),
  ],
  edges: (): FlowEdge[] => [
    fe('t574-b-e1', 't574-b-trigger', 't574-b-cond'),
    fe('t574-b-e2', 't574-b-cond', 't574-b-yes'),
    fe('t574-b-e3', 't574-b-cond', 't574-b-no'),
  ],
}

test.describe('24-链 D 流程图全链路（T574，A1-A6）', () => {
  // 用例之间有真实依赖（24.1 建的模板给 24.3 跑实例，24.3 的实例给 24.5/24.7 对平）⇒ 显式串行。
  // 首跑实测：非串行时 24.2 失败后 Playwright 换了 worker，模块级状态被清零，
  // 24.3-24.7 全红在读到空串的自建守卫上（把「上一格没跑成」误报成契约问题）。
  test.describe.configure({ mode: 'serial' })

  let adminToken = ''
  /** 本文件建过的模板号，afterAll 逐颗删并逐颗定性 */
  const madeTemplates: string[] = []
  /** 24.0 时已在册的本前缀模板号（往轮「在用不可删」的永久残留，见文件头②），守恒律要把它算进基线 */
  let preexistingTplIds: string[] = []
  let baselineTplTotal: number | null = null
  /** 本文件成功起起来的实例颗数（实例行删不掉 ⇒ 残留量必须有个可对平的数字） */
  let instancesStarted = 0
  /** 已经用掉的告警号（一条告警一个实例，重复起会 409 ⇒ 每格现读一颗新的） */
  const usedAlerts = new Set<string>()
  let linTemplateId = ''
  let linTemplateName = ''
  let linAlertId = ''

  /** 会话自愈：串行下通常复用 24.0 的令牌，一旦为空就重新登录，不把承重前提押在内存状态上 */
  async function ensureAdmin(page: Page): Promise<string> {
    if (adminToken === '') {
      await realLogin(page)
      adminToken = (await getAuthToken(page)) ?? ''
      expect(adminToken, '管理端登录没回令牌（localStorage admin_token 缺失）').toBeTruthy()
    }
    return adminToken
  }

  /* ───────── T610：取号口径（这一组替换掉原先只读最新一窗的那条腿） ─────────
   *
   * 病不在判据而在候选面：旧腿只读 `/api/v1/alerts?page=1&pageSize=20` 那一窗，而告警面是
   * `ORDER BY ts DESC`（repo/query.go:164）——值班 PM 08:49 实测四条：告警共 118 颗、有流程实例的
   * 只 22 颗、无实例 96 颗、按旧口径取的那 20 颗恰好全在「有实例」那 22 颗里。数据池够用，
   * 是取号方式把候选面押死在最新一窗。甲案＝只改取号（不新建告警、不清数据、不动断言）。
   *
   * 后端没有「按无实例过滤」的入参（public.go:177-206 只认 patientId/type/status/page/pageSize，
   * flow 实例面又必须带 alertId 单查，见 flow_t274.go:601-623），所以条件只能用例侧逐颗现读，
   * 但候选面换成全池按页推进。告警面 pageSize 上限 100（repo/query.go:24），一次装下 100 颗。
   */
  const ALERT_PAGE_SIZE = 100
  /** 一轮要用的颗数：24.3 一颗 + 24.4 三颗；队列按这个数建，够用即止，不整池探穿 */
  const FREE_QUEUE_TARGET = 4
  /** 扫描封顶（颗）：池再长也只消费到这里，扫到顶仍 0 空闲才停手 */
  const MAX_SCAN_ALERTS = 300
  let freeQueue: string[] = []
  let scanPages = 0
  let scanCursor = 0
  let scanTotal: number | null = null
  const idWindow: string[] = []
  /** 实例面缓存（颗号→实例颗数）：24.0 快照腿与建队腿对同一颗不重复发读 */
  const probedInstances = new Map<string, number>()

  function faceLine(suffix: string): string {
    const pool = scanTotal === null ? '未读' : String(Math.min(scanTotal, MAX_SCAN_ALERTS))
    return (
      `[t610-取号] 无实例条件全池建队｜池total=${scanTotal ?? '未读'} 扫描上限=${pool} 已消费=${scanCursor} ` +
      `实探=${probedInstances.size} 翻页=${scanPages} 队列剩=${freeQueue.length}｜${suffix}` +
      // 🔴 E-130（2026-10-07 16:2x）：本行四个数都不是「被占数」，逐个标名，
      //   免得读的人拿其中一个当占用量（旧句「共 118 颗」就是这么被读成「池耗尽」的）。
      `｜口径：池total=告警面声明总数｜扫描上限=本 run 最多扫几颗｜已消费=扫过几颗｜` +
      `实探=逐颗现读实例数几颗｜队列剩=判为无实例、待取的颗数｜被占数不在本行，须按 alertId 反查实例面另取`
    )
  }

  /** 逐颗现读实例数（带缓存，缓存只为不重复发同一发读） */
  async function instanceCountOf(p: Page, token: string, id: string): Promise<number> {
    const hit = probedInstances.get(id)
    if (hit !== undefined) return hit
    const inst = await callOk<{ list: InstanceDTO[] }>(p, 'GET', `/api/v1/admin/flow/instances?alertId=${id}`, {
      token,
      why: `按告警取实例读不通（告警 ${id}）⇒「无流程实例」这一条件无法自证`,
    })
    probedInstances.set(id, inst.list.length)
    return inst.list.length
  }

  /** 按「无流程实例」条件在全池按页推进，把候选告警号推进队列；返回本次新增颗数 */
  async function fillFreeQueue(p: Page, token: string, need: number): Promise<number> {
    if (scanTotal === null) {
      const head = await callOk<{ total: number }>(p, 'GET', '/api/v1/alerts?page=1&pageSize=1', {
        token,
        why: '告警面 total 读不通 ⇒ 全池边界无从落值',
      })
      scanTotal = head.total
    }
    const before = freeQueue.length
    while (freeQueue.length - before < need && scanCursor < Math.min(scanTotal, MAX_SCAN_ALERTS)) {
      if (idWindow.length === 0) {
        scanPages += 1
        const pageOfIds = await callOk<{ list: { alertId: number | string }[] }>(
          p,
          'GET',
          `/api/v1/alerts?page=${scanPages}&pageSize=${ALERT_PAGE_SIZE}`,
          { token, why: '告警分页读不通 ⇒ 拿不到可起实例的告警' },
        )
        if (pageOfIds.list.length === 0) {
          scanTotal = scanCursor // 翻到空页＝池比 total 声明的短，边界收到这里
          break
        }
        for (const a of pageOfIds.list) idWindow.push(String(a.alertId))
        if (idWindow.length < ALERT_PAGE_SIZE) scanTotal = scanCursor + idWindow.length // 末页：按实际颗数收边
      }
      const id = idWindow.shift() as string
      scanCursor += 1
      if (usedAlerts.has(id) || freeQueue.includes(id)) continue
      if ((await instanceCountOf(p, token, id)) === 0) freeQueue.push(id)
    }
    return freeQueue.length - before
  }

  /**
   * 停手句：不再猜号、直接停手报 PM ＋ 可执行下一步指针。
   *
   * 🔴 E-130（2026-10-07 16:2x，值班 PM 实测后改写）：本句旧版把「授权新建无实例告警」列成
   *   待裁第②项，但那一格（造峰）已被 12:0x 裁定**不批** ——「为满足判据去造数据= 测试反过来
   *  塑造被测系统」。留着它会让下一个跑红的人去请一个已被否的授权。
   *   ⇒ ②改成「先读占用侧实况，不造数」；造峰与真清理都不再是本停手句的出口。
   *
   * 另一处已改：faceLine 里 `扫描上限` 与 `池total` 在池小于上限时**两数同值**，
   *   与旧句「前 20 颗…（共 118 颗）」是同一类歧义（两个数并排、无标签 ⇒ 读的人会把总数读成被占数，
   *   本席 15:2x 就真这么读错过一次，报了「池耗尽」而实际空闲 88）。现每个数都带名。
   */
  function stopAndReport(): Error {
    return new Error(
      faceLine('空闲=0') +
        ' 本文件不再猜号，直接停手报 PM。下一步按可执行口径走（不是只留一句停手）：' +
        '① 把本行读数原样贴进 T610 卡评论并点名值班 PM；' +
        '② 先读占用侧实况再判：「实探」是本 run 逐颗现读的颗数，「已消费」是扫过的颗数，' +
        '两者都不是被占数——被占数须按 alertId 逐颗反查实例面另取（QA 判据：资源结论只认总量/已占/空闲三颗数，' +
        '不认任何一句报错文案里的数字）；' +
        '③ 出口只有两条，**都不造数**：甲= 等真实告警事件流进来（本停手句默认走这条）；' +
        '乙= 清理在用实例（须Boss 书面授权，本句不代请）；' +
        '④ 判据不动，直接复跑：npx playwright test --config=e2e-real/playwright.real.config.ts -g "24.3"',
    )
  }

  /** 取号：队列见底就按无实例条件补建；取用前再现读一次（实例数必须现读为 0，不吃建队那一面的旧账） */
  async function takeFreeAlert(p: Page, token: string): Promise<string> {
    for (let round = 0; round < 8; round++) {
      if (freeQueue.length === 0 && (await fillFreeQueue(p, token, FREE_QUEUE_TARGET)) === 0) break
      while (freeQueue.length > 0) {
        const id = freeQueue.shift() as string
        const now = await callOk<{ list: InstanceDTO[] }>(p, 'GET', `/api/v1/admin/flow/instances?alertId=${id}`, {
          token,
          why: `取号复现读不通（告警 ${id}）⇒ 用前那一面读不到就不起实例`,
        })
        probedInstances.set(id, now.list.length)
        if (now.list.length === 0) {
          usedAlerts.add(id)
          console.log(faceLine(`取到号=${id} 用前现读实例=0`))
          return id
        }
        // 建队之后被别的写腿占了：这颗不再算空闲，回队列看下一颗
        usedAlerts.add(id)
      }
    }
    throw stopAndReport()
  }

  /**
   * 模板详情（带真 instanceCount）。
   * 🔴 对平必须走详情面，不能走列表面：列表面投影把 instanceCount 写死成 0
   *   （repo/flow.go:178-180 的 SELECT 里那一列就是字面量 `0`，与 nodes/edges 写死 '[]' 同一处口径），
   *   只有详情面走 flowTemplateCols 的子 SELECT 真 COUNT（repo/flow.go:148-150）。
   *   实测背书：本轮 4 颗实例、2 颗模板 DELETE 回 409「在用」，而名册里这两颗的 instanceCount 仍报 0。
   */
  async function detailOf(p: Page, token: string, templateId: string): Promise<TemplateDTO> {
    return callOk<TemplateDTO>(p, 'GET', `/api/v1/admin/flow/templates/${templateId}`, {
      token,
      why: `模板 ${templateId} 详情读不通（instanceCount 的对平腿就在这一面上）`,
    })
  }

  /**
   * T647 丙案：把「往轮漏删的空壳」从「报给人清」改成本文件自己清一遍，通路就是 afterAll 每轮都在用的
   * 那条 admin 模板单删（文件头 41 行），作用域锁死在按 FLOW_TPL_PREFIX 筛出来的名册里，
   * 越出前缀即零动作（不碰 alerts、不碰实例行、不碰 seed、不碰非本前缀模板）。
   * 四纪律落三件：快照（删前逐颗 templateId/name/instanceCount）＋读数（删后逐颗定性）＋报备
   * （stdout 与 job summary 两头都不得静默）；「还原」对模板单删做不到，这是文件头两处之外的第三处，
   * 钉在证据面上而不是藏起来。颗数超上限时返回 overCap，由调用方抛停手句，本腿不改数据。
   */
  async function cleanPrefixShells(
    p: Page,
    token: string,
    tag: string,
  ): Promise<{ scanned: number; deleted: string[]; refusedInUse: string[]; unexpected: string[]; shellCount: number; overCap: boolean }> {
    const list = await callOk<{ list: TemplateDTO[]; total: number }>(p, 'GET', '/api/v1/admin/flow/templates?pageSize=100', {
      token,
      why: '自清前的模板列表读不通（作用域尺就立在这一枚面上）',
    })
    expect(list.list.length, `名册尺要成立，pageSize=100 必须装得下全量（实回 ${list.list.length}/${list.total}）`).toBe(list.total)
    const mine = list.list.filter((t) => t.name.startsWith(FLOW_TPL_PREFIX))
    const shells: TemplateDTO[] = []
    for (const t of mine) {
      const d = await detailOf(p, token, t.templateId)
      if (d.instanceCount < 1) shells.push(d)
    }
    const line =
      `[t647-自清][${tag}] 在册本前缀=${mine.length} 空壳=${shells.length} 单轮上限=${SELF_CLEAN_CAP} ` +
      `逐颗=${shells.map((d) => `${d.name}(${d.templateId}) instanceCount=${d.instanceCount}`).join(' ; ') || '无'}`
    console.log(line)
    test.info().annotations.push({ type: 't647-self-clean-snapshot', description: `自清快照（删前逐颗现读）：${line}` })
    appendJobSummary(line)
    const out = { scanned: mine.length, deleted: [] as string[], refusedInUse: [] as string[], unexpected: [] as string[], shellCount: shells.length, overCap: shells.length > SELF_CLEAN_CAP }
    if (out.overCap) return out
    for (const d of shells) {
      const r = await callApi(p, 'DELETE', `/api/v1/admin/flow/templates/${d.templateId}`, { token })
      if (r.code === 0) out.deleted.push(d.templateId)
      // 建队之后被别的写腿占了实例 ⇒ 这颗改判「在用不可删」，留在名册里，不算自清失败
      else if (r.status === 409 && r.code === 10409) out.refusedInUse.push(d.templateId)
      else out.unexpected.push(`${d.templateId}: HTTP=${r.status} code=${String(r.code)} message=${r.message}`)
      console.log(
        `[t647-自清][${tag}] 删 ${d.name}(${d.templateId}) ⇒ HTTP=${r.status} code=${String(r.code)} message=${r.message} 定性=${r.code === 0 ? '删成' : r.status === 409 && r.code === 10409 ? '在用不可删（改判）' : '第三种读数'}`,
      )
    }
    return out
  }

  test('24.0 前置：管理端会话在架、路由形状确认、基线快照与空闲告警面现读', async ({ page }) => {
    const token = await ensureAdmin(page)

    // 负对照前置：未注册路径必须回非 JSON 的 gin 404 —— 缺了它，后面「code=10400」分不清路由在不在架
    for (const method of ['POST', 'DELETE'] as const) {
      const r = await callApi(page, method, NOT_REGISTERED_PATH, { token })
      expect(
        r.code,
        `前置探针失效：未注册路径 ${method} 应回非 JSON 的 404，实得 status=${r.status} code=${r.code} ct=${r.contentType} rawHead=${r.rawHead}`,
      ).toBeNull()
    }

    // 「在用不可删」的残留会永久留在架上（文件头②），所以基线不许写「本前缀 0 颗」，要改成逐颗现读详情核
    // 「残留都在用」：instanceCount=0 却还在册 = 往轮漏删。T647 丙案把这一类从「报给人清」改成本腿自清
    // （通路就是 afterAll 每轮都在用的那条模板单删），清完再按原口径复尺断言，牙不丢；
    // 基线与往轮残留名册一律取自**清后**那一枚面，否则 afterAll 的行数守恒会把自清掉的颗数算成漏删。
    const preClean = await callOk<{ list: TemplateDTO[]; total: number }>(page, 'GET', '/api/v1/admin/flow/templates?pageSize=100', {
      token,
      why: '模板列表读不通',
    })
    expect(preClean.list.length, `名册尺要成立，pageSize=100 必须装得下全量（实回 ${preClean.list.length}/${preClean.total}）`).toBe(preClean.total)
    const cleaned = await cleanPrefixShells(page, token, '前置')
    expect(
      cleaned.unexpected,
      `自清的删除结果只该有两种（删成 / 在用 409），出现第三种就是端点变化或越界：${cleaned.unexpected.join(' ; ') || '无'}`,
    ).toEqual([])
    if (cleaned.overCap) {
      const over = new Error(
        `[t647-自清][前置] 本前缀空壳 ${cleaned.shellCount} 颗超过单轮上限 ${SELF_CLEAN_CAP} 颗 ⇒ 本腿不改数据、照旧停手：` +
          `清前 templates total=${preClean.total} 在册本前缀=${cleaned.scanned}。需要谁：T639 侧的授权清理席先确认这几颗的来历，本卡不代删、不放宽上限。`,
      )
      console.log(over.message)
      appendJobSummary(over.message)
      throw over
    }
    const list = await callOk<{ list: TemplateDTO[]; total: number }>(page, 'GET', '/api/v1/admin/flow/templates?pageSize=100', {
      token,
      why: '自清后的模板列表读不通（基线要取这一枚面，不是清前那枚）',
    })
    baselineTplTotal = list.total
    const mine = list.list.filter((t) => t.name.startsWith(FLOW_TPL_PREFIX))
    expect(list.list.length, `名册尺要成立，pageSize=100 必须装得下全量（实回 ${list.list.length}/${list.total}）`).toBe(list.total)
    preexistingTplIds = mine.map((t) => t.templateId)
    const prevUnsupported: string[] = []
    for (const t of mine) {
      const d = await detailOf(page, token, t.templateId)
      if (d.instanceCount < 1) prevUnsupported.push(`${d.name} instanceCount=${d.instanceCount}`)
    }
    expect(
      prevUnsupported,
      `自清之后仍有 ${prevUnsupported.length} 颗详情 instanceCount=0（这一类删得动却没删掉，是自清腿的缺陷，不是往轮账）：${prevUnsupported.join(' ; ') || '无'}`,
    ).toEqual([])
    console.log(
      `[t574-chain-d][快照] templates total=${baselineTplTotal} 本前缀在册颗数=${mine.length}（详情面 instanceCount 逐颗≥1 已核，都是「在用不可删」的往轮残留）` +
        `｜本轮自清 清前total=${preClean.total} 空壳=${cleaned.shellCount} 删成=${cleaned.deleted.length} 改判在用=${cleaned.refusedInUse.length}`,
    )

    // 合成夹具格（T647 验收第一格）：自然名册本来就干净时，上面那串断言会空绿，所以这里造一颗
    // 「只建模板、不起实例」的前缀空壳逼自清腿真删一次。这一格证「前置有自愈的牙」，
    // 与现网格（staging 当前那几颗空壳）各证一半，两格不可替代。
    const fixtureName = uniqueName(FLOW_TPL_PREFIX)
    const fixture = await callOk<TemplateDTO>(page, 'POST', '/api/v1/admin/flow/templates', {
      token,
      body: { name: fixtureName, nodes: LIN.nodes(), edges: LIN.edges() },
      why: '夹具建模板应回 code=0（这一格的前提是造得出空壳）',
    })
    // 🔴 不推进 madeTemplates：这颗由本格的自清腿当场删掉，再让 afterAll 删第二次就会读出第三种结果。
    const fixtureClean = await cleanPrefixShells(page, token, '夹具')
    expect(
      fixtureClean.overCap,
      `夹具轮撞上单轮上限（在册空壳=${fixtureClean.shellCount} 颗 > ${SELF_CLEAN_CAP}）⇒ 这一格判不了自清有没有牙，按上限停手口径交人，不放宽`,
    ).toBe(false)
    expect(
      fixtureClean.unexpected,
      `夹具轮的删除结果同样只该有两种（删成 / 在用 409）：${fixtureClean.unexpected.join(' ; ') || '无'}`,
    ).toEqual([])
    expect(
      fixtureClean.deleted.includes(fixture.templateId),
      `夹具那颗 ${fixtureName}(${fixture.templateId}) 没被自清腿删掉（deleted=${fixtureClean.deleted.join(' ; ') || '空'}）⇒ 前置的自愈是假的`,
    ).toBe(true)
    const afterFixture = await callOk<{ list: TemplateDTO[]; total: number }>(page, 'GET', '/api/v1/admin/flow/templates?pageSize=100', {
      token,
      why: '夹具轮的收尾列表读不通',
    })
    expect(
      afterFixture.list.filter((t) => t.templateId === fixture.templateId).length,
      `夹具那颗删后仍在本前缀名册里（回读 id=${fixture.templateId}）⇒ 单删只删了行没出名册`,
    ).toBe(0)
    expect(
      afterFixture.total,
      `夹具造一颗又删一颗，收尾 total 应回到基线 ${baselineTplTotal}（实回 ${afterFixture.total}）⇒ 净零不成立，基线与守恒律的账都会被这格带歪`,
    ).toBe(baselineTplTotal)

    // 实例面基线（T582 格一改自证式）：现读**整页**每一颗告警各自的实例颗数，只作快照，不再对 free 作门禁断言。
    // 为什么这一格不该当门禁（口径来自派发单 §四 格一 允许方向②，不是「放宽」）：
    //   ① free 从来不是本文件的真实前置。真正取号的是 takeFreeAlert，T610 起它按「无流程实例」条件
    //      在全池按页推进并逐颗现读，扫到顶仍 0 空闲才抛停手句——那一红才是 A3/A4 起不了实例的真读数。
    //      24.0 原先只看前 10 颗，是一把比自身用途更严的尺：它红的时候后面几条未必跑不动
    //      （2026-10-06 02:12 只读实测：total=116、整页 20 颗里空闲=8、全库空闲=104、已起实例=12）。
    //   ② 同时禁两条歪路：把断言改成 free >= 0（空断言，抹平症状）或把这一格自己的分母往后翻页凑数。
    //      取号腿翻页不算这一条：它换的是候选面（最新一窗→全池），门禁的读数照旧是本页那一面。
    //   ③ 数据面自检的实质留在断言里，且只断实质：告警面在架（total>0）、本页非空（list>0）、
    //      逐颗读数按预期形态回（callOk 已把 HTTP/code 非绿的每一发当场判红并带坐标）。
    // 出口三处：stdout（run 日志）+ annotation（HTML 报告）+ job summary（不得静默）。
    const alerts = await callOk<{ list: { alertId: number | string }[]; total: number }>(page, 'GET', '/api/v1/alerts?page=1&pageSize=20', {
      token,
      why: '告警列表读不通',
    })
    expect(alerts.total, '告警面 total=0 ⇒ staging 没 seed 告警，本文件与所有链路都无从跑起（这是数据面缺口，不是本用例的判据）').toBeGreaterThan(0)
    expect(alerts.list.length, '告警面 total>0 而本页回 0 颗 ⇒ 分页面读空，取数姿势有问题').toBeGreaterThan(0)

    const faceRows: { alertId: string; instances: number }[] = []
    for (const a of alerts.list) {
      const id = String(a.alertId)
      faceRows.push({ alertId: id, instances: await instanceCountOf(page, token, id) })
    }
    const free = faceRows.filter((r) => r.instances === 0).length
    const freeIn10 = faceRows.slice(0, 10).filter((r) => r.instances === 0).length
    const snapshotLine =
      `[t574-chain-d][快照] 告警 total=${alerts.total} 本页=${faceRows.length} 空闲(整页)=${free} 空闲(前10)=${freeIn10} ` +
      `逐颗=${faceRows.map((r) => `${r.alertId}:${r.instances}`).join(' ')}`
    console.log(snapshotLine)
    test.info().annotations.push({
      type: 'chain-d-alert-face',
      description: `实例面快照（自证式，非门禁）：${snapshotLine}`,
    })
    appendJobSummary(snapshotLine)

    // T610 第二面（仍只读，零写腿）：按「无流程实例」条件在全池建队后，队首候选现读实例数必须为 0。
    // 这一格是 A3/A4 的取号前提，放在 24.0 里先亮出来——今天那条红（24.4 起不到实例）在这一步就该看得见。
    const queued = await fillFreeQueue(page, token, FREE_QUEUE_TARGET)
    const peekId = freeQueue.length > 0 ? freeQueue[0] : ''
    const peekCount = peekId === '' ? -1 : await instanceCountOf(page, token, peekId)
    const faceOut = faceLine(`首窗空闲=${free}（本页 ${faceRows.length} 颗） 建队新增=${queued} 队首候选=${peekId || '无'} 队首现读实例=${peekCount}`)
    console.log(faceOut)
    test.info().annotations.push({ type: 't610-free-alert-face', description: `取号口径面（自证式）：${faceOut}` })
    appendJobSummary(faceOut)
    if (peekId !== '') {
      expect(peekCount, `队首候选 ${peekId} 现读实例数应为 0，否则「无流程实例」这个条件名不副实`).toBe(0)
    }
  })

  test('24.1 A1 建模板 → 详情逐字段值级回读（不是只断言「有 nodes 数组」）', async ({ page }) => {
    const token = await ensureAdmin(page)
    const name = uniqueName(FLOW_TPL_PREFIX)
    const created = await callOk<TemplateDTO>(page, 'POST', '/api/v1/admin/flow/templates', {
      token,
      body: { name, nodes: LIN.nodes(), edges: LIN.edges() },
      why: 'A1 建模板应 200',
    })
    madeTemplates.push(created.templateId)
    linTemplateId = created.templateId
    linTemplateName = name
    expect(created.templateId, '建模板应回模板号').toBeTruthy()
    expect(created.name, '回读的 name 应等于写入值').toBe(name)

    const detail = await callOk<TemplateDTO>(page, 'GET', `/api/v1/admin/flow/templates/${created.templateId}`, {
      token,
      why: 'A1 详情读回应 200',
    })
    expect(detail.nodes.length, '节点数应等于写入的 3 颗').toBe(3)
    expect(detail.nodes.map((n) => n.id), '节点 id 序列应逐项等于写入值').toEqual(LIN.nodes().map((n) => n.id))
    expect(detail.nodes.map((n) => n.properties.kind), '每个节点的 kind 应逐项等于写入值').toEqual(['trigger', 'process', 'archive'])
    expect(detail.nodes.map((n) => n.text.value), '节点展示名应逐项等于写入值').toEqual(['触发', '处理', '归档'])
    expect(detail.edges.map((e) => [e.sourceNodeId, e.targetNodeId]), '边的端点对应逐项等于写入值').toEqual([
      ['t574-l-trigger', 't574-l-process'],
      ['t574-l-process', 't574-l-archive'],
    ])
    expect(detail.version, '首建版本应为 1').toBe(1)
    expect(detail.instanceCount, '刚建的模板实例数应为 0').toBe(0)

    const listAfter = await callOk<{ list: TemplateDTO[]; total: number }>(page, 'GET', '/api/v1/admin/flow/templates?pageSize=100', {
      token,
      why: 'A1 建后列表读不通',
    })
    expect(listAfter.total, `建一颗后 total 应等于基线 +1（基线 ${baselineTplTotal}，实得 ${listAfter.total}）`).toBe(baselineTplTotal! + 1)
    expect(listAfter.list.map((t) => t.templateId)).toContain(created.templateId)
    console.log(`[t574-chain-d][A1] templateId=${created.templateId} name=${name} 节点=${detail.nodes.length} 边=${detail.edges.length} version=${detail.version}`)
  })

  test('24.2 A2 坏图四形必须被拒（本卡核心负对照）＋ 逐形配对正对照 ＋ 零落地反证', async ({ page }) => {
    const token = await ensureAdmin(page)
    const readTemplates = async (): Promise<{ total: number; list: TemplateDTO[] }> => {
      const r = await callOk<{ total: number; list: TemplateDTO[] }>(
        page,
        'GET',
        '/api/v1/admin/flow/templates?pageSize=100',
        { token, why: 'A2 的列表读不通（零落地反证要靠名册，读不通就不能定性）' },
      )
      expect(r.list.length, `名册尺要成立，pageSize=100 必须装得下全量（实回 ${r.list.length}/${r.total}）`).toBe(r.total)
      return r
    }

    // 每一形只让「被检的那一处」不合法，并给一处改对的配对：
    // 负对照证明被拒，配对证明同一条请求改那一处就收 ⇒ 拒的是这条规则，不是别的锁抢先。
    const shapes: {
      why: string
      rule: string
      badNodes: FlowNode[]
      badEdges: FlowEdge[]
      fixedNodes: FlowNode[]
      fixedEdges: FlowEdge[]
    }[] = [
      {
        why: '边指向不存在的节点',
        rule: 'flow_t274.go:160-162 targetNodeId',
        badNodes: LIN.nodes(),
        badEdges: [fe('t574-x-e1', 't574-l-trigger', 't574-no-such-node')],
        fixedNodes: LIN.nodes(),
        fixedEdges: [fe('t574-x-e1', 't574-l-trigger', 't574-l-archive')],
      },
      {
        why: '边的起点不存在',
        rule: 'flow_t274.go:157-159 sourceNodeId',
        badNodes: LIN.nodes(),
        badEdges: [fe('t574-x-e2', 't574-no-such-node', 't574-l-process')],
        fixedNodes: LIN.nodes(),
        fixedEdges: [fe('t574-x-e2', 't574-l-trigger', 't574-l-process')],
      },
      {
        why: '边自环',
        rule: 'flow_t274.go:163-165 self-loop',
        badNodes: LIN.nodes(),
        badEdges: [fe('t574-x-e3', 't574-l-process', 't574-l-process')],
        fixedNodes: LIN.nodes(),
        fixedEdges: [fe('t574-x-e3', 't574-l-process', 't574-l-archive')],
      },
      {
        why: '节点 id 重复',
        rule: 'flow_t274.go:135-137 duplicate node id',
        badNodes: [LIN.nodes()[0], LIN.nodes()[0]],
        badEdges: [],
        fixedNodes: [LIN.nodes()[0]],
        fixedEdges: [],
      },
    ]

    let total = (await readTemplates()).total
    const accepted: string[] = []
    const messages: string[] = []
    for (const shape of shapes) {
      const badName = uniqueName(`${FLOW_TPL_PREFIX}坏形`)
      const bad = await callApi(page, 'POST', '/api/v1/admin/flow/templates', {
        token,
        body: { name: badName, nodes: shape.badNodes, edges: shape.badEdges },
      })
      // 承重判据 = HTTP + 业务码 + trace 两枚（T464 双通道下 message 只有中文短句，见文件头）
      expect(bad.status, `${shape.why}（${shape.rule}）：应被拒为 HTTP 400`).toBe(400)
      expect(bad.code, `${shape.why}：业务码应是 10400 参数非法（0 就意味着静默落库）`).toBe(10400)
      expect(bad.trace?.errorCode ?? null, `${shape.why}：trace.errorCode 应与 code 同值`).toBe(10400)
      expect(bad.trace?.requestId ?? '', `${shape.why}：trace.requestId 应在场（服务端日志的反查坐标）`).not.toBe('')
      messages.push(bad.message)
      console.log(
        `[t574-chain-d][A2] ${shape.why} ⇒ HTTP=${bad.status} code=${bad.code} trace.errorCode=${String(bad.trace?.errorCode)} message=${bad.message}`,
      )

      // 零落地反证（逐形，不等四条跑完再数）：那颗名不在架上，且 total 没动
      const after = await readTemplates()
      expect(after.list.filter((t) => t.name === badName).length, `${shape.why}：被拒的这颗名不许落库`).toBe(0)
      expect(after.total, `${shape.why}：被拒后 total 不该变（前 ${total} 后 ${after.total}）`).toBe(total)

      // 配对正对照：其余逐字相同、只把被拒那一处改对，必须收
      const fixed = await callOk<TemplateDTO>(page, 'POST', '/api/v1/admin/flow/templates', {
        token,
        body: { name: uniqueName(`${FLOW_TPL_PREFIX}配对`), nodes: shape.fixedNodes, edges: shape.fixedEdges },
        why: `${shape.why} 的配对正对照：只改那一处应收（${shape.rule}）`,
      })
      madeTemplates.push(fixed.templateId)
      accepted.push(fixed.templateId)
      total = (await readTemplates()).total
      console.log(`[t574-chain-d][A2] ${shape.why} 配对正对照 ⇒ templateId=${fixed.templateId} 收单，total=${total}`)
    }

    expect(accepted.length, `四形各配 1 颗正对照收单（实得 ${accepted.length}）`).toBe(shapes.length)
    // 现象登记（不断言，只把读数打全）：现契约的错误响应面 message 只有中文用户面短句，
    // 四形是否可区分由 distinct 读数自己说话；后端若改成逐形可定位，这一行的读数会跟着变。
    console.log(
      `[t574-chain-d][A2-现象] 四形 message 逐形颗数=${messages.length} distinct=${new Set(messages).size} 值=${JSON.stringify(Array.from(new Set(messages)))}`,
    )
    expect(
      (await readTemplates()).list.filter((t) => t.name.startsWith(`${FLOW_TPL_PREFIX}坏形`)).length,
      '坏形一颗都不许落库',
    ).toBe(0)
  })

  test('24.3 A3 起实例 → running → 逐节点 confirm → completed ＋ 重复启动回 409', async ({ page }) => {
    const token = await ensureAdmin(page)
    expect(linTemplateId, '依赖 24.1 建的线性模板（串行模式下 24.1 红则本条不跑）').toBeTruthy()
    const alertId = await takeFreeAlert(page, token)
    linAlertId = alertId
    const inst = await callOk<InstanceDTO>(page, 'POST', '/api/v1/admin/flow/instances', {
      token,
      body: { templateId: linTemplateId, alertId },
      why: 'A3 起实例应 200',
    })
    instancesStarted += 1
    expect(inst.status, '新实例应为 running').toBe('running')
    expect(inst.alertId, '实例回指的告警号应等于入参').toBe(alertId)
    expect(inst.currentNodeId, '入口节点（入边为 0）应是 trigger').toBe(LIN.n1)
    expect(inst.endedAt, 'running 实例不该有 endedAt').toBeNull()
    console.log(`[t574-chain-d][A3] instanceId=${inst.instanceId} alertId=${alertId} status=${inst.status} current=${inst.currentNodeId}`)

    const statesAt = async (): Promise<Record<string, string>> => {
      const s = await callOk<{ list: NodeStateDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances/${inst.instanceId}/nodes`, {
        token,
        why: '节点状态读不通',
      })
      return Object.fromEntries(s.list.map((x) => [x.nodeId, x.status]))
    }
    const statusOf = async (): Promise<{ status: string; currentNodeId: string | null }> => {
      const l = await callOk<{ list: InstanceDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances?alertId=${alertId}`, {
        token,
        why: '实例回读不通',
      })
      return { status: l.list[0].status, currentNodeId: l.list[0].currentNodeId }
    }

    expect(await statesAt(), '启动瞬间：入口 current，其余 todo').toEqual({ [LIN.n1]: 'current', [LIN.n2]: 'todo', [LIN.n3]: 'todo' })

    // 非当前节点不许被 confirm（状态机的第二道锁）
    const notCurrent = await callApi(page, 'POST', `/api/v1/admin/flow/instances/${inst.instanceId}/nodes/${LIN.n3}/actions`, {
      token,
      body: { action: 'confirm' },
    })
    expect(notCurrent.status, `confirm 非当前节点应被拒（HTTP=${notCurrent.status} message=${notCurrent.message}）`).toBe(409)
    expect(notCurrent.code, '非当前节点的状态冲突业务码应是 10409').toBe(10409)
    console.log(`[t574-chain-d][A3] 非当前节点 confirm ⇒ HTTP=${notCurrent.status} code=${notCurrent.code} message=${notCurrent.message}`)

    for (const nodeId of [LIN.n1, LIN.n2, LIN.n3]) {
      const act = await callOk<ActionDTO>(page, 'POST', `/api/v1/admin/flow/instances/${inst.instanceId}/nodes/${nodeId}/actions`, {
        token,
        body: { action: 'confirm', remark: `T574链D推进-${nodeId}` },
        why: `A3 confirm ${nodeId} 应 200`,
      })
      expect(act.nodeId, `动作回读应落在 ${nodeId}`).toBe(nodeId)
      expect(act.actionLabel, '标签由后端给（前端不硬编码映射）').toBe('确认处理')
      const s = await statusOf()
      console.log(`[t574-chain-d][A3] confirm ${nodeId} ⇒ 实例 status=${s.status} current=${s.currentNodeId} 节点态=${JSON.stringify(await statesAt())}`)
      if (nodeId === LIN.n1) expect(s.currentNodeId, 'trigger 之后 current 应落在 process').toBe(LIN.n2)
      if (nodeId === LIN.n2) expect(s.currentNodeId, 'process 之后 current 应落在 archive').toBe(LIN.n3)
    }
    const fin = await statusOf()
    expect(fin.status, '最后一颗无后继 ⇒ 实例应 completed').toBe('completed')
    expect(await statesAt(), '三颗节点都应 done').toEqual({ [LIN.n1]: 'done', [LIN.n2]: 'done', [LIN.n3]: 'done' })

    // 幂等入口：同一条告警再起一次必须被拒，且不许起出第二颗实例。
    // 现契约的 fail() 出口 data 恒为 null（handler.go:367-372），"已有实例号"不在响应体里
    // （flow_t274.go:597 与 repo/flow.go:42 的注释口径与实测不同一面）⇒ 用 GET 反查做补偿读数，
    // 并把响应体那一格原样打进 stdout 作为现象（只交现象，不判根因）。
    const dup = await callApi(page, 'POST', '/api/v1/admin/flow/instances', {
      token,
      body: { templateId: linTemplateId, alertId },
    })
    expect(dup.status, '重复启动应 409').toBe(409)
    expect(dup.code, '重复启动的业务码应是 10409 状态冲突').toBe(10409)
    expect(dup.trace?.requestId ?? '', '重复启动的 trace.requestId 应在场').not.toBe('')
    console.log(
      `[t574-chain-d][A3-现象] 重复启动 ⇒ HTTP=${dup.status} code=${dup.code} data=${JSON.stringify(dup.data)} message=${dup.message}（响应体是否带已有实例号，以本行读数为准）`,
    )
    const dupBack = await callOk<{ list: InstanceDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances?alertId=${alertId}`, {
      token,
      why: '重复启动后的反查读不通',
    })
    expect(dupBack.list.length, '重复启动不许起出第二颗实例（按告警反查应恰好 1 颗）').toBe(1)
    expect(dupBack.list[0].instanceId, '反查到的实例号应等于先前起的那颗').toBe(inst.instanceId)

    // 时间线：三笔 confirm 都该在流水里
    const tl = await callOk<{ list: ActionDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances/${inst.instanceId}/actions`, {
      token,
      why: '处理时间线读不通',
    })
    expect(tl.list.length, '时间线应有 3 笔（三颗节点各一笔 confirm）').toBe(3)
    expect(tl.list.map((a) => a.nodeId), '时间线节点序列应等于推进序列').toEqual([LIN.n1, LIN.n2, LIN.n3])
  })

  test('24.4 A4 条件分流双输入对比 ＋ 越界收窄反证 ＋ reject 不推进 ＋ transfer 入参锁', async ({ page }) => {
    const token = await ensureAdmin(page)
    const condName = uniqueName(`${FLOW_TPL_PREFIX}分叉`)
    const cond = await callOk<TemplateDTO>(page, 'POST', '/api/v1/admin/flow/templates', {
      token,
      body: { name: condName, nodes: BR.nodes(), edges: BR.edges() },
      why: 'A4 建分叉模板应 200',
    })
    madeTemplates.push(cond.templateId)

    const COND = 't574-b-cond'
    const YES = 't574-b-yes'
    const NO = 't574-b-no'
    /** nodeId → 该节点状态行（assignee 也要读，转派那一格靠它回读） */
    const nodesAt = async (instanceId: string): Promise<Record<string, NodeStateDTO>> => {
      const s = await callOk<{ list: NodeStateDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances/${instanceId}/nodes`, {
        token,
        why: 'A4 节点态读不通',
      })
      return Object.fromEntries(s.list.map((x) => [x.nodeId, x]))
    }
    const statusOf = (states: Record<string, NodeStateDTO>): Record<string, string> =>
      Object.fromEntries(Object.entries(states).map(([k, v]) => [k, v.status]))
    const instOf = async (alertId: string): Promise<InstanceDTO> => {
      const l = await callOk<{ list: InstanceDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances?alertId=${alertId}`, {
        token,
        why: 'A4 实例回读不通',
      })
      expect(l.list.length, `告警 ${alertId} 应恰好一颗实例（一条告警一个实例）`).toBe(1)
      return l.list[0]
    }
    const actionCount = async (instanceId: string): Promise<number> => {
      const tl = await callOk<{ list: ActionDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances/${instanceId}/actions`, {
        token,
        why: 'A4 时间线读不通',
      })
      return tl.list.length
    }
    /** 起一颗实例并推进到判断节点（三组各用一颗新告警，互不干扰） */
    const startToCond = async (): Promise<{ inst: InstanceDTO; alertId: string }> => {
      const alertId = await takeFreeAlert(page, token)
      const started = await callOk<InstanceDTO>(page, 'POST', '/api/v1/admin/flow/instances', {
        token,
        body: { templateId: cond.templateId, alertId },
        why: 'A4 起分叉实例应 200',
      })
      instancesStarted += 1
      await callOk(page, 'POST', `/api/v1/admin/flow/instances/${started.instanceId}/nodes/t574-b-trigger/actions`, {
        token,
        body: { action: 'confirm' },
        why: 'A4 trigger 推进应 200',
      })
      const back = await instOf(alertId)
      expect(back.currentNodeId, `推进到判断节点后 current 应落在 ${COND}（入读不对，后面的分支读数就没有前提）`).toBe(COND)
      return { inst: back, alertId }
    }

    // 两组不同条件输入（D3：必须是对比读数，不是只跑一组）
    const g1 = await startToCond()
    await callOk(page, 'POST', `/api/v1/admin/flow/instances/${g1.inst.instanceId}/nodes/${COND}/actions`, {
      token,
      body: { action: 'confirm', nextNodeIds: [YES] },
      why: `A4 组一选 ${YES} 应 200`,
    })
    const g2 = await startToCond()
    await callOk(page, 'POST', `/api/v1/admin/flow/instances/${g2.inst.instanceId}/nodes/${COND}/actions`, {
      token,
      body: { action: 'confirm', nextNodeIds: [NO] },
      why: `A4 组二选 ${NO} 应 200`,
    })
    const g1Back = await instOf(g1.alertId)
    const g2Back = await instOf(g2.alertId)
    const g1States = statusOf(await nodesAt(g1.inst.instanceId))
    const g2States = statusOf(await nodesAt(g2.inst.instanceId))
    expect(g1Back.currentNodeId, `组一（选 ${YES}）应停在「通过分支」`).toBe(YES)
    expect(g2Back.currentNodeId, `组二（选 ${NO}）应停在「不通过分支」`).toBe(NO)
    expect(g1Back.currentNodeId, '两组输入的读数必须不同（同值就说明 nextNodeIds 收窄没生效）').not.toBe(g2Back.currentNodeId)
    expect(g1States[NO], '组一没走的那一支该留 todo').toBe('todo')
    expect(g2States[YES], '组二没走的那一支该留 todo').toBe('todo')
    console.log(
      `[t574-chain-d][A4] 双输入对比 组一 alert=${g1.alertId} current=${g1Back.currentNodeId} 态=${JSON.stringify(g1States)}｜组二 alert=${g2.alertId} current=${g2Back.currentNodeId} 态=${JSON.stringify(g2States)}`,
    )

    // 组三：越界收窄 —— nextNodeIds 指到「不是该节点出边」的号上必须被拒（flow_t274.go:832-840）
    const g3 = await startToCond()
    const g3ActionsBefore = await actionCount(g3.inst.instanceId)
    const stray = await callApi(page, 'POST', `/api/v1/admin/flow/instances/${g3.inst.instanceId}/nodes/${COND}/actions`, {
      token,
      body: { action: 'confirm', nextNodeIds: [LIN.n3] },
    })
    expect(stray.status, '越界 nextNodeIds 应被拒为 HTTP 400').toBe(400)
    expect(stray.code, '越界收窄的业务码应是 10400（回 0 就是按非法后继推进了流程）').toBe(10400)
    expect(stray.trace?.requestId ?? '', '越界收窄的 trace.requestId 应在场').not.toBe('')
    console.log(
      `[t574-chain-d][A4] 越界 nextNodeIds ⇒ HTTP=${stray.status} code=${stray.code} trace.errorCode=${String(stray.trace?.errorCode)} message=${stray.message}`,
    )
    expect((await instOf(g3.alertId)).currentNodeId, '被拒的越界收窄不许推进流程').toBe(COND)
    expect(await actionCount(g3.inst.instanceId), '被拒的越界收窄不许留下动作行').toBe(g3ActionsBefore)
    // 配对正对照：同一颗判断节点只把 nextNodeIds 换成真实出边就收 ⇒ 拒的确实是「不是出边」这一条
    await callOk(page, 'POST', `/api/v1/admin/flow/instances/${g3.inst.instanceId}/nodes/${COND}/actions`, {
      token,
      body: { action: 'confirm', nextNodeIds: [YES] },
      why: 'A4 组三配对正对照：换成真实出边应收',
    })
    expect((await instOf(g3.alertId)).currentNodeId, '组三配对正对照后应停在通过分支').toBe(YES)

    // reject：置 skipped 且不推进、不动实例指针（组二当前停在 NO）
    const beforeReject = await instOf(g2.alertId)
    const rej = await callOk<ActionDTO>(page, 'POST', `/api/v1/admin/flow/instances/${g2.inst.instanceId}/nodes/${NO}/actions`, {
      token,
      body: { action: 'reject', remark: 'T574链D驳回读数' },
      why: 'reject 应 200',
    })
    expect(rej.actionLabel, '驳回标签由后端给').toBe('驳回')
    const afterReject = await instOf(g2.alertId)
    expect(afterReject.currentNodeId, 'reject 不该动实例指针').toBe(beforeReject.currentNodeId)
    expect(afterReject.status, 'reject 不该把实例推到终态').toBe('running')
    expect((await nodesAt(g2.inst.instanceId))[NO]?.status, '被驳回的节点应置 skipped').toBe('skipped')

    // transfer 入参锁：缺 targetOperator 必须被拒（四枚动作各自的入参锁），给足就收并写回节点行
    const tr = await callApi(page, 'POST', `/api/v1/admin/flow/instances/${g2.inst.instanceId}/nodes/${NO}/actions`, {
      token,
      body: { action: 'transfer' },
    })
    expect(tr.status, 'transfer 缺 targetOperator 应被拒为 HTTP 400').toBe(400)
    expect(tr.code, 'transfer 缺入参的业务码应是 10400').toBe(10400)
    expect(tr.trace?.requestId ?? '', 'transfer 缺入参的 trace.requestId 应在场').not.toBe('')
    console.log(`[t574-chain-d][A4] transfer 缺入参 ⇒ HTTP=${tr.status} code=${tr.code} message=${tr.message}`)
    const trOk = await callOk<ActionDTO>(page, 'POST', `/api/v1/admin/flow/instances/${g2.inst.instanceId}/nodes/${NO}/actions`, {
      token,
      body: { action: 'transfer', targetOperator: TECH_ACCOUNT },
      why: 'A4 transfer 配对正对照：给足入参应收',
    })
    expect(trOk.actionLabel, '转派标签由后端给').toBe('转派')
    const g2AfterTransfer = await nodesAt(g2.inst.instanceId)
    expect(g2AfterTransfer[NO]?.assignee, '转派后被转派人应回读得到').toBe(TECH_ACCOUNT)
    console.log(
      `[t574-chain-d][A4] 转派配对正对照 ⇒ 节点 ${NO} assignee=${String(g2AfterTransfer[NO]?.assignee)} status=${String(g2AfterTransfer[NO]?.status)}（转派只改指派，不改节点状态）`,
    )
  })

  test('24.5 A5 告警 ↔ 实例 ↔ 模板三点对平（值级，并证明按告警过滤真的生效）', async ({ page }) => {
    const token = await ensureAdmin(page)
    expect(linAlertId, '依赖 24.3 起过实例的那颗告警（串行模式下 24.3 红则本条不跑）').toBeTruthy()
    // 正向：从告警查实例
    const byAlert = await callOk<{ list: InstanceDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances?alertId=${linAlertId}`, {
      token,
      why: 'A5 从告警侧查实例应 200',
    })
    expect(byAlert.list.length, '一条告警只该有一颗实例').toBe(1)
    const inst = byAlert.list[0]
    // 反向：实例回指告警 + 模板
    expect(inst.alertId, '实例应回指同一个告警号').toBe(linAlertId)
    expect(inst.templateId, '实例应回指 A1 那颗模板').toBe(linTemplateId)
    const tpl = await callOk<TemplateDTO>(page, 'GET', `/api/v1/admin/flow/templates/${inst.templateId}`, {
      token,
      why: 'A5 从实例反查模板应 200',
    })
    expect(tpl.name, '模板名应等于 24.1 写入的那个唯一名').toBe(linTemplateName)
    expect(inst.templateName, '实例带的模板名应与模板详情同值').toBe(tpl.name)

    // 过滤对照：另一颗告警读回来的必须是另一颗实例 —— 缺这条就分不清「过滤生效」与「回全量」
    const others = Array.from(usedAlerts).filter((id) => id !== linAlertId)
    expect(others.length, '需要至少一颗别的告警作过滤对照（24.4 应已起过实例）').toBeGreaterThan(0)
    const otherInst = await callOk<{ list: InstanceDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances?alertId=${others[0]}`, {
      token,
      why: 'A5 过滤对照的实例读不通',
    })
    expect(otherInst.list.length, `对照告警 ${others[0]} 也应恰好一颗实例`).toBe(1)
    expect(otherInst.list[0].instanceId, '按告警过滤必须返回不同实例（回全量就会在这一格露馅）').not.toBe(inst.instanceId)

    // 缺 alertId 的读法必须被拒（flow_t274.go:607-612：alertId 是必填正整数）
    const noQuery = await callApi(page, 'GET', '/api/v1/admin/flow/instances', { token })
    expect(noQuery.status, '实例列表缺 alertId 应被拒为 HTTP 400').toBe(400)
    expect(noQuery.code, '缺 alertId 的业务码应是 10400').toBe(10400)
    console.log(
      `[t574-chain-d][A5] 告警 ${linAlertId} ↔ 实例 ${inst.instanceId} ↔ 模板 ${tpl.templateId} 三点对平，status=${inst.status}｜对照告警 ${others[0]} 的实例=${otherInst.list[0].instanceId}（不同颗）`,
    )
  })

  test('24.6 A6 权限正反两面（读放开给 staff、写锁在 admin；正面已由 24.1-24.5 走通）', async ({ page, browser }) => {
    const token = await ensureAdmin(page)
    expect(linTemplateId, '依赖 24.1 那颗模板作越权靶子（串行模式下 24.1 红则本条不跑）').toBeTruthy()
    // 反面一：医护读模板 / 读实例 = 放行（T359 放宽的那一面，与 03-alerts.spec.ts:450 同口径）
    const ctxDoc = await browser.newContext()
    const pDoc = await ctxDoc.newPage()
    await realLogin(pDoc, DOCTOR_ACCOUNT)
    const docTok = (await getAuthToken(pDoc)) ?? ''
    expect(docTok, `${DOCTOR_ACCOUNT} 会话应建得起来`).toBeTruthy()
    const docRead = await callApi(pDoc, 'GET', '/api/v1/admin/flow/templates?pageSize=1', { token: docTok })
    expect(docRead.code, `医护读模板应放行（HTTP=${docRead.status} message=${docRead.message}）`).toBe(0)
    const docReadInst = await callApi(pDoc, 'GET', `/api/v1/admin/flow/instances?alertId=${linAlertId}`, { token: docTok })
    expect(docReadInst.code, `医护读实例应放行（HTTP=${docReadInst.status} message=${docReadInst.message}）`).toBe(0)
    // 反面二：医护写模板必须 403（gateway 的 adminOnly 那一格；图给合法图，确保拒的是权限而不是入参）
    const docWrite = await callApi(pDoc, 'POST', '/api/v1/admin/flow/templates', {
      token: docTok,
      body: { name: uniqueName(`${FLOW_TPL_PREFIX}医护越权`), nodes: LIN.nodes(), edges: LIN.edges() },
    })
    expect(docWrite.status, `医护写模板应 403，实得 HTTP=${docWrite.status} code=${String(docWrite.code)} message=${docWrite.message}`).toBe(403)
    expect(docWrite.code, '医护越权的业务码不许是 0（回 0 就是真写进去了）；具体码值由哪一层给的不作定（只读不判）').not.toBe(0)
    console.log(
      `[t574-chain-d][A6] 医护读模板=${docRead.status}/code=${docRead.code} 读实例=${docReadInst.code} 写=${docWrite.status}/code=${String(docWrite.code)} message=${docWrite.message}`,
    )
    // 反面三：医护删 admin 建的模板也要 403，且删完那颗还在（零副作用反证）
    const docDel = await callApi(pDoc, 'DELETE', `/api/v1/admin/flow/templates/${linTemplateId}`, { token: docTok })
    expect(docDel.status, `医护删模板应 403，实得 HTTP=${docDel.status} code=${String(docDel.code)}`).toBe(403)
    const stillThere = await callApi(pDoc, 'GET', `/api/v1/admin/flow/templates/${linTemplateId}`, { token })
    expect(stillThere.code, '越权删除后那颗模板应仍在架（403 不许有副作用）').toBe(0)
    // 反面四：无令牌写 = 401（与 403 分开，否则分不清「没登录」与「没权限」）
    const anon = await callApi(pDoc, 'POST', '/api/v1/admin/flow/templates', {
      body: { name: uniqueName(`${FLOW_TPL_PREFIX}无令牌`), nodes: LIN.nodes(), edges: LIN.edges() },
    })
    expect(anon.status, `无令牌写应 401，实得 HTTP=${anon.status} code=${String(anon.code)} message=${anon.message}`).toBe(401)
    // 反面五：技师角色同样写不进（第二个非 admin 角色，防「只对 doctor 单点放行」）
    const ctxTech = await browser.newContext()
    const pTech = await ctxTech.newPage()
    await realLogin(pTech, TECH_ACCOUNT)
    const techTok = (await getAuthToken(pTech)) ?? ''
    expect(techTok, `${TECH_ACCOUNT} 会话应建得起来`).toBeTruthy()
    const techWrite = await callApi(pTech, 'POST', '/api/v1/admin/flow/templates', {
      token: techTok,
      body: { name: uniqueName(`${FLOW_TPL_PREFIX}技师越权`), nodes: LIN.nodes(), edges: LIN.edges() },
    })
    expect(techWrite.status, `技师写模板应 403，实得 HTTP=${techWrite.status} code=${String(techWrite.code)} message=${techWrite.message}`).toBe(403)
    // 三发越权都不许落库（否则「拒了但也写了」）；名册现读，不靠响应推断
    const afterForbidden = await callOk<{ list: TemplateDTO[] }>(pTech, 'GET', '/api/v1/admin/flow/templates?pageSize=100', {
      token,
      why: '越权三发后的名册读不通',
    })
    const forbiddenLanded = afterForbidden.list.filter((t) => /医护越权|技师越权|无令牌/.test(t.name)).map((t) => t.name)
    expect(forbiddenLanded, `越权三发都不许落库，实得：${forbiddenLanded.join(' ; ') || '无'}`).toEqual([])
    console.log(`[t574-chain-d][A6] 技师写=${techWrite.status}/code=${String(techWrite.code)} 无令牌写=${anon.status}/code=${String(anon.code)}｜越权三发落库颗数=${forbiddenLanded.length}（名册现读）`)
    await ctxDoc.close()
    await ctxTech.close()
  })

  test('24.7 残留与守恒：实例无删除端点钉在证据面，模板颗数与实例颗数互相对平', async ({ page }) => {
    const token = await ensureAdmin(page)
    expect(linAlertId, '依赖 24.3 的实例作「发 DELETE」靶子（串行模式下 24.3 红则本条不跑）').toBeTruthy()
    // 实例侧：路由表没有 DELETE /admin/flow/instances/:id（flow_t274.go:7-9 接口清单里 DELETE 只有 templates）⇒ 对已知实例发 DELETE 应 404
    const known = await callOk<{ list: InstanceDTO[] }>(page, 'GET', `/api/v1/admin/flow/instances?alertId=${linAlertId}`, {
      token,
      why: '守恒腿读实例不通',
    })
    const instId = known.list[0].instanceId
    const delInst = await callApi(page, 'DELETE', `/api/v1/admin/flow/instances/${instId}`, { token })
    expect(
      delInst.status,
      `实例删除端点若变成 ${delInst.status} 说明契约已加删除口，本文件的「实例残留」那条要改口径（现读 code=${String(delInst.code)} message=${delInst.message}）`,
    ).toBe(404)
    console.log(
      `[t574-chain-d][残留] 实例 ${instId} 发 DELETE ⇒ HTTP=${delInst.status} code=${String(delInst.code)}（无删除端点，实例行删不掉，如实登记）`,
    )

    const rest = await callOk<{ list: TemplateDTO[]; total: number }>(page, 'GET', '/api/v1/admin/flow/templates?pageSize=100', {
      token,
      why: '守恒腿读模板不通',
    })
    const mine = rest.list.filter((t) => t.name.startsWith(FLOW_TPL_PREFIX))
    expect(rest.list.length, `名册尺要成立，pageSize=100 必须装得下全量（实回 ${rest.list.length}/${rest.total}）`).toBe(rest.total)
    expect(
      mine.length,
      `名册里本前缀颗数应等于「往轮在用残留 ${preexistingTplIds.length} + 本轮登记 ${madeTemplates.length}」（实得 ${mine.length}）`,
    ).toBe(preexistingTplIds.length + madeTemplates.length)

    // instanceCount 的对平走详情面（列表面那一列是 SQL 字面量 0，见 detailOf 注释与文件头）
    const detailCounts: string[] = []
    let sumDetail = 0
    for (const id of madeTemplates) {
      const d = await detailOf(page, token, id)
      sumDetail += d.instanceCount
      detailCounts.push(`${id}:${d.instanceCount}`)
    }
    const sumListFace = mine.reduce((acc, t) => acc + t.instanceCount, 0)
    console.log(
      `[t574-chain-d][残留] 模板：本前缀在册颗数=${mine.length}（往轮残留 ${preexistingTplIds.length} + 本轮 ${madeTemplates.length}，afterAll 逐颗删并定性）｜起成功的实例颗数=${instancesStarted}｜详情面 instanceCount 合计=${sumDetail} 逐颗=${detailCounts.join(' ; ')}`,
    )
    console.log(
      `[t574-chain-d][残留-现象] 列表面 instanceCount 合计=${sumListFace}（现契约该列写死 0，repo/flow.go:178-180）｜详情面合计=${sumDetail}｜total=${rest.total} 基线=${baselineTplTotal}`,
    )
    expect(sumDetail, `详情面 instanceCount 合计应等于本文件起成功的实例颗数（${sumDetail} vs ${instancesStarted}）`).toBe(instancesStarted)
  })

  test.afterAll(async ({ browser }) => {
    const page = await browser.newPage()
    try {
      await realLogin(page)
      const token = (await getAuthToken(page)) ?? ''
      expect(token, 'afterAll 管理端会话建不起来 ⇒ 清场没法做').toBeTruthy()
      let deleted = 0
      const refusedInUse: string[] = []
      const unexpected: string[] = []
      for (const id of madeTemplates) {
        const r = await callApi(page, 'DELETE', `/api/v1/admin/flow/templates/${id}`, { token })
        if (r.code === 0) {
          deleted += 1
          console.log(`[t574-chain-d][还原] 模板 ${id} 已删`)
        } else if (r.status === 409 && r.code === 10409) {
          refusedInUse.push(id)
          console.log(
            `[t574-chain-d][残留报备] 模板 ${id} 在用不可删 ⇒ HTTP=${r.status} code=${r.code} message=${r.message}（先数引用再删，repo/flow.go:284-291）`,
          )
        } else {
          unexpected.push(`${id}: HTTP=${r.status} code=${String(r.code)} message=${r.message}`)
        }
      }
      expect(
        unexpected,
        `删除结果只该有两种（删成 / 在用 409），出现第三种就是漏删或端点变化：${unexpected.join(' ; ') || '无'}`,
      ).toEqual([])
      const after = await callOk<{ list: TemplateDTO[]; total: number }>(page, 'GET', '/api/v1/admin/flow/templates?pageSize=100', {
        token,
        why: '还原后的模板列表读不通',
      })
      const leftovers = after.list.filter((t) => t.name.startsWith(FLOW_TPL_PREFIX))
      expect(after.list.length, `名册尺要成立，pageSize=100 必须装得下全量（实回 ${after.list.length}/${after.total}）`).toBe(after.total)
      const leftoverNames = leftovers.map((t) => t.templateId)
      console.log(
        `[t574-chain-d][守恒] 模板 total=${after.total} 基线=${baselineTplTotal} 删成=${deleted} 在用残留=${leftovers.length}（其中往轮 ${preexistingTplIds.length} + 本轮 ${refusedInUse.length}）｜实例残留颗数=${instancesStarted}（契约无删除端点，只能推到终态不能删行）`,
      )
      console.log(`[t574-chain-d][守恒] 残留名册=${leftoverNames.join(' ; ') || '无'}`)
      if (madeTemplates.length === 0) {
        // 本轮一笔写都没做成（前置红或被跳过）⇒ 没有守恒可验，只登记名册读数，不把往轮残留判成本轮的错
        console.log(`[t574-chain-d][守恒] 本轮登记颗数=0 ⇒ 跳过守恒断言（只留上面两行读数）`)
        return
      }
      // 残留集合按号对平，不靠颗数：往轮在用残留 + 本轮「在用不可删」= 收尾在册的每一颗
      const sorted = (ids: string[]) => [...ids].sort()
      expect(
        sorted(leftoverNames),
        `收尾在册的本前缀号集应等于「往轮残留 ${preexistingTplIds.length} 颗 + 本轮在用 ${refusedInUse.length} 颗」，本轮登记 ${madeTemplates.length} 颗、删成 ${deleted} 颗`,
      ).toEqual(sorted([...preexistingTplIds, ...refusedInUse]))
      // 残留的每一颗都要真的带实例引用（详情面读数）：漏删与「在用不可删」必须能区分
      const unsupported: string[] = []
      for (const t of leftovers) {
        const d = await detailOf(page, token, t.templateId)
        if (d.instanceCount < 1) unsupported.push(`${d.name}(${d.templateId}) instanceCount=${d.instanceCount}`)
      }
      expect(unsupported, `残留却不带实例引用的颗数=${unsupported.length}（这一类就是漏删）：${unsupported.join(' ; ') || '无'}`).toEqual([])
      if (baselineTplTotal !== null) {
        expect(
          after.total,
          `行数守恒：收尾 total=${after.total} 应等于基线 ${baselineTplTotal} + 本轮在用残留 ${refusedInUse.length}（往轮 ${preexistingTplIds.length} 颗已在基线里，不重复计）`,
        ).toBe(baselineTplTotal + refusedInUse.length)
      }
    } finally {
      await page.close()
    }
  })
})
