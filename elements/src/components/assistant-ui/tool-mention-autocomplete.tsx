import { FC, useCallback, useEffect, useRef, useState } from 'react'
import { Wrench } from 'lucide-react'
import * as m from 'motion/react-m'

import { cn } from '@/lib/utils'
import { useDensity } from '@/hooks/useDensity'
import { useRadius } from '@/hooks/useRadius'
import { EASE_OUT_QUINT } from '@/lib/easing'
import {
  MentionableTool,
  detectMentionContext,
  filterToolsByQuery,
  insertToolMention,
} from '@/lib/tool-mentions'

export interface ToolMentionAutocompleteProps {
  tools: MentionableTool[]
  value: string
  cursorPosition: number
  onValueChange: (value: string, cursorPosition: number) => void
  textareaRef: React.RefObject<HTMLTextAreaElement | null>
  className?: string
}

export const ToolMentionAutocomplete: FC<ToolMentionAutocompleteProps> = ({
  tools,
  value,
  cursorPosition,
  onValueChange,
  textareaRef,
  className,
}) => {
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [isVisible, setIsVisible] = useState(false)
  const [filteredTools, setFilteredTools] = useState<MentionableTool[]>([])
  const [mentionContext, setMentionContext] = useState<{
    atPosition: number
    query: string
  } | null>(null)

  const containerRef = useRef<HTMLDivElement>(null)
  const d = useDensity()
  const r = useRadius()

  useEffect(() => {
    const context = detectMentionContext(value, cursorPosition)

    if (context.isInMention && tools.length > 0) {
      const filtered = filterToolsByQuery(tools, context.query)
      setFilteredTools(filtered)
      setIsVisible(filtered.length > 0)
      setMentionContext({
        atPosition: context.atPosition,
        query: context.query,
      })
      setSelectedIndex(0)
    } else {
      setIsVisible(false)
      setMentionContext(null)
    }
  }, [value, cursorPosition, tools])

  const selectTool = useCallback(
    (tool: MentionableTool) => {
      if (!mentionContext) return

      const result = insertToolMention(
        value,
        tool.name,
        mentionContext.atPosition,
        cursorPosition
      )

      onValueChange(result.text, result.cursorPosition)
      setIsVisible(false)

      setTimeout(() => {
        const textarea = textareaRef.current
        if (textarea) {
          textarea.focus()
          textarea.setSelectionRange(
            result.cursorPosition,
            result.cursorPosition
          )
        }
      }, 0)
    },
    [mentionContext, value, cursorPosition, onValueChange, textareaRef]
  )

  useEffect(() => {
    if (!isVisible) return

    const handleKeyDown = (e: KeyboardEvent) => {
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault()
          e.stopPropagation()
          setSelectedIndex((prev) => (prev + 1) % filteredTools.length)
          break
        case 'ArrowUp':
          e.preventDefault()
          e.stopPropagation()
          setSelectedIndex(
            (prev) => (prev - 1 + filteredTools.length) % filteredTools.length
          )
          break
        case 'Enter':
        case 'Tab':
          e.preventDefault()
          e.stopPropagation()
          if (filteredTools[selectedIndex]) {
            selectTool(filteredTools[selectedIndex])
          }
          break
        case 'Escape':
          e.preventDefault()
          e.stopPropagation()
          setIsVisible(false)
          break
      }
    }

    const textarea = textareaRef.current
    if (textarea) {
      textarea.addEventListener('keydown', handleKeyDown, { capture: true })
      return () =>
        textarea.removeEventListener('keydown', handleKeyDown, {
          capture: true,
        })
    }
  }, [isVisible, filteredTools, selectedIndex, selectTool, textareaRef])

  useEffect(() => {
    if (!isVisible) return

    const handleClickOutside = (e: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node) &&
        textareaRef.current &&
        !textareaRef.current.contains(e.target as Node)
      ) {
        setIsVisible(false)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [isVisible, textareaRef])

  if (!isVisible || filteredTools.length === 0) {
    return null
  }

  return (
    <m.div
      ref={containerRef}
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: 8 }}
      transition={{ duration: 0.15, ease: EASE_OUT_QUINT }}
      className={cn(
        'aui-tool-mention-autocomplete',
        'bg-popover text-popover-foreground absolute right-0 bottom-full left-0 z-50 mb-2 max-h-[200px] overflow-auto border shadow-md',
        r('lg'),
        className
      )}
    >
      <div className="flex flex-col gap-1 p-1">
        {filteredTools.map((tool, index) => (
          <button
            key={tool.id}
            type="button"
            className={cn(
              'aui-tool-mention-item flex w-full items-start gap-2 text-left transition-colors',
              r('md'),
              d('px-sm'),
              d('py-xs'),
              'hover:bg-accent hover:text-accent-foreground',
              index === selectedIndex && 'bg-accent text-accent-foreground'
            )}
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              selectTool(tool)
            }}
            onMouseEnter={() => setSelectedIndex(index)}
          >
            <Wrench className="mt-0.5 size-4 flex-shrink-0 opacity-50" />
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium">{tool.name}</div>
              {tool.description && (
                <div className="text-muted-foreground line-clamp-2 text-xs">
                  {tool.description}
                </div>
              )}
            </div>
          </button>
        ))}
      </div>
    </m.div>
  )
}

export default ToolMentionAutocomplete
