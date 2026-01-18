export type ToolRecord = Record<string, unknown> | undefined

export interface MentionableTool {
  id: string
  name: string
  description?: string
}

export interface MentionContext {
  isInMention: boolean
  query: string
  atPosition: number
}

const MENTION_PATTERN = /@(\w+)/g

export function toolSetToMentionableTools(
  tools: ToolRecord
): MentionableTool[] {
  if (!tools) return []

  return Object.entries(tools).map(([name, tool]) => ({
    id: name,
    name,
    description:
      typeof tool === 'object' && tool !== null && 'description' in tool
        ? String((tool as { description?: unknown }).description ?? '')
        : undefined,
  }))
}

export function parseMentionedTools(text: string, tools: ToolRecord): string[] {
  if (!tools || !text) return []

  const toolNames = Object.keys(tools)
  const mentions: string[] = []
  let match: RegExpExecArray | null

  MENTION_PATTERN.lastIndex = 0
  while ((match = MENTION_PATTERN.exec(text)) !== null) {
    mentions.push(match[1].toLowerCase())
  }

  const matchedToolIds = toolNames.filter((name) =>
    mentions.includes(name.toLowerCase())
  )

  return [...new Set(matchedToolIds)]
}

export function detectMentionContext(
  text: string,
  cursorPosition: number
): MentionContext {
  const textBeforeCursor = text.slice(0, cursorPosition)
  const lastAtSymbol = textBeforeCursor.lastIndexOf('@')

  if (lastAtSymbol === -1) {
    return { isInMention: false, query: '', atPosition: -1 }
  }

  const textAfterAt = textBeforeCursor.slice(lastAtSymbol + 1)

  if (textAfterAt.includes(' ') || textAfterAt.includes('\n')) {
    return { isInMention: false, query: '', atPosition: -1 }
  }

  return {
    isInMention: true,
    query: textAfterAt.toLowerCase(),
    atPosition: lastAtSymbol,
  }
}

export function filterToolsByQuery(
  tools: MentionableTool[],
  query: string
): MentionableTool[] {
  if (!query) return tools

  const queryLower = query.toLowerCase()

  return tools.filter((tool) => {
    const nameMatch = tool.name.toLowerCase().includes(queryLower)
    const descMatch = tool.description?.toLowerCase().includes(queryLower)
    return nameMatch || descMatch
  })
}

export function insertToolMention(
  text: string,
  toolName: string,
  atPosition: number,
  cursorPosition: number
): { text: string; cursorPosition: number } {
  const beforeMention = text.slice(0, atPosition)
  const afterCursor = text.slice(cursorPosition)
  const newText = `${beforeMention}@${toolName} ${afterCursor}`
  const newCursorPosition = atPosition + toolName.length + 2
  return { text: newText, cursorPosition: newCursorPosition }
}

export function removeToolMention(text: string, toolName: string): string {
  const pattern = new RegExp(`@${toolName}\\s?`, 'gi')
  return text.replace(pattern, '')
}
