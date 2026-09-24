// 契约对拍：Go DTO 的 json tag 与 packages/shared-types 的键名 / 可选性
//
// 为什么要有它（T356 / 与 T337 同族）：契约里声明了的键，后端列表 DTO 少写一个，
// 前端拿到的就是 undefined —— 而 Contract Drift Gate（vitest）比对的是 TS fixture 与
// TS 接口，两边都是手写，结构上抓不到「Go 侧真的没这个 tag」。这里把对拍拉到 Go 源码：
// 不启服务、不打网络，纯静态解析 json tag。
//
// 用法：
//   node scripts/contract/go-json-tag-audit.mjs            门禁模式（有问题 exit 1）
//   node scripts/contract/go-json-tag-audit.mjs --dump     诊断模式：打印两侧键集合，不参与判定
//
// 判红三类：
//   C1 未登记 —— 新增 *DTO 结构体没进 dto-contract-map.mjs（既没映射也没豁免），或映射指向的
//      结构体 / 接口已被改名删掉。防的是「加了资源忘了配门禁」。
//   C2 键名漂移 —— 契约声明为必填（无 ?）的键，后端 json tag 里没有。T337 的原始缺陷形状。
//      反向（后端有、契约没声明）同样判红：要么补契约（像 T337 那样登记），要么显式排除。
//   C3 可选性不一致 —— 契约声明必填但 Go tag 带 omitempty ⇒ 零值时键会从响应里消失，
//      前端按「一定有这个键」写就会拿到 undefined。

import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { CONTRACT_MAP } from './dto-contract-map.mjs'

const ROOT = path.resolve(import.meta.dirname, '../..')
const SHARED_TYPES = path.join(ROOT, 'packages/shared-types/src/index.ts')
const SERVICES = path.join(ROOT, 'services')
const DUMP = process.argv.includes('--dump')

// ---------- shared-types 侧 ----------

