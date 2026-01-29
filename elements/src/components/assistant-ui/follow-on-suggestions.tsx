import { ThreadPrimitive } from '@assistant-ui/react'
import { AnimatePresence } from 'motion/react'
import * as m from 'motion/react-m'
import { FC } from 'react'

import { Button } from '@/components/ui/button'
import { useDensity } from '@/hooks/useDensity'
import { useFollowOnSuggestions } from '@/hooks/useFollowOnSuggestions'
import { useRadius } from '@/hooks/useRadius'
import { EASE_OUT_QUINT } from '@/lib/easing'
import { cn } from '@/lib/utils'

const suggestionVariants = {
  hidden: {
    opacity: 0,
    y: 8,
    scale: 0.95,
  },
  visible: (index: number) => ({
    opacity: 1,
    y: 0,
    scale: 1,
    transition: {
      duration: 0.3,
      delay: 0.1 * index,
      ease: EASE_OUT_QUINT,
    },
  }),
  exit: {
    opacity: 0,
    y: -4,
    scale: 0.98,
    transition: {
      duration: 0.15,
      ease: EASE_OUT_QUINT,
    },
  },
}

const containerVariants = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: {
      staggerChildren: 0.08,
      delayChildren: 0.1,
    },
  },
  exit: {
    opacity: 0,
    transition: {
      staggerChildren: 0.03,
      staggerDirection: -1,
    },
  },
}

/**
 * Displays follow-on suggestions after the assistant finishes responding.
 * These are dynamically generated based on the conversation context.
 */
export const FollowOnSuggestions: FC = () => {
  const { suggestions, isLoading } = useFollowOnSuggestions()
  const r = useRadius()
  const d = useDensity()

  const showSuggestions = !isLoading && suggestions.length > 0

  return (
    <div
      className={cn(
        'aui-follow-on-suggestions w-full',
        d('gap-sm'),
        d('py-sm'),
        d('px-md')
      )}
    >
      <AnimatePresence mode="wait">
        {showSuggestions && (
          <m.div
            key="suggestions-container"
            variants={containerVariants}
            initial="hidden"
            animate="visible"
            exit="exit"
            className="flex flex-wrap gap-2"
          >
            {suggestions.map((suggestion, index) => (
              <m.div
                key={suggestion.id}
                variants={suggestionVariants}
                custom={index}
                initial="hidden"
                animate="visible"
                exit="exit"
              >
                <ThreadPrimitive.Suggestion
                  prompt={suggestion.prompt}
                  send
                  asChild
                >
                  <Button
                    variant="outline"
                    size="sm"
                    className={cn(
                      'aui-follow-on-suggestion text-muted-foreground hover:text-foreground h-auto cursor-pointer text-left text-sm whitespace-normal transition-colors',
                      d('px-md'),
                      d('py-sm'),
                      r('lg')
                    )}
                  >
                    {suggestion.prompt}
                  </Button>
                </ThreadPrimitive.Suggestion>
              </m.div>
            ))}
          </m.div>
        )}
      </AnimatePresence>
    </div>
  )
}
