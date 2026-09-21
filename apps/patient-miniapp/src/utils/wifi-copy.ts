/**
 * 患者端配网 — 设计稿逐字文案表（T192）
 *
 * 唯一来源：docs/design/patient/wifi-setup/ 11 页 + PRD §7A.9 / §7A.9.1。
 * 本文件只做"设计稿文案集中存放"，便于逐页对照验收；逻辑判断在 utils/wifi-state.ts。
 * 改这里必须同步改设计稿，不要在页面里散落字符串。
 */

/** 01-entry：入口卡片与前置检查 */
export const ENTRY = {
  navTitle: '配置家庭 WiFi',
  currentDeviceLabel: '当前设备',
  deviceName: '矫形支具监测器',
  deviceNoLabel: '设备编号',
  startBtn: '开始配置家庭 WiFi',
  prepTitle: '配置前准备',
  /** 三项前置：蓝牙 / 位置权限（安卓）/ 设备上电 */
  prepItems: [
    { key: 'bluetooth', name: '打开手机蓝牙', sub: '用于近距离发现您的监测器' },
    { key: 'location', name: '允许位置权限', sub: '安卓系统需要此权限来扫描附近设备' },
    { key: 'power', name: '设备已上电', sub: '监测器指示灯闪烁，表示处于配网状态' },
  ],
  statusDone: '已开启',
  statusTodo: '未开启',
  networkUnconnected: '未连接家庭网络',
  networkConnected: '已连接家庭网络',
  locDone: '已授权',
  locTodo: '未授权',
  /** 设备上电无法由前端探测（BLE 未建连前无信号），故不给"已就绪"假绿态 */
  powerHint: '请确认',
  /** T297（Boss 09-21 23:02 裁定 D5 选①）：只给能力提示，不教用户凭网络名判断频段（PRD §7A.9.1 ④ 红线） */
  hint: '提示：本设备仅支持 2.4GHz 家庭 WiFi，不支持 5G。请确认家中路由器已开启 2.4GHz 网络；不确定时请联系技师协助确认。',
  btModal: {
    title: '请打开手机蓝牙',
    body: '配置家庭 WiFi 需要通过蓝牙近距离连接您的监测器。请在手机系统设置中打开蓝牙，然后返回本页面。',
    primary: '去打开蓝牙',
    secondary: '稍后再说',
  },
  locModal: {
    title: '需要位置权限',
    body: '为了扫描并连接您的监测器，需要您授权位置权限。我们仅在配网时使用，不会记录您的位置信息。',
    primary: '允许位置权限',
    secondary: '暂不允许',
  },
  btReadyToast: '蓝牙已开启，开始搜索设备…',
  btSettingsToast: '请在手机系统设置中打开蓝牙，然后返回本页面',
  locReadyToast: '位置权限已授权，开始搜索设备…',
  noDeviceToast: '请先在设备页完成设备绑定',
} as const

/** 02-scan：扫描蓝牙设备（设计稿头注释：BLE 近场，无云端调用） */
export const SCAN = {
  navTitle: '搜索监测器',
  /** PRD §7A.9 约束：2.4GHz 提示必须前置到扫描页；T297 起措辞不引导用户凭网络名判断频段 */
  notice24G: '本设备仅支持 2.4GHz 家庭 WiFi，不支持 5G。请确认路由器已开启 2.4GHz 网络后再开始配置。',
  scanningText: '正在搜索附近的监测器…',
  scanningSub: '请确保设备已上电且靠近手机',
  emptyTitle: '暂时没找到您的监测器',
  emptyTips: [
    '确认设备已上电，指示灯处于闪烁状态',
    '将手机靠近设备（建议 1 米以内）',
    '确认选择了 2.4GHz WiFi（不支持 5G）',
    '仍不行？请联系您的技师协助',
  ],
  retryBtn: '重新搜索',
  connectingToast: '正在连接设备…',
} as const

/** 03-connect：建立连接 + 输入家庭 WiFi 凭据 */
export const CONNECT = {
  navTitle: '连接监测器',
  connectingText: '正在连接您的监测器…',
  connectingSub: '请保持手机靠近设备',
  tagConnecting: '连接中',
  tagConnected: '已连接',
  connectDoneBtn: '连接成功，下一步',
  ssidLabel: '家庭 WiFi 名称',
  ssidPlaceholder: '请输入 WiFi 名称',
  findLink: 'WiFi 名称怎么找？',
  pwdLabel: 'WiFi 密码',
  pwdPlaceholder: '请输入 WiFi 密码',
  /** 设计稿 tips + PRD §7A.9.1 ③-1 常驻口径（取并集，见交付偏差表 D-2） */
  tips: '仅支持 2.4GHz WiFi。密码区分大小写，请仔细输入。若家中路由器 2.4G 与 5G 同名，请先在路由器设置中改成不同名称后重试。',
  startBtn: '开始配网',
  findModal: {
    title: 'WiFi 名称怎么找？',
    steps: [
      '打开手机「设置」→「WLAN / WiFi」',
      '查看已连接的网络名称（通常在路由器背面标签上也能找到）',
      '请确认路由器已开启 2.4GHz 网络（本设备不支持 5G）',
      '将名称准确填入上方输入框',
    ],
    confirm: '我知道了',
  },
  ssidRequiredToast: '请输入 WiFi 名称',
  pwdRequiredToast: '请输入 WiFi 密码',
  sendingToast: '正在发送配置…',
  throttleToast: '操作过于频繁，请稍候再试',
  /** PRD §7A.9 约束：患者端不出现技术术语，接口错误只给生活化提示（原始错误进日志） */
  keyFailToast: '暂时无法开始配置，请稍后重试或联系技师',
  connectFailedToast: '连接失败，请靠近设备后重试',
  /** 设计稿未覆盖态（03 停留期间设备自行断开）：标签如实反映链路，避免写死"已连接"误导用户 */
  tagDisconnected: '已断开',
  /** 下发前原地重连的过程提示：PRD §7A.9 要求生活化、不出现技术术语 */
  reconnectingToast: '设备连接已断开，正在重新连接…',
} as const

