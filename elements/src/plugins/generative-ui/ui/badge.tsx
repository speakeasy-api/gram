import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { Slot } from 'radix-ui'

import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex items-center justify-center rounded-full border border-transparent px-2 py-0.5 text-xs font-medium w-fit whitespace-nowrap shrink-0 [&>svg]:size-3 gap-1 [&>svg]:pointer-events-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive transition-[color,box-shadow] overflow-hidden',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground [a&]:hover:bg-primary/90',
        secondary:
          'bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90',
        /** Matches LLM prompt variant */
        success:
          'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100',
        /** Matches LLM prompt variant */
        warning:
          'bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-100',
        /** Matches LLM prompt variant */
        error: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-100',
        destructive:
          'bg-destructive text-white [a&]:hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 dark:bg-destructive/60',
        outline:
          'border-border text-foreground [a&]:hover:bg-accent [a&]:hover:text-accent-foreground',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  }
)

interface BadgeProps
  extends React.ComponentProps<'span'>, VariantProps<typeof badgeVariants> {
  asChild?: boolean
  /** Content text (matches LLM prompt) - rendered as children */
  content?: string
}

function Badge({
  className,
  variant = 'default',
  asChild = false,
  content,
  children,
  ...props
}: BadgeProps) {
  const Comp = asChild ? Slot.Root : 'span'

  return (
    <Comp
      data-slot="badge"
      data-variant={variant}
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    >
      {content ?? children}
    </Comp>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export { Badge, badgeVariants }
