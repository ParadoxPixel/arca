import { useId } from 'react'
import { useSession } from '@/lib/session'
import { cn } from '@/lib/utils'

export function Mark({ className }: { className?: string }) {
  const mask = useId()

  return (
    <svg
      viewBox="0 0 140 140"
      role="presentation"
      className={cn('size-5 text-foreground', className)}
    >
      <mask id={mask}>
        <rect width="140" height="140" fill="#fff" />
        <rect x="80" y="80" width="40" height="40" fill="#000" />
      </mask>
      <g mask={`url(#${mask})`} fill="currentColor">
        <rect x="20" y="20" width="120" height="120" opacity=".9" />
        <polygon points="20,20 140,20 120,0 0,0" opacity=".7" />
        <polygon points="140,20 140,140 120,120 120,0" opacity=".8" />
      </g>
    </svg>
  )
}

// Logo falls back to the built in mark, so every place that brands the instance
// picks up an uploaded logo without knowing whether one exists.
export function Logo({ className }: { className?: string }) {
  const { instanceName, logoUrl } = useSession()

  if (!logoUrl) return <Mark className={className} />

  return (
    <img
      src={logoUrl}
      alt={instanceName}
      className={cn('size-5 shrink-0 object-contain', className)}
    />
  )
}

// BrandSplotch is the configured theme colour as a soft wash. Anchored on a
// corner so it bleeds past whatever contains it, which is what makes it read as
// one accent running behind the page rather than a decoration inside a box.
export function BrandSplotch({ className }: { className?: string }) {
  return (
    <div
      aria-hidden
      className={cn(
        'pointer-events-none absolute -translate-x-1/2 -translate-y-1/2 rounded-full blur-3xl',
        'h-[56rem] w-[68rem] opacity-40 dark:opacity-30',
        className,
      )}
      style={{
        background:
          'radial-gradient(closest-side, var(--brand), color-mix(in oklab, var(--brand) 40%, transparent), transparent)',
      }}
    />
  )
}
