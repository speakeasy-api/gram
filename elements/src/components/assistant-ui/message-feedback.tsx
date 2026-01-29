import { X, Heart } from 'lucide-react'
import * as m from 'motion/react-m'
import { useState, type FC } from 'react'
import { AnimatePresence } from 'motion/react'

import { cn } from '@/lib/utils'
import { EASE_OUT_QUINT } from '@/lib/easing'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

export type FeedbackType = 'dislike' | 'like'

interface MessageFeedbackProps {
  onFeedback?: (type: FeedbackType) => void
  onResolved?: () => void
  className?: string
}

const feedbackButtons = [
  {
    type: 'like' as const,
    icon: Heart,
    label: 'This resolved my question',
    color: 'text-emerald-500',
    hoverBg: 'hover:bg-emerald-500/10',
    activeBg: 'bg-emerald-500/20',
  },
  {
    type: 'dislike' as const,
    icon: X,
    label: "This didn't help",
    color: 'text-rose-500',
    hoverBg: 'hover:bg-rose-500/10',
    activeBg: 'bg-rose-500/20',
  },
]

const subtleBounceKeyframes = `
@keyframes subtle-bounce {
  0%, 100% { transform: translateY(0); }
  50% { transform: translateY(-2px); }
}
`

export const MessageFeedback: FC<MessageFeedbackProps> = ({
  onFeedback,
  onResolved,
  className,
}) => {
  const [selectedFeedback, setSelectedFeedback] = useState<FeedbackType | null>(
    null
  )
  const [showDislikeFeedback, setShowDislikeFeedback] = useState(false)

  const handleFeedback = (type: FeedbackType) => {
    setSelectedFeedback(type)
    onFeedback?.(type)
    if (type === 'like') {
      onResolved?.()
    } else {
      setShowDislikeFeedback(true)
    }
  }

  return (
    <div
      className={cn(
        'aui-message-feedback flex items-center justify-center',
        className
      )}
    >
      <style>{subtleBounceKeyframes}</style>
      <AnimatePresence mode="wait">
        {!showDislikeFeedback ? (
          <m.div
            key="feedback-buttons"
            className="relative flex items-center gap-1.5 rounded-full border border-black/[0.08] bg-gradient-to-b from-white/80 to-white/60 px-3 py-1 shadow-lg backdrop-blur-2xl dark:border-white/[0.08] dark:from-white/15 dark:to-white/10"
            initial={{ opacity: 0, scale: 0.8, y: 10 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.8, y: 5 }}
            transition={{ duration: 0.4, delay: 0.75, ease: EASE_OUT_QUINT }}
            style={{
              boxShadow:
                '0 4px 24px rgba(0, 0, 0, 0.06), 0 1px 2px rgba(0, 0, 0, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.6), inset 0 -1px 0 rgba(0, 0, 0, 0.02)',
            }}
          >
            <TooltipProvider delayDuration={300}>
              {feedbackButtons.map((button, index) => {
                const Icon = button.icon
                return (
                  <Tooltip key={button.type}>
                    <TooltipTrigger asChild>
                      <m.button
                        type="button"
                        onClick={() => handleFeedback(button.type)}
                        className={cn(
                          'group/btn relative flex size-8 items-center justify-center rounded-full transition-[background-color] duration-200 ease-out',
                          button.hoverBg,
                          selectedFeedback === button.type && button.activeBg
                        )}
                        initial="initial"
                        animate="animate"
                        whileHover="hover"
                        whileTap={{ scale: 1.3 }}
                        variants={{
                          initial: { opacity: 0, scale: 0, rotate: -180 },
                          animate: { opacity: 1, scale: 1, rotate: 0 },
                          hover: { scale: 1.2 },
                        }}
                        transition={{
                          duration: 0.8,
                          delay: 0.75 + index * 0.15,
                          type: 'spring',
                          stiffness: 150,
                          damping: 10,
                        }}
                        aria-label={button.label}
                      >
                        <span
                          className="flex"
                          style={{
                            animation: 'none',
                          }}
                          onMouseEnter={(e) => {
                            e.currentTarget.style.animation =
                              'subtle-bounce 0.6s ease-in-out infinite'
                          }}
                          onMouseLeave={(e) => {
                            e.currentTarget.style.animation = 'none'
                          }}
                        >
                          <Icon
                            className={cn(
                              'size-5 transition-[fill] duration-200',
                              button.color,
                              button.type === 'like' &&
                                'group-hover/btn:fill-emerald-500',
                              selectedFeedback === button.type &&
                                button.type === 'like' &&
                                'fill-emerald-500'
                            )}
                            strokeWidth={2}
                          />
                        </span>
                      </m.button>
                    </TooltipTrigger>
                    <TooltipContent side="top" sideOffset={8}>
                      {button.label}
                    </TooltipContent>
                  </Tooltip>
                )
              })}
            </TooltipProvider>
          </m.div>
        ) : (
          <m.div
            key="thank-you"
            className="rounded-full border border-black/[0.08] bg-gradient-to-b from-white/80 to-white/60 px-4 py-2 text-sm text-muted-foreground shadow-lg backdrop-blur-2xl dark:border-white/[0.08] dark:from-white/15 dark:to-white/10"
            initial={{ opacity: 0, y: 5 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: EASE_OUT_QUINT }}
            style={{
              boxShadow:
                '0 4px 24px rgba(0, 0, 0, 0.06), 0 1px 2px rgba(0, 0, 0, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.6)',
            }}
          >
            Feedback received, thank you
          </m.div>
        )}
      </AnimatePresence>
    </div>
  )
}
