// @ts-check
// Lint rules for the UI: ESLint's and Vue's recommended sets, typescript-eslint's
// strict type-aware rules, and the conventions this codebase follows. Formatting
// is Prettier's job (npm run format), so every stylistic rule that would fight
// it is turned off last.
import js from '@eslint/js'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'
import prettier from 'eslint-config-prettier/flat'
import pluginVue from 'eslint-plugin-vue'
import globals from 'globals'

export default defineConfigWithVueTs(
  { name: 'lumen/ignores', ignores: ['dist/', 'bindings/'] },

  js.configs.recommended,
  pluginVue.configs['flat/recommended'],
  vueTsConfigs.strictTypeChecked,
  vueTsConfigs.stylisticTypeChecked,

  {
    name: 'lumen/rules',
    files: ['src/**/*.{ts,vue}'],
    languageOptions: { globals: globals.browser },
    rules: {
      eqeqeq: ['error', 'smart'],
      'no-console': ['error', { allow: ['error'] }], // shared/errors.ts logs; nothing else should
      'prefer-template': 'error',
      'object-shorthand': 'error',

      // Imports: types apart, as verbatimModuleSyntax needs.
      '@typescript-eslint/consistent-type-imports': ['error', { fixStyle: 'separate-type-imports' }],
      '@typescript-eslint/no-import-type-side-effects': 'error',
      // Numbers read naturally in messages and CSS values (`${size}px`).
      '@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],
      // Callbacks like `() => clearTimeout(timer)` are clear as they are.
      '@typescript-eslint/no-confusing-void-expression': ['error', { ignoreArrowShorthand: true }],
      // `name || t('person.unnamed')`: an empty name means "no name" too.
      '@typescript-eslint/prefer-nullish-coalescing': ['error', { ignorePrimitives: { string: true } }],

      // Components: <script setup lang="ts">, then template, then style.
      'vue/block-lang': ['error', { script: { lang: 'ts' } }],
      'vue/block-order': ['error', { order: ['script', 'template', 'style'] }],
      'vue/component-api-style': ['error', ['script-setup']],
      'vue/define-macros-order': 'error',
      'vue/no-unused-refs': 'error',
      'vue/no-useless-v-bind': 'error',
      'vue/no-useless-mustaches': 'error',
      'vue/require-macro-variable-name': 'error',
      'vue/no-undef-components': ['error', { ignorePatterns: ['RouterLink', 'RouterView'] }],
      'vue/no-root-v-if': 'off',
      // App-level views are named for what they are (Viewer, Icon).
      'vue/multi-word-component-names': 'off',
    },
  },

  // Tool configs run in Node and sit outside the app's tsconfig.
  {
    name: 'lumen/configs',
    files: ['*.config.{js,ts}'],
    extends: [vueTsConfigs.disableTypeChecked],
    languageOptions: { globals: globals.node, parserOptions: { projectService: false } },
  },

  prettier,
)
