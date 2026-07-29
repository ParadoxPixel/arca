import type * as React from 'react'
import { Loader2 } from 'lucide-react'
import { Mark } from '@/components/brand'
import { cn } from '@/lib/utils'

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cn('size-4 animate-spin text-faint', className)} />
}

export function LoadingBlock({ label = 'Loading' }: { label?: string }) {
  return (
    <div className="flex items-center justify-center gap-2.5 py-24 text-sm text-muted-foreground">
      <Spinner />
      {label}
    </div>
  )
}

export function ErrorBlock({ message }: { message: string }) {
  return (
    <div className="rounded-2xl bg-destructive-soft px-5 py-4">
      <p className="text-[13px] font-semibold text-destructive">Something went wrong</p>
      <p className="mt-1 text-sm text-destructive/90">{message}</p>
    </div>
  )
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-4 rounded-2xl border border-dashed border-border px-6 py-20 text-center">
      <Mark className="size-8 text-faint" />
      <div className="space-y-1.5">
        <p className="font-medium">{title}</p>
        {description ? (
          <p className="mx-auto max-w-md text-sm text-muted-foreground">{description}</p>
        ) : null}
      </div>
      {action}
    </div>
  )
}

export function PageHeading({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: string
  title: React.ReactNode
  description?: string
  actions?: React.ReactNode
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div className="space-y-1.5">
        {eyebrow ? <p className="text-[13px] font-medium text-primary">{eyebrow}</p> : null}
        <h1 className="text-3xl font-semibold">{title}</h1>
        {description ? (
          <p className="max-w-2xl text-[15px] text-muted-foreground">{description}</p>
        ) : null}
      </div>
      {actions}
    </div>
  )
}
