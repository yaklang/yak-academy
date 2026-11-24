// Design gate: raw hex, !important, static inline styles, off-scale spacing,
// off-role font sizes and ad-hoc radii are rejected outside the token files.
// Migration window: scripts/design-baseline.json lists files that still carry
// legacy values; the list must be empty once the refactor lands.
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { loadBaseline, read, report, root, styleOf, templateOf, walk } from './lib/scan.mjs'

const writeBaseline = process.argv.includes('--write-baseline')
const baseline = loadBaseline('scripts/design-baseline.json')
const rules = ['hex', 'important', 'inline-style', 'spacing', 'font-size', 'radius']
const found = Object.fromEntries(rules.map((rule) => [rule, new Set()]))
const violations = []

const TOKEN_FILES = new Set(['src/styles/tokens.css', 'src/styles/shadcn-variants.css'])
const ALLOWED_SPACING = new Set([0, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64])
const ALLOWED_RADIUS = new Set(['0', '6px', '8px', '10px', '12px', '16px', '20px', '24px', '999px', '50%'])

function lineOf(source, index) {
  return source.slice(0, index).split('\n').length
}

// `!important` is the only reliable way to neutralise transition/animation
// durations declared inside components, so the reduced-motion reset is an
// intentional exception rather than a legacy escape hatch.
function inReducedMotionBlock(source, index) {
  const marker = '@media (prefers-reduced-motion: reduce)'
  let cursor = source.indexOf(marker)
  while (cursor !== -1 && cursor < index) {
    const open = source.indexOf('{', cursor)
    if (open === -1 || open > index) break
    let depth = 0
    let close = -1
    for (let i = open; i < source.length; i += 1) {
      if (source[i] === '{') depth += 1
      else if (source[i] === '}') {
        depth -= 1
        if (depth === 0) {
          close = i
          break
        }
      }
    }
    if (close === -1 || index < close) return true
    cursor = source.indexOf(marker, close)
  }
  return false
}

function add(rule, file, source, index, message) {
  if (writeBaseline) {
    found[rule].add(file)
    return
  }
  if ((baseline[rule] ?? []).includes(file)) return
  violations.push(`${rule}: ${file}:${lineOf(source, index)} ${message}`)
}

for (const file of walk('src')) {
  if (TOKEN_FILES.has(file)) continue
  const source = read(file)
  const css = file.endsWith('.vue') ? `${templateOf(source)}\n${styleOf(source)}` : source

  for (const match of css.matchAll(/#[0-9a-fA-F]{3,8}\b/g)) {
    add('hex', file, css, match.index, `raw colour ${match[0]}`)
  }
  for (const match of css.matchAll(/!important/g)) {
    if (inReducedMotionBlock(css, match.index)) continue
    add('important', file, css, match.index, '!important')
  }
  if (file.endsWith('.vue')) {
    for (const match of templateOf(source).matchAll(/\sstyle="/g)) {
      add('inline-style', file, source, match.index, 'static inline style')
    }
  }
  for (const match of css.matchAll(/font-size\s*:\s*([^;}\n]+)/g)) {
    const value = match[1].trim()
    if (value.startsWith('var(--text-') || value === 'inherit' || value === '0') continue
    add('font-size', file, css, match.index, `font-size ${value}`)
  }
  for (const match of css.matchAll(/(?:^|[;{\s])((?:padding|margin|gap|row-gap|column-gap)(?:-(?:top|right|bottom|left|inline|block))?)\s*:\s*([^;}\n]+)/g)) {
    for (const px of match[2].matchAll(/(-?\d*\.?\d+)px/g)) {
      if (ALLOWED_SPACING.has(Number(px[1]))) continue
      add('spacing', file, css, match.index, `${match[1]}: ${match[2].trim()}`)
    }
  }
  for (const match of css.matchAll(/(?:^|[;{\s])border-radius\s*:\s*([^;}\n]+)/g)) {
    const tokens = match[1].trim().split(/\s+/)
    if (tokens.every((token) => token.startsWith('var(--radius') || ALLOWED_RADIUS.has(token))) continue
    add('radius', file, css, match.index, `border-radius ${match[1].trim()}`)
  }
}

if (writeBaseline) {
  const output = Object.fromEntries(
    rules.map((rule) => [rule, [...found[rule]].sort()]).filter(([, files]) => files.length > 0),
  )
  writeFileSync(join(root, 'scripts/design-baseline.json'), `${JSON.stringify(output, null, 2)}\n`)
  console.log(`✓ design baseline written (${rules.map((rule) => `${rule}:${found[rule].size}`).join(', ')})`)
  process.exit(0)
}

process.exit(report('design check', violations))
