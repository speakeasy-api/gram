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
      className={cn('aui-root aui-thread-list-root gramel:bg-background gramel:flex gramel:flex-col gramel:items-stretch',
        d('gramel:gap-sm'),
        className
      )}
    >
      <div
        className={cn('aui-thread-list-new-section gramel:border-b gramel:pb-2',
          d('gramel:py-sm'),
          d('gramel:px-sm')
        )}
      >
        <ThreadListNew />
      </div>
      <div
        className={cn('aui-thread-list-items-section gramel:flex gramel:flex-col gramel:gap-1',
          d('gramel:py-xs'),
          d('gramel:px-sm')
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
        className={cn('aui-thread-list-new gramel:hover:bg-muted-foreground/10 gramel:data-[active=true]:bg-muted-foreground/20! gramel:flex gramel:w-full gramel:cursor-pointer gramel:items-center gramel:justify-start gramel:gap-1 gramel:rounded-lg gramel:px-2.5 gramel:py-2 gramel:text-start',
          d('gramel:p-sm'),
          d('gramel:py-xs')
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
          className="aui-thread-list-skeleton-wrapper gramel:flex gramel:items-center gramel:gap-2 gramel:rounded-md gramel:px-3 gramel:py-2"
        >
          <Skeleton className="aui-thread-list-skeleton gramel:h-[22px] gramel:grow" />
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
      className={cn('aui-thread-list-item gramel:group gramel:hover:bg-muted gramel:focus-visible:bg-muted gramel:focus-visible:ring-ring gramel:data-[active=true]:bg-muted-foreground/20 gramel:flex gramel:items-center gramel:gap-2 gramel:rounded-lg gramel:transition-all gramel:focus-visible:ring-2 gramel:focus-visible:outline-none',
        r('md')
      )}
    >
      <ThreadListItemPrimitive.Trigger
        className={cn('aui-thread-list-item-trigger gramel:flex gramel:grow gramel:cursor-pointer gramel:items-center gramel:text-start',
          d('gramel:px-lg'),
          d('gramel:py-sm')
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
    <span className="aui-thread-list-item-title gramel:text-sm">
      <ThreadListItemPrimitive.Title fallback="New Chat" />
    </span>
  )
}
