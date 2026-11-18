import { readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const frontend = fileURLToPath(new URL('../', import.meta.url))
export function checkCompilerFixtures(name) {
  const fixtureDirectory = `tools/${name}-fixtures/`
  const expected = new Set()
  for (const name of ['invalid.ts', 'Invalid.vue']) {
    const lines = readFileSync(new URL(`${fixtureDirectory.slice(6)}${name}`, import.meta.url), 'utf8').split('\n')
    lines.forEach((line, index) => {
      if (line.includes('expect-rejection')) expected.add(`${fixtureDirectory}${name}:${index + 2}`)
    })
  }
  const result = spawnSync(
    process.execPath,
    [
      'node_modules/vue-tsc/bin/vue-tsc.js',
      '--noEmit',
      '--pretty',
      'false',
      '--project',
      `${fixtureDirectory}tsconfig.json`,
    ],
    { cwd: frontend, encoding: 'utf8' },
  )
  if (result.error) throw result.error
  const output = result.stdout + result.stderr
  const actual = new Set()
  const unexpected = []
  for (const line of output.split('\n').filter(Boolean)) {
    const match = /^(.*?)\((\d+),\d+\): error TS\d+:/.exec(line)
    if (!match) {
      if (!line.startsWith(' ')) unexpected.push(line)
      continue
    }
    const key = `${match[1].replaceAll('\\', '/')}:${match[2]}`
    if (expected.has(key)) actual.add(key)
    else unexpected.push(line)
  }
  const missing = [...expected].filter((key) => !actual.has(key))
  if (result.status !== 2 || missing.length || unexpected.length) {
    throw new Error(
      `${name} compiler fixtures failed. Missing rejections: ${missing.join(', ')}\n${unexpected.join('\n')}\n${output}`,
    )
  }
  process.stdout.write(
    `${name} compiler fixtures: ${actual.size} invalid TypeScript/Vue examples rejected; valid examples compile.\n`,
  )
}