/** 04-progress：写入进度（状态 0→1→2→3，环形进度 + 四步清单） */
export const PROGRESS = {
  navTitle: '正在配置',
  /** 下标即状态码 0/1/2/3：标题、副文案、百分比、步骤行文案 */
  states: [
    { title: '正在同步设置…', sub: '已发送 WiFi 信息，设备正在处理', pct: 10, step: '正在同步设置' },
    { title: '正在连接家庭 WiFi', sub: '设备尝试连接您的家庭网络', pct: 35, step: '正在连接家庭 WiFi' },
    { title: '网络连接成功', sub: '设备已获取网络地址', pct: 65, step: '网络连接成功' },
    { title: '正在连接服务器', sub: '设备正在与云端建立连接', pct: 90, step: '正在连接服务器' },
  ],
  idleTitle: '正在同步设置…',
  idleSub: '请保持手机靠近设备，不要关闭页面',
  deviceTag: '配置中',
  stepDoneLabel: '完成',
  stepActiveLabel: '进行中',
  stepPendingLabel: '等待中',
  nearlyDoneToast: '配置即将完成…',
  cancelBtn: '取消配置',
  cancelToast: '已取消配置',
} as const

/** 05-success：配网完成（状态 9） */
export const SUCCESS = {
  navTitle: '配网成功',
  title: '配网成功',
  sub: '您的监测器已连接家庭网络',
  infoLabel: '连接信息',
  deviceKey: '设备',
  networkKey: '家庭网络',
  statusKey: '连接状态',
  onlineTag: '已联网',
  benefitTitle: '现在可以：',
  benefits: [
    '实时查看孩子的支具佩戴压力数据',
    '接收佩戴时长与异常提醒',
    '与医生同步复查数据',
  ],
  primaryBtn: '查看佩戴数据',
  secondaryBtn: '返回设备管理',
  backToast: '正在返回监测页…',
  syncFailToast: '配网已完成，网络状态同步稍后自动重试',
} as const

/** 失败态落点：与 index.vue 视图名一一对应 */
export type FailureKey = 'pwd' | 'nonet' | 'addr' | 'srv' | 'timeout' | 'linklost'

export interface FailureCopy {
  /** 图标色：设计稿 red = 密码/服务器；amber = 无网络/地址/超时 */
  tone: 'red' | 'amber'
  title: string
  desc: string
  actions: string[]
  primaryLabel: string
  /** 'connect' = 回 03 重新输入；'retry' = 回 04 重试配网；'entry' = 回 01 重新配网 */
  primaryTo: 'connect' | 'retry' | 'entry'
  secondaryLabel?: string
  /** 07-contact 的 #type= 值 */
  contactHash: FailureKey
}

