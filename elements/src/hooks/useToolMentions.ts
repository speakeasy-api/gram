import { useCallback, useMemo, useRef, useState } from 'react'
import { useAssistantApi, useAssistantState } from '@assistant-ui/react'
import {
  MentionableTool,
  parseMentionedTools,
  removeToolMention,
  toolSetToMentionableTools,
} from '@/lib/tool-mentions'

export interface UseToolMentionsOptions {
  tools: Record<string, unknown> | undefined
  enabled?: boolean
}

export interface UseToolMentionsReturn {
  mentionableTools: MentionableTool[]
  mentionedToolIds: string[]
  value: string
  cursorPosition: number
  textareaRef: React.RefObject<HTMLTextAreaElement | null>
  updateCursorPosition: () => void
  handleAutocompleteChange: (value: string, cursorPosition: number) => void
  removeMention: (toolId: string) => void
  isActive: boolean
}

export function useToolMentions({
  tools,
  enabled = true,
}: UseToolMentionsOptions): UseToolMentionsReturn {
  const [cursorPosition, setCursorPosition] = useState(0)
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)
  const api = useAssistantApi()
  const composerText = useAssistantState(({ composer }) => composer.text)

  const mentionableTools = useMemo(
    () => toolSetToMentionableTools(tools),
    [tools]
  )

  const mentionedToolIds = useMemo(
    () => (enabled ? parseMentionedTools(composerText, tools) : []),
    [composerText, tools, enabled]
  )

  const updateCursorPosition = useCallback(() => {
    const textarea = textareaRef.current
    if (textarea) {
      setCursorPosition(textarea.selectionStart)
    }
  }, [])

  const handleAutocompleteChange = useCallback(
    (newValue: string, newCursorPosition: number) => {
      api.composer().setText(newValue)
      setCursorPosition(newCursorPosition)

      setTimeout(() => {
        const textarea = textareaRef.current
        if (textarea) {
          textarea.focus()
          textarea.setSelectionRange(newCursorPosition, newCursorPosition)
        }
      }, 0)
    },
    [api]
  )

  const removeMention = useCallback(
    (toolId: string) => {
      const tool = mentionableTools.find((t) => t.id === toolId)
      if (tool) {
        const newValue = removeToolMention(composerText, tool.name)
        api.composer().setText(newValue)
      }
    },
    [composerText, mentionableTools, api]
  )

  const isActive = enabled && mentionableTools.length > 0

  return {
    mentionableTools,
    mentionedToolIds,
    value: composerText,
    cursorPosition,
    textareaRef,
    updateCursorPosition,
    handleAutocompleteChange,
    removeMention,
    isActive,
  }
}
