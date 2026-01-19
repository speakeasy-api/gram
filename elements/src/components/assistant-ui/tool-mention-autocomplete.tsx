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

  // Scroll selected item into view
  useEffect(() => {
    if (!isVisible) return
    const container = containerRef.current
    if (!container) return

    const selectedItem = container.querySelector(
      `[data-index="${selectedIndex}"]`
    ) as HTMLElement
    if (selectedItem) {
      selectedItem.scrollIntoView({ block: 'nearest' })
    }
  }, [selectedIndex, isVisible])

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

  // When autocomplete is visible, modify composer styles and position autocomplete to match composer width
  useEffect(() => {
    const textarea = textareaRef.current
    if (!textarea) return

    const composer = textarea.closest('.aui-composer-root') as HTMLElement
    if (!composer) return

    const updateStyles = () => {
      const autocomplete = containerRef.current
      if (!autocomplete) return

      if (isVisible) {
        // Modify composer to connect with autocomplete
        composer.style.borderTopColor = 'var(--ring)'
        composer.style.borderTopLeftRadius = '0'
        composer.style.borderTopRightRadius = '0'

        // Position autocomplete to match composer width
        const composerRect = composer.getBoundingClientRect()
        const autocompleteParent = autocomplete.offsetParent as HTMLElement
        if (autocompleteParent) {
          const parentRect = autocompleteParent.getBoundingClientRect()
          autocomplete.style.left = `${composerRect.left - parentRect.left}px`
          autocomplete.style.right = 'auto'
          autocomplete.style.width = `${composerRect.width}px`
        }
      }
    }

    if (isVisible) {
      // Use requestAnimationFrame to ensure DOM is updated
      requestAnimationFrame(updateStyles)
    } else {
      composer.style.borderTopColor = ''
      composer.style.borderTopLeftRadius = ''
      composer.style.borderTopRightRadius = ''
    }

    return () => {
      composer.style.borderTopColor = ''
      composer.style.borderTopLeftRadius = ''
      composer.style.borderTopRightRadius = ''
    }
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
        'aui-tool-mention-autocomplete border-ring bg-background absolute bottom-full z-50 max-h-[220px] overflow-clip overflow-y-auto overscroll-contain rounded-br-none! rounded-bl-none! border border-b-0 shadow-xs',
        r('xl'),
        className
      )}
    >
      <div className="flex flex-col gap-1">
        {filteredTools.map((tool, index) => (
          <button
            key={tool.id}
            type="button"
            data-index={index}
            className={cn(
              'aui-tool-mention-item flex w-full items-center gap-2 text-left transition-colors',
              d('p-sm'),
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
