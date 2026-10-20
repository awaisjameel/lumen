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

// The feature folders: each owns its views and store (CONVENTIONS.md).
const features = [
  'about',
  'albums',
  'duplicates',
  'events',
  'library',
  'media',
  'onboarding',
  'organise',
  'people',
  'places',
  'search',
  'settings',
  'things',
  'trash',
]

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

  // Only the bridge talks to the engine (see src/bridge/index.ts).
  {
    name: 'lumen/bridge-only',
    files: ['src/**/*.{ts,vue}'],
    ignores: ['src/bridge/**'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { group: ['**/bindings/**'], message: 'Call the engine through src/bridge.' },
            { group: ['@wailsio/runtime', '@wailsio/runtime/*'], message: 'Use the runtime through src/bridge.' },
          ],
        },
      ],
    },
  },

  // Shared code sits below the features: it never imports one (features
  // register with it instead, as stores do with shared/block). This block
  // replaces bridge-only's patterns for these files, so it repeats them.
  {
    name: 'lumen/shared-below-features',
    files: ['src/shared/**/*.{ts,vue}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { group: ['**/bindings/**'], message: 'Call the engine through src/bridge.' },
            { group: ['@wailsio/runtime', '@wailsio/runtime/*'], message: 'Use the runtime through src/bridge.' },
            {
              group: features.map((feature) => `../${feature}/*`),
              message: 'Shared code must not depend on a feature folder.',
            },
            { group: ['../app/*'], message: 'Shared code must not depend on the shell.' },
          ],
        },
      ],
    },
  },

  // The design system sits below everything but the bridge's types: tokens,
  // the icon manifest and how the appearance is applied know no feature,
  // shared composition or shell. This block replaces bridge-only's patterns
  // for these files, so it repeats them.
  {
    name: 'lumen/design-below-all',
    files: ['src/design/**/*.{ts,vue}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { group: ['**/bindings/**'], message: 'Call the engine through src/bridge.' },
            { group: ['@wailsio/runtime', '@wailsio/runtime/*'], message: 'Use the runtime through src/bridge.' },
            {
              group: [...features, 'shared', 'app'].map((folder) => `../${folder}/*`),
              message: 'The design system must not depend on shared code, a feature or the shell.',
            },
          ],
        },
      ],
    },
  },

  // Features sit below the shell: a feature never imports from app/ (the
  // shell composes features, and they reach it through shared registries).
  {
    name: 'lumen/features-below-shell',
    files: features.map((feature) => `src/${feature}/**/*.{ts,vue}`),
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { group: ['**/bindings/**'], message: 'Call the engine through src/bridge.' },
            { group: ['@wailsio/runtime', '@wailsio/runtime/*'], message: 'Use the runtime through src/bridge.' },
            { group: ['../app/*'], message: 'A feature must not depend on the shell: register with shared code.' },
          ],
        },
      ],
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
