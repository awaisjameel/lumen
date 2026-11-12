import type { DropdownMenuItem } from '@nuxt/ui/components/DropdownMenu.vue'
import type { Destination } from './contract'

// Nuxt's arbitrary item index signature otherwise admits any shape for `to`.
type Known<T> = { [K in keyof T as string extends K ? never : number extends K ? never : K]: T[K] }
export type NavigationMenuItem = Omit<Known<DropdownMenuItem>, 'to' | 'href' | 'children' | 'external' | 'target'> & {
  to?: Destination
  children?: NavigationMenuItems
}
export type NavigationMenuItems = NavigationMenuItem[] | NavigationMenuItem[][]
function grouped(items: NavigationMenuItems): items is NavigationMenuItem[][] {
  return items.every((item) => Array.isArray(item))
}
/** Preserve commands, checked state and nested groups while adapting only links. */
export function resolvedMenu(
  items: NavigationMenuItems | undefined,
  href: (target: Destination) => string,
): DropdownMenuItem[] | DropdownMenuItem[][] {
  function item(value: NavigationMenuItem): DropdownMenuItem {
    return {
      ...value,
      to: value.to ? href(value.to) : undefined,
      children: value.children ? resolvedMenu(value.children, href) : undefined,
    }
  }
  if (!items) return []
  return grouped(items) ? items.map((group) => group.map(item)) : items.map(item)
}
