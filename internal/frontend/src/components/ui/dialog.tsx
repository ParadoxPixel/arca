import type * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

export const Dialog = DialogPrimitive.Root
export const DialogTrigger = DialogPrimitive.Trigger
export const DialogClose = DialogPrimitive.Close

export function DialogContent({
  className,
  children,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content>) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-foreground/25 backdrop-blur-[3px]" />
      <DialogPrimitive.Content
        className={cn(
          'fixed left-1/2 top-1/2 z-50 w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2',
          // A tall form has to scroll inside the dialog rather than run off the
          // viewport, which on a short screen puts the submit button out of reach.
          'flex max-h-[calc(100dvh-2rem)] flex-col',
          'overflow-hidden rounded-3xl bg-card shadow-float outline-none dark:ring-1 dark:ring-border',
          className,
        )}
        {...props}
      >
        {children}
        <DialogPrimitive.Close className="absolute right-4 top-4 inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-foreground">
          <X className="size-4" />
          <span className="sr-only">Close</span>
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  )
}

// DialogForm keeps the header, body and footer laid out by DialogContent when a
// form sits between them, which is otherwise the one flex item in the column.
export function DialogForm({ className, ...props }: React.ComponentProps<'form'>) {
  return <form className={cn('flex min-h-0 flex-1 flex-col', className)} {...props} />
}

export function DialogHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div className={cn('shrink-0 flex flex-col gap-1.5 px-6 pb-1 pt-6 pr-14', className)} {...props} />
  )
}

export function DialogBody({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      className={cn('min-h-0 flex-1 space-y-5 overflow-y-auto scrollbar-thin px-6 py-5', className)}
      {...props}
    />
  )
}

export function DialogFooter({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div className={cn('shrink-0 flex justify-end gap-2 px-6 pb-6 pt-1', className)} {...props} />
  )
}

export function DialogTitle({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title className={cn('text-lg font-semibold', className)} {...props} />
  )
}

export function DialogDescription({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      className={cn('text-sm text-muted-foreground', className)}
      {...props}
    />
  )
}
