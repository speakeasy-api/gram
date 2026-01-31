import type { FC } from 'react'
import {
  ThreadListItemPrimitive,
  ThreadListPrimitive,
  useAssistantState,
} from '@assistant-ui/react'
import { PlusIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { useDensity } from '@/hooks/useDensity'

interface ThreadListProps {
  className?: string
}

export const ThreadList: FC<ThreadListProps> = ({ className }) => {
  const d = useDensity()
  return (
    <ThreadListPrimitive.Root
      className={cn(
        'aui-root aui-thread-list-root bg-background text-foreground flex flex-col items-stretch',
        d('gap-sm'),
        className
      )}
    >
      <div
        className={cn(
          'aui-thread-list-new-section border-border border-b pb-2',
          d('py-sm'),
          d('px-sm')
        )}
      >
        <ThreadListNew />
      </div>
      <div
        className={cn(
          'aui-thread-list-items-section flex flex-col gap-1',
          d('py-xs'),
          d('px-sm')
        )}
      >
        <ThreadListItems />
      </div>
    </ThreadListPrimitive.Root>
  )
}

const ThreadListNew: FC = () => {
  const d = useDensity()
  return (
    <ThreadListPrimitive.New asChild>
      <Button
        className={cn(
          'aui-thread-list-new text-foreground hover:bg-muted data-[active=true]:bg-muted/80 flex w-full cursor-pointer items-center justify-start gap-1 rounded-lg px-2.5 py-2 text-start',
          d('p-sm'),
          d('py-xs')
        )}
        variant="ghost"
      >
        <PlusIcon />
        New Thread
      </Button>
    </ThreadListPrimitive.New>
  )
}

const ThreadListItems: FC = () => {
  const isLoading = useAssistantState(({ threads }) => threads.isLoading)

  if (isLoading) {
    return <ThreadListSkeleton />
  }

  return <ThreadListPrimitive.Items components={{ ThreadListItem }} />
}

const ThreadListSkeleton: FC = () => {
  return (
    <>
      {Array.from({ length: 5 }, (_, i) => (
        <div
          key={i}
          role="status"
          aria-label="Loading threads"
          aria-live="polite"
          className="aui-thread-list-skeleton-wrapper flex items-center gap-2 rounded-md px-3 py-2"
        >
          <Skeleton className="aui-thread-list-skeleton h-[22px] grow" />
        </div>
      ))}
    </>
  )
}

const ThreadListItem: FC = () => {
  const r = useRadius()
  const d = useDensity()
  return (
    <ThreadListItemPrimitive.Root
      className={cn(
        'aui-thread-list-item group hover:bg-muted focus-visible:bg-muted focus-visible:ring-ring data-[active=true]:bg-muted flex items-center gap-2 rounded-lg transition-all focus-visible:ring-2 focus-visible:outline-none',
        r('md')
      )}
    >
      <ThreadListItemPrimitive.Trigger
        className={cn(
          'aui-thread-list-item-trigger flex grow cursor-pointer items-center text-start',
          d('px-lg'),
          d('py-sm')
        )}
      >
        <ThreadListItemTitle />
      </ThreadListItemPrimitive.Trigger>
      {/* Archive button hidden until feature is implemented */}
      {/* <ThreadListItemArchive /> */}
    </ThreadListItemPrimitive.Root>
  )
}

const ThreadListItemTitle: FC = () => {
  return (
    <span className="aui-thread-list-item-title text-foreground text-sm">
      <ThreadListItemPrimitive.Title fallback="New Chat" />
    </span>
  )
}
