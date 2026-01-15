import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { XIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

function Dialog({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Root>) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />
}

function DialogTrigger({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Trigger>) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />
}

function DialogPortal({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Portal>) {
  return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />
}

function DialogClose({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Close>) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />
}

function DialogOverlay({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      data-slot="dialog-overlay"
      className={cn('gramel:data-[state=open]:animate-in gramel:data-[state=closed]:animate-out gramel:data-[state=closed]:fade-out-0 gramel:data-[state=open]:fade-in-0 gramel:fixed gramel:inset-0 gramel:z-50 gramel:bg-black/50',
        className
      )}
      {...props}
    />
  )
}

function DialogContent({
  className,
  children,
  showCloseButton = true,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
  showCloseButton?: boolean
}) {
  return (
    <DialogPortal data-slot="dialog-portal">
      <DialogOverlay />
      <DialogPrimitive.Content
        data-slot="dialog-content"
        className={cn('gramel:bg-background gramel:data-[state=open]:animate-in gramel:data-[state=closed]:animate-out gramel:data-[state=closed]:fade-out-0 gramel:data-[state=open]:fade-in-0 gramel:data-[state=closed]:zoom-out-95 gramel:data-[state=open]:zoom-in-95 gramel:fixed gramel:top-[50%] gramel:left-[50%] gramel:z-50 gramel:grid gramel:w-full gramel:max-w-[calc(100%-2rem)] gramel:translate-x-[-50%] gramel:translate-y-[-50%] gramel:gap-4 gramel:rounded-lg gramel:border gramel:p-6 gramel:shadow-lg gramel:duration-200 gramel:sm:max-w-lg',
          className
        )}
        {...props}
      >
        {children}
        {showCloseButton && (
          <DialogPrimitive.Close
            data-slot="dialog-close"
            className="gramel:ring-offset-background gramel:focus:ring-ring gramel:data-[state=open]:bg-accent gramel:data-[state=open]:text-muted-foreground gramel:absolute gramel:top-4 gramel:right-4 gramel:rounded-xs gramel:opacity-70 gramel:transition-opacity gramel:hover:opacity-100 gramel:focus:ring-2 gramel:focus:ring-offset-2 gramel:focus:outline-hidden gramel:disabled:pointer-events-none gramel:[&_svg]:pointer-events-none gramel:[&_svg]:shrink-0 gramel:[&_svg:not([class*='size-'])]:size-4"
          >
            <XIcon />
            <span className="gramel:sr-only">Close</span>
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </DialogPortal>
  )
}

function DialogHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="dialog-header"
      className={cn('gramel:flex gramel:flex-col gramel:gap-2 gramel:text-center gramel:sm:text-left', className)}
      {...props}
    />
  )
}

function DialogFooter({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn('gramel:flex gramel:flex-col-reverse gramel:gap-2 gramel:sm:flex-row gramel:sm:justify-end',
        className
      )}
      {...props}
    />
  )
}

function DialogTitle({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn('gramel:text-lg gramel:leading-none gramel:font-semibold', className)}
      {...props}
    />
  )
}

function DialogDescription({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      data-slot="dialog-description"
      className={cn('gramel:text-muted-foreground gramel:text-sm', className)}
      {...props}
    />
  )
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
}
