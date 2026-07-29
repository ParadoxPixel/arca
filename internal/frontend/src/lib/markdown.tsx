import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

const inlineToken = /(\*\*[^*\n]+\*\*)|(\*[^*\n]+\*)|(`[^`\n]+`)|(\[[^\]\n]+\]\([^)\s]+\))/g
const link = /^\[([^\]]+)\]\(([^)\s]+)\)$/
const heading = /^(#{1,3})\s+(.+)$/
const bullet = /^\s*[-*]\s+(.+)$/
const ordered = /^\s*\d+[.)]\s+(.+)$/
const quote = /^>\s?(.*)$/
const rule = /^(-{3,}|\*{3,}|_{3,})\s*$/

// Only targets the browser can safely follow from a page anyone may read. An
// unrecognised scheme is left as literal text rather than silently dropped.
function target(value: string): string | null {
  return /^(https?:\/\/|mailto:|\/)/i.test(value) ? value : null
}

function inline(text: string, key: string): ReactNode[] {
  const nodes: ReactNode[] = []
  let cursor = 0
  let match: RegExpExecArray | null

  inlineToken.lastIndex = 0
  while ((match = inlineToken.exec(text)) !== null) {
    if (match.index > cursor) nodes.push(text.slice(cursor, match.index))

    const token = match[0]
    const id = `${key}-${match.index}`

    if (token.startsWith('**')) {
      nodes.push(<strong key={id}>{token.slice(2, -2)}</strong>)
    } else if (token.startsWith('`')) {
      nodes.push(<code key={id}>{token.slice(1, -1)}</code>)
    } else if (token.startsWith('*')) {
      nodes.push(<em key={id}>{token.slice(1, -1)}</em>)
    } else {
      const parts = link.exec(token)
      const url = parts ? target(parts[2]!) : null
      nodes.push(
        url ? (
          <a key={id} href={url} rel="noreferrer noopener">
            {parts![1]}
          </a>
        ) : (
          token
        ),
      )
    }

    cursor = match.index + token.length
  }

  if (cursor < text.length) nodes.push(text.slice(cursor))
  return nodes
}

function blocks(source: string): ReactNode[] {
  const lines = source.replace(/\r\n/g, '\n').split('\n')
  const nodes: ReactNode[] = []
  let paragraph: string[] = []

  const flush = () => {
    if (paragraph.length === 0) return
    const key = `p-${nodes.length}`
    nodes.push(<p key={key}>{inline(paragraph.join(' '), key)}</p>)
    paragraph = []
  }

  const gather = (start: number, pattern: RegExp): [string[], number] => {
    const items: string[] = []
    let index = start
    for (; index < lines.length; index++) {
      const item = pattern.exec(lines[index]!)
      if (!item) break
      items.push(item[1]!)
    }
    return [items, index - 1]
  }

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index]!
    const key = `b-${nodes.length}`

    if (!line.trim()) {
      flush()
      continue
    }

    if (rule.test(line)) {
      flush()
      nodes.push(<hr key={key} />)
      continue
    }

    const title = heading.exec(line)
    if (title) {
      flush()
      const Tag = (['h1', 'h2', 'h3'] as const)[title[1]!.length - 1]!
      nodes.push(<Tag key={key}>{inline(title[2]!, key)}</Tag>)
      continue
    }

    if (bullet.test(line) || ordered.test(line)) {
      flush()
      const isOrdered = !bullet.test(line)
      const [items, last] = gather(index, isOrdered ? ordered : bullet)
      const List = isOrdered ? 'ol' : 'ul'
      nodes.push(
        <List key={key}>
          {items.map((item, position) => (
            <li key={`${key}-${position}`}>{inline(item, `${key}-${position}`)}</li>
          ))}
        </List>,
      )
      index = last
      continue
    }

    if (quote.test(line)) {
      flush()
      const [items, last] = gather(index, quote)
      nodes.push(<blockquote key={key}>{inline(items.join(' '), key)}</blockquote>)
      index = last
      continue
    }

    paragraph.push(line.trim())
  }

  flush()
  return nodes
}

export function Markdown({ source, className }: { source: string; className?: string }) {
  return <div className={cn('cn-prose', className)}>{blocks(source)}</div>
}
