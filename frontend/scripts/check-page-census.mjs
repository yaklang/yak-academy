// Page census: every routed page owns exactly one h1 (PageHeader provides it),
// and any page rendering a table uses DataTablePanel as its only container.
// Full-screen competition shells and public pages are explicit exemptions.
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { loadBaseline, read, report, root, templateOf, walk } from './lib/scan.mjs'

const writeBaseline = process.argv.includes('--write-baseline')
const baseline = loadBaseline('scripts/census-baseline.json')
const pending = new Set(baseline.pending ?? [])
const violations = []
const found = []

// 满屏外壳与公开页自带版式，不进入应用 PageHeader 体系。
const EXEMPT = new Set([
  'src/views/Landing.vue',
  'src/views/Login.vue',
  'src/views/CertificateVerify.vue',
  'src/views/CompetitionEnter.vue',
  'src/views/CompetitionHome.vue',
  'src/views/competition/CompetitionEntry.vue',
  'src/views/admin/CompetitionDetailEntry.vue',
  'src/views/admin/awd/AwdAdmin.vue',
  'src/views/admin/exam/ExamAdmin.vue',
  'src/views/competition/awd/AwdParticipant.vue',
  'src/views/competition/exam/ExamParticipant.vue',
])

for (const file of walk('src/views')) {
  if (EXEMPT.has(file)) continue
  const template = templateOf(read(file))
  const hasHeader = /<PageHeader[\s>]/.test(template)
  const h1Count = (template.match(/<h1[\s>]/g) ?? []).length
  const usesTable = /<UiTable[\s>]/.test(template)
  const usesPanel = /<DataTablePanel[\s>]/.test(template)
  const problems = []
  if (!hasHeader) problems.push('未使用 PageHeader')
  if (hasHeader && h1Count > 0) problems.push('PageHeader 与手写 h1 同时存在')
  if (usesTable && !usesPanel && !['src/views/RoomDetail.vue'].includes(file)) problems.push('表格未使用 DataTablePanel')

  if (problems.length === 0) {
    pending.delete(file)
    continue
  }
  found.push(file)
  if (writeBaseline) continue
  if (pending.has(file)) continue
  for (const problem of problems) violations.push(`${file} ${problem}`)
}

if (writeBaseline) {
  writeFileSync(join(root, 'scripts/census-baseline.json'), `${JSON.stringify({ pending: found.sort() }, null, 2)}\n`)
  console.log(`✓ census baseline written (${found.length} pending pages)`)
  process.exit(0)
}

process.exit(report('page census', violations))
