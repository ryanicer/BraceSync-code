#!/usr/bin/env node
/**
 * T465: compare the in-repo mapping-table mirror with the docs-repo copy.
 *
 * Why a local script and not a CI step: the code CI never checks out the docs repo
 * (T370 measured this - reading docs from code CI just throws), so a cross-repo
 * assertion cannot be wired. CI does lock code truth vs this repo's mirror
 * (packages/shared-utils/test/error-copy-doc.test.ts); this script closes the
 * remaining gap by hand at delivery/deploy time.
 *
 * usage: node scripts/ci/check-error-copy-doc-sync.mjs <codeMirror.md> <docsCopy.md>
 * exit:  0 identical, 1 differs, 2 usage/IO error
 */
import { readFileSync } from 'node:fs'
import { createHash } from 'node:crypto'

const [codePath, docsPath] = process.argv.slice(2)
if (!codePath || !docsPath) {
  console.error('usage: node scripts/ci/check-error-copy-doc-sync.mjs <codeMirror.md> <docsCopy.md>')
  process.exit(2)
}

function load(p) {
  const raw = readFileSync(p, 'utf8')
  // CRLF-safe compare: the docs clone may be checked out with a different autocrlf setting
  const norm = raw.replace(/\r\n/g, '\n')
  return {
    bytes: Buffer.byteLength(raw, 'utf8'),
    sha256: createHash('sha256').update(norm, 'utf8').digest('hex'),
    lines: norm.split('\n'),
  }
}

const a = load(codePath)
const b = load(docsPath)
console.log(`file_a=${codePath} bytes=${a.bytes} sha256=${a.sha256}`)
console.log(`file_b=${docsPath} bytes=${b.bytes} sha256=${b.sha256}`)
if (a.sha256 === b.sha256) {
  console.log('RESULT=IDENTICAL')
  process.exit(0)
}
console.log('RESULT=DIFFERS')
const max = Math.max(a.lines.length, b.lines.length)
let shown = 0
for (let i = 0; i < max && shown < 20; i++) {
  if (a.lines[i] !== b.lines[i]) {
    console.log(`line ${i + 1}`)
    console.log(`  code: ${a.lines[i] ?? '<missing>'}`)
    console.log(`  docs: ${b.lines[i] ?? '<missing>'}`)
    shown++
  }
}
if (shown === 20) console.log('(first 20 diffs shown only)')
process.exit(1)
