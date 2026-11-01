import { Fragment, isVNode, type InjectionKey, type VNode } from 'vue'

export const fieldContext: InjectionKey<{ id: string; labelId: string; required: boolean }> = Symbol('academy-form-field')
export function flattenNodes(nodes: unknown): VNode[] {
  if (!Array.isArray(nodes)) return []
  return nodes.flatMap(node => {
    if (!isVNode(node)) return []
    if (node.type === Fragment) return flattenNodes(node.children)
    return [node]
  })
}
export function childComponents(nodes: unknown, name: string) {
  return flattenNodes(nodes).filter(node => (node.type as { name?: string })?.name === name)
}
export function content(nodes: unknown) { return flattenNodes(nodes) }
export function withoutStyle(attrs: Record<string, unknown>) {
  const { class: _class, style: _style, ...rest } = attrs
  return rest
}
