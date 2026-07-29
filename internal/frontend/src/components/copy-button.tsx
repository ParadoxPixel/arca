import { useEffect, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import type { RepositoryFormat } from '@/lib/api'
import { cn } from '@/lib/utils'

function useCopied(): [boolean, (value: string) => void] {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = window.setTimeout(() => setCopied(false), 1600)
    return () => window.clearTimeout(timer)
  }, [copied])

  return [
    copied,
    (value: string) => void navigator.clipboard.writeText(value).then(() => setCopied(true)),
  ]
}

export function CopyButton({ value, className }: { value: string; className?: string }) {
  const [copied, copy] = useCopied()

  return (
    <button
      type="button"
      onClick={() => copy(value)}
      aria-label={copied ? 'Copied' : 'Copy to clipboard'}
      className={cn(
        'inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-foreground',
        className,
      )}
    >
      {copied ? <Check className="size-3.5 text-release" /> : <Copy className="size-3.5" />}
    </button>
  )
}

export function CodeBlock({ code, language }: { code: string; language: string }) {
  const [copied, copy] = useCopied()

  return (
    <div className="group relative overflow-hidden rounded-2xl bg-muted">
      <div className="flex items-center justify-between gap-3 px-5 pt-4">
        <span className="truncate text-xs font-medium text-muted-foreground">{language}</span>
        <button
          type="button"
          onClick={() => copy(code)}
          className="shrink-0 rounded-full px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-card hover:text-foreground"
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre className="whitespace-pre-wrap break-words px-5 pb-5 pt-3 font-mono text-xs leading-relaxed text-foreground">
        <code>{code}</code>
      </pre>
    </div>
  )
}

// StackedCoordinate is the two-line form the admin tables use, where the column
// is too narrow to hold a scope and a name side by side.
export function StackedCoordinate({
  format,
  namespace,
  name,
}: {
  format: RepositoryFormat
  namespace: string
  name: string
}) {
  const scope = format === 'npm' ? (namespace ? `@${namespace}` : '') : namespace

  return (
    <>
      {scope ? (
        <span className="block truncate font-mono text-xs text-muted-foreground">{scope}</span>
      ) : null}
      <span className="block truncate font-mono text-[13px]">{name}</span>
    </>
  )
}

export function Coordinate({
  format,
  namespace,
  name,
  version,
  className,
}: {
  format: RepositoryFormat
  namespace: string
  name: string
  version?: string | null
  className?: string
}) {
  const npm = format === 'npm'

  return (
    <span className={cn('font-mono', className)}>
      {npm ? (
        namespace ? (
          <span className="text-muted-foreground">@{namespace}/</span>
        ) : null
      ) : (
        <>
          <span className="text-muted-foreground">{namespace}</span>
          <span className="text-faint">:</span>
        </>
      )}
      <span className="font-medium text-foreground">{name}</span>
      {version ? (
        <>
          <span className="text-faint">{npm ? '@' : ':'}</span>
          <span className="text-primary">{version}</span>
        </>
      ) : null}
    </span>
  )
}
