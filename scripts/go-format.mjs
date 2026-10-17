import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const args = process.argv.slice(2)
if (args.some((argument) => argument !== '--write') || args.length > 1) {
  console.error('Usage: node scripts/go-format.mjs [--write]')
  process.exit(1)
}
const result = spawnSync('gofmt', [args.includes('--write') ? '-w' : '-l', 'cmd', 'internal'], {
  cwd: fileURLToPath(new URL('../', import.meta.url)),
  encoding: 'utf8',
  windowsHide: true,
})
if (result.error || result.status !== 0) {
  console.error(result.error?.message || result.stderr)
  process.exit(1)
}
if (result.stdout.trim()) {
  console.error('Run node scripts/go-format.mjs --write to format:\n' + result.stdout)
  process.exit(1)
}
console.log('Go formatting OK')