// 解析 export interface / export type X = A & { ... }
// 返回 name -> { fields: Map<键名, {optional:boolean}>, parents: string[], line: number }
function parseSharedTypes(src) {
  const decls = new Map()
  // [^{;]* —— 声明头到 { 之间不许出现分号：否则 `export type X = 'a' | 'b';` 这类联合类型
  // 会一路吃到下一条声明的 { ，把别人的字段挂到自己名下（实测把 InstallRecord 记成了 CalibStatus）
  const declRe = /export\s+(interface|type)\s+([A-Za-z_]\w*)\s*([^{;]*)\{/g
  let m
  while ((m = declRe.exec(src))) {
    const [full, kind, name, mid] = m
    const open = m.index + full.length - 1
    const close = matchBrace(src, open)
    if (close < 0) continue
    const line = src.slice(0, m.index).split('\n').length
    const body = src.slice(open + 1, close)
    const parents = []
    // interface X extends A, B  /  type X = A & B & { ... }（InstallRecordRow 就是交集类型）
    const head = kind === 'interface'
      ? (mid.trim().startsWith('extends') ? mid.replace(/^\s*extends\s*/, '') : '')
      : mid.replace(/^\s*=\s*/, '')
    for (const p of head.split(/&|,/)) {
      const t = p.trim().replace(/<.*>$/, '')
      if (/^[A-Za-z_]\w*$/.test(t) && !isBuiltin(t)) parents.push(t)
    }
    decls.set(name, { kind, fields: parseFields(body), parents, line })
    declRe.lastIndex = close
  }
  return decls
}

const BUILTIN = new Set(['string', 'number', 'boolean', 'unknown', 'any', 'null', 'Record', 'Array', 'Partial'])
function isBuiltin(t) {
  return BUILTIN.has(t) || /^(string|number|boolean|void|unknown|never)$/.test(t)
}

function matchBrace(src, open) {
  let depth = 0
  for (let i = open; i < src.length; i++) {
    const c = src[i]
    if (c === '{') depth++
    else if (c === '}') {
      depth--
      if (depth === 0) return i
    } else if (c === "'" || c === '"' || c === '`') {
      i = skipString(src, i, c)
    } else if (c === '/' && src[i + 1] === '/') {
      i = src.indexOf('\n', i) - 1
    } else if (c === '/' && src[i + 1] === '*') {
      i = src.indexOf('*/', i + 2) + 1
    }
  }
  return -1
}

function skipString(src, i, q) {
  for (let j = i + 1; j < src.length; j++) {
    if (src[j] === '\\') { j++; continue }
    if (src[j] === q) return j
  }
  return i
}

// 顶层字段：按分号/换行切，嵌套 {} 与 [] 内的不算
function parseFields(body) {
  const fields = new Map()
  const chunks = splitTopLevel(body)
  for (const raw of chunks) {
    const text = stripComments(raw).trim()
    if (!text) continue
    const fm = /^(?:readonly\s+)?(['"]?)([A-Za-z_$][\w$]*)\1(\?)?\s*:/.exec(text)
    if (!fm) continue // 索引签名 [key: string]: X 等，本卡不判
    fields.set(fm[2], { optional: Boolean(fm[3]) })
  }
  return fields
}

function splitTopLevel(body) {
  const out = []
  let cur = ''
  let depth = 0
  for (let i = 0; i < body.length; i++) {
    const c = body[i]
    if (c === '{' || c === '(' || c === '[') depth++
    else if (c === '}' || c === ')' || c === ']') depth--
    else if (c === "'" || c === '"' || c === '`') {
      const j = skipString(body, i, c)
      cur += body.slice(i, j + 1)
      i = j
      continue
    } else if (c === '/' && body[i + 1] === '*') {
      const j = body.indexOf('*/', i + 2)
      cur += ' '
      i = j + 1
      continue
    } else if (c === '/' && body[i + 1] === '/') {
      const j = body.indexOf('\n', i)
      i = j < 0 ? body.length : j - 1
      continue
    }
    if (depth === 0 && (c === ';' || c === '\n')) {
      if (cur.trim()) out.push(cur)
      cur = ''
      continue
    }
    cur += c
  }
  if (cur.trim()) out.push(cur)
  return out
}

function stripComments(s) {
  return s.replace(/\/\/[^\n]*/g, '').replace(/\/\*[\s\S]*?\*\//g, '')
}

// 展平继承链：接口 + 所有祖先的字段合并（父先子后，子可覆盖可选性）
function flatten(decls, name, seen = new Set()) {
  const d = decls.get(name)
  if (!d || seen.has(name)) return new Map()
  seen.add(name)
  const out = new Map()
  for (const p of d.parents) for (const [k, v] of flatten(decls, p, seen)) out.set(k, v)
  for (const [k, v] of d.fields) out.set(k, v)
  return out
}

// ---------- Go 侧 ----------

function walk(dir, acc = []) {
  for (const ent of fs.readdirSync(dir, { withFileTypes: true })) {
    if (ent.name === 'tmp' || ent.name === 'TestResults') continue
    const p = path.join(dir, ent.name)
    if (ent.isDirectory()) walk(p, acc)
    else if (ent.name.endsWith('.go') && !ent.name.endsWith('_test.go')) acc.push(p)
  }
  return acc
}

// type X struct { ... }  字段行：Name  Type  `json:"tag,omitempty"`
function parseGoStructs(file, src) {
  const out = new Map()
  const re = /^type\s+([A-Za-z_]\w*)\s+struct\s*\{/gm
  let m
  while ((m = re.exec(src))) {
    const name = m[1]
    const start = m.index + m[0].length
    const close = src.indexOf('\n}', start)
    if (close < 0) continue
    const body = src.slice(start, close)
    const line = src.slice(0, m.index).split('\n').length
    const fields = []
    for (const raw of body.split('\n')) {
      const text = raw.replace(/\/\/.*$/, '').trim()
      if (!text) continue
      const tagm = /`([^`]*)`/.exec(text)
      const tags = {}
      // 先取 Key:"value" 对再按逗号拆选项 —— 直接 split(',') 会切进引号里的 omitempty
      if (tagm) for (const pm of tagm[1].matchAll(/(\w+):"([^"]*)"/g)) tags[pm[1]] = pm[2]
      const fm = /^([A-Za-z_]\w*)\s+\S+/.exec(text)
      if (!fm) continue // 嵌入字段（无名字）单独记
      const jsonTag = tags.json
      if (jsonTag === undefined) {
        fields.push({ go: fm[1], key: fm[1], omitempty: false, tagged: false })
      } else if (jsonTag === '-') {
        fields.push({ go: fm[1], key: null, omitempty: false, tagged: true, skipped: true })
      } else {
        const key = jsonTag.split(',')[0] || fm[1]
        fields.push({ go: fm[1], key, omitempty: jsonTag.split(',').includes('omitempty'), tagged: true })
      }
    }
    if (!out.has(name)) out.set(name, { name, file: path.relative(ROOT, file).replace(/\\/g, '/'), line, fields })
  }
  return out
}

function loadGoStructs() {
  const all = new Map()
  const taggedNonDTO = []
  const collisions = new Map()
  for (const f of walk(SERVICES)) {
    const src = fs.readFileSync(f, 'utf8')
    const structs = parseGoStructs(f, src)
    for (const [n, s] of structs) {
      if (!all.has(n)) all.set(n, s)
      else {
        const prev = all.get(n)
        const sig = (x) => x.fields.map((y) => y.key).join(',')
        if (sig(prev) !== sig(s)) collisions.set(n, [prev, s])
      }
      if (s.fields.some((x) => x.tagged) && !/DTO$/.test(n)) taggedNonDTO.push(n + '@' + s.file)
    }
  }
  return { structs: all, taggedNonDTO, collisions }
}

const isDTO = (n) => n.endsWith('DTO')

// ---------- 判定 ----------

function main() {
  const tsSrc = fs.readFileSync(SHARED_TYPES, 'utf8')
  const tsDecls = parseSharedTypes(tsSrc)
  const { structs: goStructs, taggedNonDTO, collisions } = loadGoStructs()

  const goDTOs = [...goStructs.keys()].filter(isDTO).sort()
  const problems = []
  const notes = []

  // C1a 未登记的 DTO
  for (const g of goDTOs) {
    const entry = CONTRACT_MAP[g]
    if (!entry) {
      problems.push(`C1 未登记: ${g}  (${goStructs.get(g).file}:${goStructs.get(g).line}) 不在 CONTRACT_MAP —— 请映射到契约接口，或写明 ts: null + 理由`)
      continue
    }
    if (!goStructs.has(g)) problems.push(`C1 失配: CONTRACT_MAP 里的 ${g} 在 services/ 下已找不到该结构体`)
  }
  // C1b 映射指向的契约接口不存在 / 豁免必须带理由
  for (const [g, e] of Object.entries(CONTRACT_MAP)) {
    if (e.ts && !tsDecls.has(e.ts)) problems.push(`C1 失配: ${g} 映射到契约 ${e.ts}，shared-types 里没有该声明`)
    if (!goStructs.has(g) && !e.external) problems.push(`C1 失配: CONTRACT_MAP 登记的 ${g} 在 services/ 下找不到（改名或删掉了？）`)
    if (!e.ts && !e.reason) problems.push(`C1 无理由: ${g} 登记为「不对拍契约」(ts: null) 但没写 reason`)
  }
  // C1d 同名结构体在两个包里字段集不同 —— CONTRACT_MAP 按名字登记，这种歧义必须显式处理
  for (const [n, [a, b]] of collisions) {
    if (!isDTO(n) && !CONTRACT_MAP[n]) continue
    problems.push(`C1 歧义: ${n} 在 ${a.file}:${a.line} 与 ${b.file}:${b.line} 两处定义且 json 键集合不同，登记到契约时按 alias 消歧`)
  }
  // C1c 同名结构体即契约实体，却没登记 —— 覆盖不带 DTO 后缀的响应体（如 SensorPoint / DeviceConfig）
  for (const g of goStructs.keys()) {
    if (CONTRACT_MAP[g]) continue
    const s = goStructs.get(g)
    const wired = s.fields.some((f) => f.tagged)
    if (wired && tsDecls.has(g) && !isDTO(g)) {
      problems.push(`C1 未登记: ${g}  (${s.file}:${s.line}) 带 json tag 且与契约接口 ${g} 同名，却没登记进 CONTRACT_MAP`)
    }
  }

  // C2 / C3 逐字段对拍
  const rows = []
  for (const [g, e] of Object.entries(CONTRACT_MAP)) {
    if (!e.ts) continue
    const gs = goStructs.get(g)
    if (!gs || !tsDecls.has(e.ts)) continue
    const goKeys = new Map(gs.fields.filter((f) => f.key && !f.skipped).map((f) => [f.key, f]))
    const tsFields = flatten(tsDecls, e.ts)
    // ignore 必须逐键写理由（键 -> 理由），否则等于把门禁悄悄关掉
    const ignored = new Set(Object.keys(e.ignore || {}))
    for (const [k, why] of Object.entries(e.ignore || {})) {
      if (typeof why !== 'string' || !why.trim()) problems.push(`C1 缺理由: ${g} 对 ${e.ts} 豁免了 ${k} 却没写理由`)
    }
    const drift = { tsOnly: [], goOnly: [], optMiss: [] }

    for (const [k, v] of tsFields) {
      if (ignored.has(k)) continue
      if (!goKeys.has(k)) { drift.tsOnly.push(k); continue }
      // C3: 契约必填 + Go omitempty ⇒ 零值时键消失
      if (!v.optional && goKeys.get(k).omitempty) drift.optMiss.push(k)
    }
    for (const k of goKeys.keys()) {
      if (ignored.has(k)) continue
      if (!tsFields.has(k)) drift.goOnly.push(k)
    }
    rows.push({ go: g, file: `${gs.file}:${gs.line}`, ts: e.ts, tsLine: tsDecls.get(e.ts).line, ...drift })

    for (const k of drift.tsOnly) problems.push(`C2 键名: 契约 ${e.ts}.${k} 必填或可选，但 ${g} 无此 json tag`)
    for (const k of drift.goOnly) problems.push(`C2 键名: ${g} 回 ${k}，但契约 ${e.ts} 未声明（补契约或登记 ignore）`)
    for (const k of drift.optMiss) problems.push(`C3 可选性: 契约 ${e.ts}.${k} 声明必填，但 ${g} tag 带 omitempty（零值时前端拿不到该键）`)
  }

  if (DUMP) {
    for (const r of rows.sort((a, b) => a.go.localeCompare(b.go))) {
      const gk = [...new Set(goStructs.get(r.go).fields.map((f) => f.key).filter(Boolean))]
      const tk = [...flatten(tsDecls, r.ts).keys()]
      console.log(`\n### ${r.go} (${r.file})  <->  ${r.ts} (:${r.tsLine})`)
      console.log(`go(${gk.length}) ${gk.join(' ')}`)
      console.log(`ts(${tk.length}) ${tk.join(' ')}`)
      if (r.tsOnly.length) console.log(`  契约有Go无: ${r.tsOnly.join(' ')}`)
      if (r.goOnly.length) console.log(`  Go有契约无: ${r.goOnly.join(' ')}`)
      if (r.optMiss.length) console.log(`  必填却omitempty: ${r.optMiss.join(' ')}`)
    }
    const unmapped = goDTOs.filter((g) => !CONTRACT_MAP[g])
    console.log(`\n未登记 DTO ${unmapped.length}: ${unmapped.join(' ')}`)
    if (unmapped.length) {
      // 只帮我手工登记用，不参与判定：按 json 键集合的 Jaccard 相似度找最像的契约接口
      console.log('\n未登记 DTO 的候选契约（Jaccard，仅辅助登记）：')
      for (const g of unmapped.sort()) {
        const gk = new Set(goStructs.get(g).fields.map((f) => f.key).filter(Boolean))
        const scored = []
        for (const [t, d] of tsDecls) {
          const tk = new Set(flatten(tsDecls, t, new Set()).keys())
          if (!tk.size || !gk.size) continue
          let inter = 0
          for (const k of gk) if (tk.has(k)) inter++
          scored.push([t, inter / new Set([...gk, ...tk]).size])
        }
        scored.sort((a, b) => b[1] - a[1])
        const top = scored.slice(0, 2).filter(([, s]) => s > 0.2).map(([t, s]) => `${t} ${s.toFixed(2)}`)
        console.log(`  ${g} (${gk.size}键) -> ${top.join(' | ') || '无候选'}`)
      }
    }
    console.log(`未覆盖契约 ${[...tsDecls.keys()].filter((t) => !rows.some((r) => r.ts === t)).join(' ')}`)
    console.log(`带 json tag 但名字不以 DTO 结尾的结构体 ${taggedNonDTO.length}: ${taggedNonDTO.join(' ')}`)
    return 0
  }

  const compared = rows.length
  const gaps = Object.entries(CONTRACT_MAP).filter(([, e]) => !e.ts).length
  notes.push(`DTO 结构体 ${goDTOs.length} 个，已登记 ${Object.keys(CONTRACT_MAP).length} 条`)
  notes.push(`对拍 ${compared} 条（逐字段核键名与可选性）｜契约覆盖缺口 ${gaps} 条（登记为 ts:null + 理由，不比对字段）`)
  console.log(notes.join('\n'))
  if (!problems.length) {
    console.log('契约对拍通过：0 项不一致')
    return 0
  }
  const byKind = {}
  for (const p of problems) byKind[p.slice(0, 2)] = (byKind[p.slice(0, 2)] || 0) + 1
  console.log(`契约对拍判红 ${problems.length} 项：${JSON.stringify(byKind)}`)
  for (const p of problems) console.log('  ' + p)
  return 1
}

process.exit(main())
