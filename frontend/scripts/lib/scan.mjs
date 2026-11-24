import { readdirSync, readFileSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

export const root = process.cwd()

export function walk(dir, extensions = ['.vue', '.css']) {
  const out = []
  for (const entry of readdirSync(join(root, dir), { withFileTypes: true })) {
    const rel = join(dir, entry.name)
    if (entry.isDirectory()) {
      if (['node_modules', 'dist', '.git'].includes(entry.name)) continue
      out.push(...walk(rel, extensions))
    } else if (extensions.some((ext) => entry.name.endsWith(ext))) {
      out.push(rel.split(sep).join('/'))
    }
  }
  return out.sort()
}

export function read(rel) {
  return readFileSync(join(root, rel), 'utf8')
}

export function templateOf(source) {
  const match = source.match(/<template>([\s\S]*?)<\/template>/)
  return match ? match[1] : ''
}

export function styleOf(source) {
  const blocks = [...source.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)]
  return blocks.map((block) => block[1]).join('\n')
}

export function visibleTextOf(source) {
  return templateOf(source)
    .replace(/<!--[\s\S]*?-->/g, ' ')
    .replace(/<[^>]+>/g, ' ')
}

export function baselined(baseline, rule, file) {
  return (baseline[rule] ?? []).includes(file)
}

export function loadBaseline(rel) {
  try {
    return JSON.parse(read(rel))
  } catch {
    return {}
  }
}

export function report(title, violations) {
  if (violations.length === 0) {
    console.log(`✓ ${title}`)
    return 0
  }
  console.error(`✗ ${title}`)
  for (const violation of violations) console.error(`  ${violation}`)
  return 1
}

export function relFromRoot(file) {
  return relative(root, join(root, file)).split(sep).join('/')
}
