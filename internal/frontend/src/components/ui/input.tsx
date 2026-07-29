import * as React from 'react'
import * as LabelPrimitive from '@radix-ui/react-label'
import { cn } from '@/lib/utils'

const field =
  'flex w-full rounded-xl border border-border bg-card px-3.5 text-sm text-foreground transition-colors placeholder:text-faint hover:border-faint/60 focus-visible:border-primary focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-40'

export function Input({ className, ...props }: React.ComponentProps<'input'>) {
  return <input className={cn(field, 'h-10', className)} {...props} />
}

export function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  return <textarea className={cn(field, 'min-h-24 py-2.5', className)} {...props} />
}

export function Select({ className, ...props }: React.ComponentProps<'select'>) {
  return <select className={cn(field, 'h-10 pr-9', className)} {...props} />
}

export function Label({ className, ...props }: React.ComponentProps<typeof LabelPrimitive.Root>) {
  return (
    <LabelPrimitive.Root
      className={cn('block text-[13px] font-medium text-foreground', className)}
      {...props}
    />
  )
}

export function Checkbox({ className, ...props }: React.ComponentProps<'input'>) {
  return (
    <input
      type="checkbox"
      className={cn('cn-checkbox', className)}
      {...props}
    />
  )
}

export function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  const id = React.useId()
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      {React.isValidElement(children)
        ? React.cloneElement(children as React.ReactElement<{ id?: string }>, { id })
        : children}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}
