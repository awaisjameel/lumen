import { expect, it } from 'vitest'
import { Linter } from 'eslint'
import vueParser from 'vue-eslint-parser'
import tseslint from 'typescript-eslint'
import { navigationRule } from './navigation-rule.mjs'

function lint(code, filename = '/fixture/src/Example.vue') {
  const linter = new Linter({ cwd: '/fixture' })
  return linter.verify(
    code,
    [
      {
        files: ['**/*.{vue,ts}'],
        languageOptions: {
          parser: filename.endsWith('.vue') ? vueParser : tseslint.parser,
          parserOptions: { parser: tseslint.parser, ecmaVersion: 'latest', sourceType: 'module' },
        },
        plugins: { navigation: { rules: { contract: navigationRule } } },
        rules: { 'navigation/contract': 'error' },
      },
    ],
    { filename },
  )
}
it.each([
  "import { useRouter } from 'vue-router'",
  "import type { RouteLocationRaw } from 'vue-router'",
  "import { createRouter, createMemoryHistory } from 'vue-router'",
  "import router from './router'",
  "import { createNavigation } from './shared/navigation/router'",
  "import Menu from '@nuxt/ui/components/DropdownMenu.vue'",
  "window.history.pushState({}, '', '/blocked')",
  "location.href = '/blocked'",
  "window.location.href = '/blocked'",
  "window.location.assign('/blocked')",
  "document.location.replace('/blocked')",
  "navigation.visit('/blocked')",
])('rejects a script bypass: %s', (code) => {
  expect(lint(code, '/fixture/src/Example.ts').some((message) => message.ruleId === 'navigation/contract')).toBe(true)
})
it.each([
  '<template><a href="/blocked">Blocked</a></template>',
  '<template><a :href="\'/blocked\'">Blocked</a></template>',
  '<template><RouterLink to="/blocked">Blocked</RouterLink></template>',
  '<script setup lang="ts">import UButton from "@nuxt/ui/components/Button.vue"</script><template><UButton :to="{ name: \'blocked\' }" /></template>',
  '<script setup lang="ts">import NavLink from "./shared/navigation/NavLink.vue"</script><template><NavLink to="/blocked" /></template>',
])('rejects a template bypass', (code) => {
  expect(lint(code).some((message) => message.ruleId === 'navigation/contract')).toBe(true)
})
it('accepts typed links, external fixture data and local file history', () => {
  expect(
    lint(
      '<script setup lang="ts">import NavLink from "./shared/navigation/NavLink.vue"; import { destination } from "./shared/navigation/contract"</script><template><NavLink :to="destination({ name: \'blocked\' })" /><Teleport to="body" /></template>',
    ),
  ).toEqual([])
  expect(lint('<template><a href="https://example.org">External</a></template>')).toEqual([])
  expect(
    lint(
      "const history = { value: [] }; history.value = [{ to: 'D:/pictures' }]; const fixture = '/blocked'; const url = 'https://example.org';",
      '/fixture/src/Example.ts',
    ),
  ).toEqual([])
})
it('permits only the named infrastructure imports', () => {
  expect(lint("import { createMemoryHistory } from 'vue-router'", '/fixture/src/shared/navigation/testing.ts')).toEqual(
    [],
  )
  expect(lint("import { useRouter } from 'vue-router'", '/fixture/src/shared/navigation/testing.ts')).toHaveLength(1)
})
