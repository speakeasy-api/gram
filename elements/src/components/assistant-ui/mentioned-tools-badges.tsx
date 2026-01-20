import { FC } from 'react'
import { Wrench, X } from 'lucide-react'
import { AnimatePresence } from 'motion/react'
import * as m from 'motion/react-m'

import { cn } from '@/lib/utils'
import { useDensity } from '@/hooks/useDensity'
import { useRadius } from '@/hooks/useRadius'
import { EASE_OUT_QUINT } from '@/lib/easing'
import { MentionableTool } from '@/lib/tool-mentions'

export interface MentionedToolsBadgesProps {
  mentionedToolIds: string[]
  tools: MentionableTool[]
  onRemove?: (toolId: string) => void
  className?: string
}

export const MentionedToolsBadges: FC<MentionedToolsBadgesProps> = ({
  mentionedToolIds,
  tools,
  onRemove,
  className,
}) => {
  const d = useDensity()
  const mentionedTools = tools.filter((tool) =>
    mentionedToolIds.includes(tool.id)
  )

  if (mentionedTools.length === 0) {
    return null
  }

  return (
    <div
      className={cn(
        'aui-mentioned-tools-badges flex flex-wrap items-center gap-1',
        d('px-sm'),
        d('py-xs'),
        className
      )}
    >
      <span className="text-muted-foreground flex-shrink-0 text-xs">
        Tools:
      </span>
      <AnimatePresence mode="popLayout">
        {mentionedTools.map((tool) => (
          <m.div
            key={tool.id}
            layout
            initial={{ opacity: 0, scale: 0.8 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.8 }}
            transition={{ duration: 0.15, ease: EASE_OUT_QUINT }}
          >
            <ToolBadge
              tool={tool}
              onRemove={onRemove ? () => onRemove(tool.id) : undefined}
            />
          </m.div>
        ))}
      </AnimatePresence>
    </div>
  )
}

interface ToolBadgeProps {
  tool: MentionableTool
  onRemove?: () => void
}

const ToolBadge: FC<ToolBadgeProps> = ({ tool, onRemove }) => {
  const d = useDensity()
  const r = useRadius()

  return (
    <div
      className={cn(
        'aui-tool-badge bg-primary/10 text-primary inline-flex items-center gap-1',
        r('md'),
        d('px-sm'),
        d('py-xs')
      )}
    >
      <Wrench className="size-3 flex-shrink-0" />
      <span className="text-xs font-medium">{tool.name}</span>
      {onRemove && (
        <button
          type="button"
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            onRemove()
          }}
          className="hover:opacity-70 focus:outline-none"
          aria-label={`Remove ${tool.name}`}
        >
          <X className="size-3 flex-shrink-0" />
        </button>
      )}
    </div>
  )
}

export default MentionedToolsBadges
