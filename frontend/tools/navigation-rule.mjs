import path from 'node:path'

/** AST enforcement complements destination types; fixture strings and file paths are unrelated. */
export const navigationRule = {
  meta: {
    type: 'problem',
    schema: [],
    messages: { boundary: 'Use the shared typed navigation contract; raw routing belongs at its explicit boundary.' },
  },
  create(context) {
    const filename = context.filename.replaceAll('\\', '/')
    const relative = filename.slice(filename.lastIndexOf('/src/') + 1)
    const rawImports = {
      'src/router.ts': ['createWebHistory'],
      'src/shared/navigation/router.ts': ['createRouter', 'RouterHistory', 'RouteRecordRaw'],
      'src/shared/navigation/testing.ts': ['createMemoryHistory'],
      'src/shared/navigation/router.test.ts': ['createMemoryHistory'],
      'src/shared/navigation/NavLink.vue': ['RouterLink', 'RouterLinkProps'],
      'src/shared/navigation/NavigationView.vue': ['RouterView'],
    }
    const widgets = new Map()
    const report = (node) => context.report({ node, messageId: 'boundary' })
    function global(identifier, node) {
      if (identifier.type !== 'Identifier') return false
      let scope = context.sourceCode.getScope(node)
      while (scope) {
        const variable = scope.set.get(identifier.name)
        if (variable) return variable.defs.length === 0
        scope = scope.upper
      }
      return true
    }
    function browserLocation(node) {
      return (
        (node.type === 'Identifier' && node.name === 'location' && global(node, node)) ||
        (node.type === 'MemberExpression' &&
          node.property.type === 'Identifier' &&
          node.property.name === 'location' &&
          node.object.type === 'Identifier' &&
          ['window', 'document', 'globalThis'].includes(node.object.name) &&
          global(node.object, node))
      )
    }
    const listeners = {
      ImportDeclaration(node) {
        const source = node.source.value
        if (source === 'vue-router') {
          const allowed = rawImports[relative] ?? []
          if (
            node.specifiers.some((entry) => entry.type !== 'ImportSpecifier' || !allowed.includes(entry.imported.name))
          )
            report(node)
        }
        if (source.startsWith('.')) {
          const resolved = path
            .resolve(path.dirname(filename), source)
            .replaceAll('\\', '/')
            .replace(/\.(ts|vue)$/, '')
          if (resolved.endsWith('/src/router') && relative !== 'src/main.ts') report(node)
          if (
            resolved.endsWith('/shared/navigation/router') &&
            node.specifiers.some((entry) => entry.imported?.name === 'createNavigation') &&
            !['src/router.ts', 'src/shared/navigation/testing.ts', 'src/shared/navigation/router.test.ts'].includes(
              relative,
            )
          )
            report(node)
        }
        if (source === '@nuxt/ui/components/DropdownMenu.vue' || source === '@nuxt/ui/components/ContextMenu.vue') {
          if (
            ![
              'src/shared/navigation/menu.ts',
              'src/shared/navigation/NavDropdownMenu.vue',
              'src/shared/navigation/NavContextMenu.vue',
            ].includes(relative)
          )
            report(node)
        }
        for (const specifier of node.specifiers) widgets.set(specifier.local.name.toLowerCase(), source)
      },
      MemberExpression(node) {
        if (node.property.type !== 'Identifier') return
        if (['$router', '$route'].includes(node.property.name)) report(node)
      },
      CallExpression(node) {
        const callee = node.callee
        if (callee.type !== 'MemberExpression' || callee.property.type !== 'Identifier') return
        if (['pushState', 'replaceState'].includes(callee.property.name)) report(node)
        if (browserLocation(callee.object) && ['assign', 'replace'].includes(callee.property.name)) report(node)
        if (
          callee.property.name === 'visit' &&
          !filename.endsWith('.test.ts') &&
          relative !== 'src/shared/navigation/router.ts'
        )
          report(node)
      },
      AssignmentExpression(node) {
        const left = node.left
        if (browserLocation(left) || (left.type === 'MemberExpression' && browserLocation(left.object))) report(node)
      },
    }
    const template = {
      VElement(node) {
        const source = widgets.get(node.rawName.toLowerCase())
        if (
          ['routerlink', 'routerview', 'router-link', 'router-view'].includes(node.rawName.toLowerCase()) &&
          source !== 'vue-router' &&
          !source?.endsWith('/NavLink.vue') &&
          !source?.endsWith('/NavigationView.vue')
        )
          report(node)
      },
      VAttribute(node) {
        const name = node.directive ? node.key.argument?.name : node.key.name
        const element = node.parent.parent
        if (name === 'href' && !relative.startsWith('src/shared/navigation/')) {
          const value = node.directive ? node.value?.expression : node.value
          const text =
            value?.type === 'VLiteral' || value?.type === 'Literal'
              ? value.value
              : value?.type === 'TemplateLiteral' && value.expressions.length === 0
                ? value.quasis[0]?.value.cooked
                : undefined
          // An opaque href can hide the same internal route string as a
          // literal. Dynamic links need the typed adapter; explicit external
          // literals remain separate from application destinations.
          if (typeof text !== 'string' || !/^(?:https?:\/\/|mailto:|tel:|\/\/)/.test(text)) report(node)
        }
        if (name !== 'to' || element.name === 'teleport') return
        if (!node.directive) {
          report(node)
          return
        }
        const source = widgets.get(element.rawName.toLowerCase())
        if (
          ['@nuxt/ui/components/Button.vue', '@nuxt/ui/components/Link.vue'].includes(source) &&
          !relative.startsWith('src/shared/navigation/')
        )
          report(node)
      },
    }
    return context.sourceCode.parserServices.defineTemplateBodyVisitor?.(template, listeners) ?? listeners
  },
}
