import { strict as assert } from 'node:assert'
import { spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

for (const failed of [false, true]) {
  test(`a report with ${failed ? 'a cleanup exception fails' : 'no exception passes'} the process`, () => {
    const work = mkdtempSync(join(tmpdir(), 'lumen-suite-exit-'))
    try {
      const run = spawnSync(process.execPath, ['--input-type=module', '-e', `
        import { suite } from ${JSON.stringify(new URL('./suite.mjs', import.meta.url).href)}
        const s = suite('onboarding')
        s.check('behaviour passed', true)
        s.finish({ failure: ${failed ? "{ phase: 'cleanup', error: 'planned cleanup failure' }" : 'undefined'} })
      `], { encoding: 'utf8', env: { ...process.env, LUMEN_EXE: process.execPath, LUMEN_E2E_WORK: work } })
      assert.equal(run.error, undefined)
      const report = JSON.parse(readFileSync(join(work, 'results', 'onboarding', 'report.json'), 'utf8'))
      assert.equal(run.status, failed ? 1 : 0, run.stdout + run.stderr)
      assert.equal(report.checks.filter(check => !check.pass).length, failed ? 1 : 0)
      if (failed) assert.deepEqual(report.failure, { phase: 'cleanup', error: 'planned cleanup failure' })
    } finally {
      rmSync(work, { recursive: true, force: true })
    }
  })
}
