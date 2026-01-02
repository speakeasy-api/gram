import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { useAssistantState } from '@assistant-ui/react'
import { CheckIcon, ChevronDownIcon, ChevronUpIcon, Loader } from 'lucide-react'
import { useMemo, useState, type FC, type PropsWithChildren } from 'react'
import { Button } from '../ui/button'
import { AnimatePresence, domAnimation, LazyMotion } from 'motion/react'
import * as m from 'motion/react-m'
import { EASE_OUT_QUINT } from '@/lib/easing'
import { useElements } from '@/hooks/useElements'
import { humanizeToolName } from '@/lib/humanize'
import { useDensity } from '@/hooks/useDensity'

export const ToolGroup: FC<
  PropsWithChildren<{ startIndex: number; endIndex: number }>
> = ({ children }) => {
  const r = useRadius()
  const parts = useAssistantState(({ message }) => message).parts
  const toolCallParts = parts.filter((part) => part.type === 'tool-call')
  const anyMessagePartsAreRunning = toolCallParts.some(
    (part) => part.status?.type === 'running'
  )
  const icon = useMemo(() => {
    if (anyMessagePartsAreRunning)
      return <Loader className="text-muted-foreground size-4 animate-spin" />
    return <CheckIcon className="size-4 text-green-500" />
  }, [anyMessagePartsAreRunning])

  const { config } = useElements()
  const [isOpen, setIsOpen] = useState(
    config.tools?.expandToolGroupsByDefault ?? false
  )

  const d = useDensity()
  const groupTitle = useMemo(() => {
    const toolParts = parts.filter((part) => part.type === 'tool-call')

    if (toolParts.length === 0) return 'No tools called'
    if (toolParts.length === 1)
      return `Calling ${humanizeToolName(toolParts[0].toolName)}...`
    return anyMessagePartsAreRunning
      ? `Calling ${toolParts.length} tools...`
      : `Executed ${toolParts.length} tools`
  }, [parts, anyMessagePartsAreRunning])

  if (config.tools?.components?.[toolCallParts[0].toolName]) {
    return children
  }

  return (
    <LazyMotion features={domAnimation}>
      {toolCallParts.length > 1 ? (
        <button
          className={cn(
            'group my-4 w-full max-w-xl cursor-pointer border',
            r('sm')
          )}
          onClick={() => setIsOpen(!isOpen)}
        >
          <div
            className={cn(
              'bg-muted/40 flex items-center rounded-b-none!',
              r('sm'),
              d('py-xs'),
              d('gap-md'),
              d('px-md'),
              isOpen && 'border-b'
            )}
          >
            <span>{icon}</span>
            <span
              className={cn(
                'font-semibold',
                anyMessagePartsAreRunning && 'shimmer'
              )}
            >
              {groupTitle}
            </span>

            <div className="ml-auto">
              <Button
                variant="ghost"
                size="icon"
                className="cursor-pointer hover:bg-transparent"
                onClick={() => setIsOpen(!isOpen)}
              >
                {isOpen ? (
                  <ChevronUpIcon className="size-4" />
                ) : (
                  <ChevronDownIcon className="size-4" />
                )}
              </Button>
            </div>
          </div>

          <AnimatePresence>
            {isOpen && (
              <m.div
                initial={{ height: 0, opacity: 0, y: -10 }}
                animate={{ height: 'auto', opacity: 1, y: 0 }}
                exit={{ height: 0, opacity: 0, y: -10 }}
                transition={{ duration: 0.3, ease: EASE_OUT_QUINT }}
                style={{ overflow: 'hidden' }}
              >
                {children}
              </m.div>
            )}
          </AnimatePresence>
        </button>
      ) : (
        <div
          className={cn(
            'bg-muted/40 my-4 flex w-full max-w-xl cursor-pointer items-center rounded-b-none! border'
          )}
        >
          {children}
        </div>
      )}
    </LazyMotion>
  )
}
