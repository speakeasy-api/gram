// Type for json-render tree structure
export interface JsonRenderNode {
  type: string
  props?: Record<string, unknown>
  children?: JsonRenderNode[]
}

/**
 * Check if content is a json-render compatible tree structure
 */
export function isJsonRenderTree(content: unknown): content is JsonRenderNode {
  return (
    typeof content === 'object' &&
    content !== null &&
    'type' in content &&
    typeof (content as JsonRenderNode).type === 'string'
  )
}
