// T324：把本轮 e2e-real 里「post-deploy 跳过」的用例汇总成可读清单。
//
// 判红/判绿不由本脚本决定（那条逻辑在 e2e-real/deploy-guard.ts 里，PR 阶段跳过、部署后阶段判红）；
// 本脚本只负责让 reviewer 在 PR 界面上一眼看到「哪些新行为断言这轮没在 staging 验到、等哪次部署」。
// 用法：node e2e-real/report-post-deploy.mjs [result.json 路径...]
import fs from 'node:fs'

const candidates = process.argv.slice(2).length
  ? process.argv.slice(2)
  : ['e2e-real/test-results/result.json', 'test-results/result.json']

const file = candidates.find((p) => fs.existsSync(p))
if (!file) {
  console.log('（未找到 Playwright JSON 报告，跳过 post-deploy 汇总）')
  process.exit(0)
}

const report = JSON.parse(fs.readFileSync(file, 'utf8'))
const rows = []

function walk(node, trail) {
  if (!node || typeof node !== 'object') return
  const title = typeof node.title === 'string' ? node.title : null
  const path = title ? [...trail, title] : trail
  const anns = Array.isArray(node.annotations) ? node.annotations : null
  if (anns) {
    for (const a of anns) {
      if (a && a.type === 'post-deploy') rows.push({ spec: path.slice(1).join(' › ') || path[0], desc: a.description || '' })
    }
  }
  for (const key of ['suites', 'specs']) {
    if (Array.isArray(node[key])) for (const child of node[key]) walk(child, path)
  }
  if (Array.isArray(node.tests)) {
    for (const t of node.tests) {
      for (const a of (t.annotations || [])) {
        if (a && a.type === 'post-deploy') rows.push({ spec: [...path, t.projectTitle ?? ''].filter(Boolean).join(' › '), desc: a.description || '' })
      }
    }
  }
}

for (const suite of report.suites || []) walk(suite, [])

const stats = report.stats || {}
console.log('## e2e-real 真实模式：本轮结论')
console.log('')
console.log(`- 用例统计：${stats.expected ?? '?'} passed / ${stats.skipped ?? '?'} skipped / ${stats.unexpected ?? '?'} failed`)
if (!rows.length) {
  console.log('- post-deploy 跳过：0 条 —— staging 上部署的构建已含本轮全部断言所需的行为')
} else {
  console.log(`- post-deploy 标记：${rows.length} 条（断的行为尚未部署到 staging —— PR 阶段显式跳过，部署后阶段判红）`)
  console.log('')
  console.log('| 用例 | 缺失的构建标记 |')
  console.log('| --- | --- |')
  for (const r of rows) console.log(`| ${r.spec} | ${r.desc} |`)
}
