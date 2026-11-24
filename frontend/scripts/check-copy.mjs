// Copy gate: visible template text must not leak internal enums, field names or
// raw JSON. Migration window: scripts/copy-baseline.json lists legacy pages.
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { loadBaseline, read, report, root, visibleTextOf, walk } from './lib/scan.mjs'

const writeBaseline = process.argv.includes('--write-baseline')
const baseline = loadBaseline('scripts/copy-baseline.json')
const rules = {
  'academy-env': /ACADEMY_[A-Z_]+/g,
  'flag-mode': /\bflag_mode\b/g,
  'noop': /\bnoop\b/g,
  'append-only': /append-only/g,
  'slug': /\bslug\b/gi,
  'raw-enum': /\b(running|draft|published|archived|scheduled|ended|paused|pending)\b/g,
  'raw-json': /\{\s*"[a-z_]+"\s*:|"[a-z_]+"\s*:\s*[[{"0-9]/g,
  'wp': /\bWP\b/g,
  'room': /\bRoom\b/g,
  'account-id': /账号\s*ID/g,
  'ordinary-user': /普通人/g,
}
const found = Object.fromEntries(Object.keys(rules).map((rule) => [rule, new Set()]))
const violations = []

for (const file of walk('src/views', ['.vue'])) {
  const text = visibleTextOf(read(file))
  for (const [rule, pattern] of Object.entries(rules)) {
    for (const match of text.matchAll(pattern)) {
      if (writeBaseline) {
        found[rule].add(file)
        continue
      }
      if ((baseline[rule] ?? []).includes(file)) continue
      const index = match.index ?? 0
      const line = text.slice(0, index).split('\n').length
      violations.push(`${rule}: ${file}:${line} ${JSON.stringify(match[0])}`)
    }
  }
}

if (writeBaseline) {
  const output = Object.fromEntries(
    Object.keys(rules).map((rule) => [rule, [...found[rule]].sort()]).filter(([, files]) => files.length > 0),
  )
  writeFileSync(join(root, 'scripts/copy-baseline.json'), `${JSON.stringify(output, null, 2)}\n`)
  console.log(`✓ copy baseline written (${Object.keys(rules).map((rule) => `${rule}:${found[rule].size}`).join(', ')})`)
  process.exit(0)
}

process.exit(report('copy check', violations))
