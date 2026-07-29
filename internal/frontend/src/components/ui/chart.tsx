import { useLayoutEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'

export interface Series {
  key: string
  label: string
  color: string
  values: number[]
}

const compact = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 })
const exact = new Intl.NumberFormat()

export function formatCount(value: number): string {
  return value < 10_000 ? exact.format(value) : compact.format(value)
}

function niceCeiling(value: number, ticks: number): number {
  if (value <= 0) return ticks
  const rough = value / ticks
  const magnitude = 10 ** Math.floor(Math.log10(rough))
  const step = magnitude * ([1, 2, 2.5, 5, 10].find((factor) => factor * magnitude >= rough) ?? 10)
  return step * ticks
}

function useMeasuredWidth() {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)

  useLayoutEffect(() => {
    const element = ref.current
    if (!element) return

    const observer = new ResizeObserver((entries) => setWidth(entries[0]?.contentRect.width ?? 0))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  return [ref, width] as const
}

const plot = { top: 12, right: 14, bottom: 26, left: 46, height: 210 }
const tickCount = 4

interface TimeSeriesProps {
  timestamps: number[]
  series: Series[]
  labelFor: (timestamp: number) => string
  tooltipLabelFor?: (timestamp: number) => string
  detailFor?: (index: number) => string | null
  dimmed?: boolean
}

export function TimeSeriesChart({
  timestamps,
  series,
  labelFor,
  tooltipLabelFor = labelFor,
  detailFor,
  dimmed,
}: TimeSeriesProps) {
  const [container, width] = useMeasuredWidth()
  const [active, setActive] = useState<number | null>(null)

  const height = plot.height
  const innerWidth = Math.max(width - plot.left - plot.right, 1)
  const innerHeight = height - plot.top - plot.bottom
  const peak = Math.max(...series.flatMap((entry) => entry.values), 0)
  const ceiling = niceCeiling(peak, tickCount)

  const x = (index: number) =>
    plot.left + (timestamps.length < 2 ? innerWidth / 2 : (index * innerWidth) / (timestamps.length - 1))
  const y = (value: number) => plot.top + innerHeight - (value / ceiling) * innerHeight

  const linePath = (values: number[]) =>
    values.map((value, index) => `${index === 0 ? 'M' : 'L'}${x(index)},${y(value)}`).join(' ')

  const areaPath = (values: number[]) =>
    `${linePath(values)} L${x(values.length - 1)},${plot.top + innerHeight} L${x(0)},${plot.top + innerHeight} Z`

  const ticks = Array.from({ length: tickCount + 1 }, (_, index) => (ceiling / tickCount) * index)
  const labelStride = Math.max(1, Math.ceil(timestamps.length / Math.max(2, Math.floor(width / 110))))

  const track = (event: React.PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    const offset = event.clientX - bounds.left - plot.left
    const step = timestamps.length < 2 ? innerWidth : innerWidth / (timestamps.length - 1)
    setActive(Math.min(timestamps.length - 1, Math.max(0, Math.round(offset / step))))
  }

  return (
    <div ref={container} className="relative">
      {width > 0 ? (
        <svg
          width={width}
          height={height}
          role="img"
          aria-label="Traffic over time"
          className={cn('touch-none transition-opacity', dimmed && 'opacity-50')}
          onPointerMove={track}
          onPointerLeave={() => setActive(null)}
        >
          {ticks.map((tick) => (
            <g key={tick}>
              <line
                x1={plot.left}
                x2={plot.left + innerWidth}
                y1={y(tick)}
                y2={y(tick)}
                className="stroke-chart-grid"
                strokeWidth={1}
              />
              <text
                x={plot.left - 10}
                y={y(tick) + 4}
                textAnchor="end"
                className="fill-faint text-[10px] tabular-nums"
              >
                {formatCount(tick)}
              </text>
            </g>
          ))}

          {timestamps.map((timestamp, index) =>
            index % labelStride === 0 ? (
              <text
                key={timestamp}
                x={x(index)}
                y={height - 8}
                textAnchor={index === 0 ? 'start' : 'middle'}
                className="fill-faint text-[10px]"
              >
                {labelFor(timestamp)}
              </text>
            ) : null,
          )}

          {series.map((entry) => (
            <g key={entry.key}>
              <path d={areaPath(entry.values)} fill={entry.color} fillOpacity={0.1} />
              <path
                d={linePath(entry.values)}
                fill="none"
                stroke={entry.color}
                strokeWidth={2}
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </g>
          ))}

          {active !== null ? (
            <g>
              <line
                x1={x(active)}
                x2={x(active)}
                y1={plot.top}
                y2={plot.top + innerHeight}
                className="stroke-faint"
                strokeWidth={1}
              />
              {series.map((entry) => (
                <circle
                  key={entry.key}
                  cx={x(active)}
                  cy={y(entry.values[active] ?? 0)}
                  r={4}
                  fill={entry.color}
                  className="stroke-card"
                  strokeWidth={2}
                />
              ))}
            </g>
          ) : null}
        </svg>
      ) : (
        <div style={{ height }} />
      )}

      {active !== null ? (
        <div
          className="pointer-events-none absolute top-2 z-10 min-w-44 rounded-xl bg-card p-3 shadow-float ring-1 ring-border"
          style={{
            left: active * 2 < timestamps.length ? Math.max(width - 190, 0) : 0,
          }}
        >
          <p className="text-[11px] text-muted-foreground">
            {tooltipLabelFor(timestamps[active] ?? 0)}
          </p>
          <div className="mt-2 space-y-1.5">
            {series.map((entry) => (
              <div key={entry.key} className="flex items-center gap-2">
                <span
                  aria-hidden
                  className="h-0.5 w-3 shrink-0 rounded-full"
                  style={{ backgroundColor: entry.color }}
                />
                <span className="font-mono text-[13px] font-medium tabular-nums">
                  {exact.format(entry.values[active] ?? 0)}
                </span>
                <span className="text-[11px] text-muted-foreground">{entry.label}</span>
              </div>
            ))}
          </div>
          {detailFor?.(active) ? (
            <p className="mt-2 border-t border-border pt-2 text-[11px] text-muted-foreground">
              {detailFor(active)}
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

export function ChartLegend({ series }: { series: Series[] }) {
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
      {series.map((entry) => (
        <div key={entry.key} className="flex items-center gap-2">
          <span
            aria-hidden
            className="h-0.5 w-4 rounded-full"
            style={{ backgroundColor: entry.color }}
          />
          <span className="text-xs text-muted-foreground">{entry.label}</span>
        </div>
      ))}
    </div>
  )
}

export interface BarDatum {
  id: string
  label: string
  value: number
  caption?: string
  href?: string
}

export function BarList({
  data,
  format = formatCount,
  emptyLabel = 'Nothing recorded yet',
}: {
  data: BarDatum[]
  format?: (value: number) => string
  emptyLabel?: string
}) {
  const peak = Math.max(...data.map((entry) => entry.value), 1)

  if (data.length === 0) {
    return <p className="py-8 text-center text-sm text-muted-foreground">{emptyLabel}</p>
  }

  return (
    <div className="space-y-2.5">
      {data.map((entry) => (
        <div key={entry.id} className="space-y-1">
          <div className="flex items-baseline justify-between gap-4">
            {entry.href ? (
              <Link to={entry.href} className="min-w-0 truncate font-mono text-[13px] hover:text-primary">
                {entry.label}
              </Link>
            ) : (
              <span className="min-w-0 truncate font-mono text-[13px]">{entry.label}</span>
            )}
            <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">
              {format(entry.value)}
            </span>
          </div>
          <div className="flex items-center gap-3">
            <div className="h-2 flex-1 overflow-hidden rounded-r-[4px] bg-accent/60">
              <div
                className="h-full rounded-r-[4px] bg-chart-downloads"
                style={{ width: `${Math.max((entry.value / peak) * 100, 1.5)}%` }}
              />
            </div>
            {entry.caption ? (
              <span className="shrink-0 text-[11px] text-faint">{entry.caption}</span>
            ) : null}
          </div>
        </div>
      ))}
    </div>
  )
}

export interface Slice {
  key: string
  label: string
  value: number
  className: string
}

export function StackedBar({ slices, format }: { slices: Slice[]; format: (value: number) => string }) {
  const total = slices.reduce((sum, slice) => sum + slice.value, 0)
  const present = slices.filter((slice) => slice.value > 0)

  return (
    <div className="space-y-3">
      <div className="flex h-3 gap-0.5 overflow-hidden rounded-full">
        {present.length === 0 ? (
          <div className="flex-1 bg-accent" />
        ) : (
          present.map((slice) => (
            <div
              key={slice.key}
              className={slice.className}
              style={{ width: `${(slice.value / total) * 100}%` }}
              title={slice.label}
            />
          ))
        )}
      </div>
      <div className="flex flex-wrap gap-x-5 gap-y-1.5">
        {slices.map((slice) => (
          <div key={slice.key} className="flex items-center gap-2">
            <span aria-hidden className={cn('size-2 rounded-full', slice.className)} />
            <span className="text-xs text-muted-foreground">{slice.label}</span>
            <span className="font-mono text-xs tabular-nums">{format(slice.value)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

export function StatTile({
  label,
  value,
  delta,
  caption,
}: {
  label: string
  value: string
  delta?: number | null
  caption?: string
}) {
  return (
    <div className="rounded-2xl bg-muted px-4 py-3.5">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className="mt-1 text-2xl font-semibold">{value}</p>
      {delta !== null && delta !== undefined ? (
        <p
          className={cn(
            'mt-1 text-xs',
            delta > 0 ? 'text-release' : delta < 0 ? 'text-destructive' : 'text-muted-foreground',
          )}
        >
          {delta > 0 ? '+' : ''}
          {Math.round(delta)}% vs previous period
        </p>
      ) : caption ? (
        <p className="mt-1 text-xs text-muted-foreground">{caption}</p>
      ) : null}
    </div>
  )
}