/** 06a–06e：五类失败态独立文案与处置（PRD §7A.9 错误态表） */
export const FAILURES: Record<FailureKey, FailureCopy> = {
  pwd: {
    tone: 'red',
    title: 'WiFi 密码不正确',
    desc: '设备无法连接到您的家庭网络，可能是 WiFi 密码输入有误。',
    actions: [
      '回到上一步，重新输入 WiFi 密码（注意区分大小写）',
      '确认选择的是 2.4GHz 网络',
      '重新尝试配网',
    ],
    primaryLabel: '重新输入密码',
    primaryTo: 'connect',
    secondaryLabel: '重试配网',
    contactHash: 'pwd',
  },
  nonet: {
    tone: 'amber',
    title: '找不到您的 WiFi 网络',
    // 第 1 条按 PRD §7A.9.1 ③-4 细化到"空格 / 大小写 / 5G"，不得只给泛泛提示
    actions: [
      '检查 WiFi 名称是否输入正确（区分大小写，名称前后不要有空格）',
      '确认选择的是 2.4GHz 网络，不是 5G',
      '将设备移近路由器后重试',
    ],
    desc: '设备搜索不到您填写的家庭网络，可能是网络名称不对或信号太弱。',
    primaryLabel: '换一个网络',
    primaryTo: 'connect',
    secondaryLabel: '重试配网',
    contactHash: 'nonet',
  },
  addr: {
    tone: 'amber',
    title: '网络连接异常',
    desc: '设备已连上 WiFi，但无法获取网络地址。通常重启路由器可以解决。',
    actions: [
      '拔掉路由器电源，等待 10 秒后重新插上',
      '等待路由器指示灯恢复正常',
      '重新尝试配网',
    ],
    primaryLabel: '重启后重试',
    primaryTo: 'retry',
    contactHash: 'addr',
  },
  srv: {
    tone: 'red',
    title: '暂时无法连接服务器',
    desc: '设备已连上 WiFi，但无法连接到我们的服务器。可能是服务器临时维护或网络受限。',
    actions: [
      '请稍后再试（服务器会自动重试）',
      '确认您的 WiFi 能正常上网',
      '如长时间不行，请联系技师',
    ],
    primaryLabel: '稍后重试',
    primaryTo: 'retry',
    contactHash: 'srv',
  },
  timeout: {
    tone: 'amber',
    title: '设备响应超时',
    desc: '长时间没有收到设备的反馈，可能是设备离手机太远或已断电。',
    actions: [
      '确认设备已上电（指示灯闪烁）',
      '将手机靠近设备（1 米以内）',
      '重新尝试配网',
    ],
    primaryLabel: '重新配网',
    primaryTo: 'entry',
    contactHash: 'timeout',
  },
  // T218-B(A20)：配网途中断链且重连不回时的失败态——与"设备响应超时"区分：
  // 设备侧配网不依赖 BLE、可能仍在继续甚至已配好，文案不得让用户误以为设备一定没配好
  linklost: {
    tone: 'amber',
    title: '连接中断，未能确认结果',
    desc: '配网过程中手机与设备的连接中断了。设备的配网可能在后台继续，它有可能已经配置成功。',
    actions: [
      '等待一分钟后，到设备页看看设备是否已经联网',
      '若设备未联网，请靠近设备后重新配网（重复配网不会损坏设备）',
      '多次中断时，请确认手机与设备之间没有墙体或电器遮挡',
    ],
    primaryLabel: '重新配网',
    primaryTo: 'entry',
    secondaryLabel: '重试配网',
    contactHash: 'linklost',
  },
}

/** 失败页共用：导航标题与「建议操作」区块标题 */
export const FAILURE_COMMON = {
  navTitle: '配网未成功',
  actionLabel: '建议操作',
  contactBtn: '联系技师',
} as const

/**
 * B512 状态码 → 失败页（PRD §7A.9 错误态表）。
 * 60s 无推送不走此表，由页面直接落 timeout（§7A.9 超时口径）。
 */
export const FAILURE_KEY_BY_CODE: Record<number, FailureKey> = {
  [-1]: 'pwd',
  [-2]: 'nonet',
  [-3]: 'addr',
  [-4]: 'srv',
}

/** 07-contact 的问题类型键：五类配网失败 + 扫描无设备（02 空态第 4 条指引） */
export type ContactIssue = FailureKey | 'nodevice'

/** 07-contact：联系客服（进入即提交反馈，见 PRD §7A.9 联系技师路由链） */
export const CONTACT = {
  navTitle: '联系客服',
  title: '正在为您联系客服',
  subtitle: '我们已记录本次配网遇到的问题，客服会尽快与您联系',
  contextTitle: '已附上的信息',
  typeLabel: '问题类型',
  deviceLabel: '设备',
  timeLabel: '发生时间',
  steps: [
    '问题信息已提交到客服系统，客服可在工作台查看',
    '点击下方按钮，打开与客服的对话窗口',
    '客服会在对话中协助您完成配网',
  ],
  primaryBtn: '打开客服对话',
  secondaryBtn: '稍后再说',
  /** 步骤 1 真实状态（设计稿静态稿固定画 ✓，实现须反映提交结果） */
  step1Pending: '正在提交问题信息…',
  step1Done: '问题信息已提交到客服系统，客服可在工作台查看',
  step1Failed: '问题信息提交失败，请在对话中直接说明情况',
  openingToast: '正在打开客服对话…',
  submitFailToast: '问题记录提交失败，请直接打开客服对话说明情况',
  /** 设计稿 TYPE_MAP：问题类型展示文案（与 06 页标题同源但措辞更短） */
  typeMap: {
    pwd: 'WiFi 密码不正确',
    nonet: '找不到 WiFi 网络',
    addr: '网络地址获取失败',
    srv: '服务器不可达',
    timeout: '设备响应超时',
    linklost: '连接中断，结果未确认',
    nodevice: '未搜索到设备',
  } as Record<ContactIssue, string>,
} as const

/** 全链路 BLE 断开提示（PRD §7A.9 错误态表最后一行） */
export const BLE_DISCONNECTED_TOAST = '设备连接已断开，请靠近设备后重试'

/** PRD §7A.9 患技差异表：患者端信号只说"良好/弱"，不出现 RSSI 数值 */
export const SIGNAL_GOOD = '信号良好'
export const SIGNAL_WEAK = '信号弱'
