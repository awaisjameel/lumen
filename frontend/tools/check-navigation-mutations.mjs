import { cpSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const frontend = fileURLToPath(new URL('../', import.meta.url))
const scratch = mkdtempSync(join(tmpdir(), 'lumen-navigation-'))
try {
  for (const name of ['src', 'bindings', 'tsconfig.json'])
    cpSync(join(frontend, name), join(scratch, name), { recursive: true })
  symlinkSync(join(frontend, 'node_modules'), join(scratch, 'node_modules'), 'junction')
  const contract = join(scratch, 'src/shared/navigation/contract.ts')
  const things = join(scratch, 'src/things/ThingsView.vue')
  const originalContract = readFileSync(contract, 'utf8')
  const originalThings = readFileSync(things, 'utf8')
  function rejects(label, expected) {
    const result = spawnSync(
      process.execPath,
      [
        join(frontend, 'node_modules/vue-tsc/bin/vue-tsc.js'),
        '--noEmit',
        '--pretty',
        'false',
        '--project',
        join(scratch, 'tsconfig.json'),
      ],
      { cwd: frontend, encoding: 'utf8' },
    )
    if (result.error) throw result.error
    const output = (result.stdout + result.stderr).replaceAll('\\', '/')
    if (result.status !== 2 || expected.some((text) => !output.includes(text)))
      throw new Error(`${label} did not fail the required compiler checks:\n${output}`)
    process.stdout.write(`Navigation mutation rejected: ${label}.\n`)
  }
  writeFileSync(contract, originalContract.replace("route('blocked',", "route('retiredBlocked',"))
  rejects('renamed Blocked identity', ['ThingsView.vue', 'PlacesView.vue', 'EventsView.vue', 'contract.test.ts'])
  writeFileSync(contract, originalContract)
  const broken = originalThings.replace(':to="destination({ name: \'blocked\' })"', 'to="/blocked"')
  if (broken === originalThings) throw new Error('The original recovery-link mutation did not apply')
  writeFileSync(things, broken)
  rejects('original /blocked recovery link', ['ThingsView.vue', "Type 'string' is not assignable to type 'Destination"])
  writeFileSync(things, originalThings)
  const marker = '] as const\n\nexport type Definition'
  if (!originalContract.includes(marker)) throw new Error('The unbound-view mutation did not apply')
  writeFileSync(
    contract,
    originalContract.replace(
      marker,
      "  route('unbound', '/unbound', noParams, noQuery, { view: 'unbound' }),\n" + marker,
    ),
  )
  rejects('new destination without a view binding or contract example', [
    'src/router.ts',
    "'unbound'",
    'contract.test.ts',
  ])
} finally {
  rmSync(scratch, { recursive: true, force: true })
}
