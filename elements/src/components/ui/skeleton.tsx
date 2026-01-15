import { cn } from '@/lib/utils'

function Skeleton({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="skeleton"
      className={cn('gramel:bg-accent gramel:animate-pulse gramel:rounded-md', className)}
      {...props}
    />
  )
}

export { Skeleton }
