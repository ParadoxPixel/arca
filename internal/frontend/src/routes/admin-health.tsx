import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ErrorBlock, LoadingBlock, PageHeading } from '@/components/ui/feedback'
import {
  BarList,
  ChartLegend,
  StatTile,
  TimeSeriesChart,
  formatCount,
  type Series,
} from '@/components/ui/chart'
import { api, type HealthReport, type TrafficRange, type TrafficReport } from '@/lib/api'
import { packageLabel } from '@/lib/coordinates'
import { cn, formatBytes, formatRelative } from '@/lib/utils'
import { useSession } from '@/lib/session'

const ranges: Array<{ value: TrafficRange; label: string }> = [
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
  { value: '90d', label: 'Last 90 days' },
]

const hourly = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' })
const weekdayHour = new Intl.DateTimeFormat(undefined, { weekday: 'short', hour: 'numeric' })
const daily = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' })
const full = new Intl.DateTimeFormat(undefined, {
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

const day = 24 * 60 * 60 * 1000

function percentChange(current: number, previous: number): number | null {
  if (previous === 0) return null
  return ((current - previous) / previous) * 100
}

function formatUptime(milliseconds: number): string {
  const minutes = Math.floor(milliseconds / 60000)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours}h ${minutes % 60}m`
  return `${Math.floor(hours / 24)}d ${hours % 24}h`
}

function RangeFilter({
  value,
  onChange,
}: {
  value: TrafficRange
  onChange: (range: TrafficRange) => void
}) {
  return (
    <div className="inline-flex items-center gap-1 rounded-full bg-muted p-1">
      {ranges.map((range) => (
        <button
          key={range.value}
          type="button"
          onClick={() => onChange(range.value)}
          className={cn(
            'rounded-full px-3.5 py-1.5 text-[13px] font-medium transition-colors',
            range.value === value
              ? 'bg-card text-foreground shadow-card'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {range.label}
        </button>
      ))}
    </div>
  )
}

function TrafficPanel({ range }: { range: TrafficRange }) {
  const { data, error, isPending, isFetching } = useQuery({
    queryKey: ['traffic', range],
    queryFn: () => api<TrafficReport>(`/admin/traffic?range=${range}`),
    placeholderData: keepPreviousData,
  })

  if (isPending) return <LoadingBlock label="Loading traffic" />
  if (error) return <ErrorBlock message={(error as Error).message} />

  const series: Series[] = [
    {
      key: 'downloads',
      label: 'Downloads',
      color: 'var(--chart-downloads)',
      values: data.buckets.map((bucket) => bucket.downloads),
    },
    {
      key: 'visits',
      label: 'Visits',
      color: 'var(--chart-visits)',
      values: data.buckets.map((bucket) => bucket.visits),
    },
    {
      key: 'uploads',
      label: 'Uploads',
      color: 'var(--chart-uploads)',
      values: data.buckets.map((bucket) => bucket.uploads),
    },
  ]

  const timestamps = data.buckets.map((bucket) => bucket.at)
  const axisFormat =
    data.interval >= day ? daily : data.until - data.since > day ? weekdayHour : hourly
  const labelFor = (timestamp: number) => axisFormat.format(new Date(timestamp))

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile
          label="Downloads"
          value={formatCount(data.totals.downloads)}
          delta={percentChange(data.totals.downloads, data.previous.downloads)}
        />
        <StatTile
          label="Visits"
          value={formatCount(data.totals.visits)}
          delta={percentChange(data.totals.visits, data.previous.visits)}
        />
        <StatTile
          label="Uploads"
          value={formatCount(data.totals.uploads)}
          delta={percentChange(data.totals.uploads, data.previous.uploads)}
        />
        <StatTile
          label="Transferred"
          value={formatBytes(data.totals.bytes)}
          caption="Artifact bytes in and out"
        />
      </div>

      <Card>
        <CardHeader className="flex-row items-center justify-between gap-4">
          <CardTitle>Traffic</CardTitle>
          <ChartLegend series={series} />
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="chart">
            <TabsList className="mb-4">
              <TabsTrigger value="chart">Chart</TabsTrigger>
              <TabsTrigger value="table">Table</TabsTrigger>
            </TabsList>
            <TabsContent value="chart" className="mt-0">
              <TimeSeriesChart
                timestamps={timestamps}
                series={series}
                labelFor={labelFor}
                tooltipLabelFor={(timestamp) => full.format(new Date(timestamp))}
                detailFor={(index) => {
                  const transferred = data.buckets[index]?.bytes ?? 0
                  return transferred > 0 ? `${formatBytes(transferred)} transferred` : null
                }}
                dimmed={isFetching}
              />
            </TabsContent>
            <TabsContent value="table" className="mt-0">
              <div className="max-h-64 overflow-y-auto scrollbar-thin rounded-xl bg-card">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Period</TableHead>
                      <TableHead className="w-28">Downloads</TableHead>
                      <TableHead className="w-24">Visits</TableHead>
                      <TableHead className="w-24">Uploads</TableHead>
                      <TableHead className="w-28">Transferred</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.buckets.map((bucket) => (
                      <TableRow key={bucket.at}>
                        <TableCell className="text-[13px]">
                          {full.format(new Date(bucket.at))}
                        </TableCell>
                        <TableCell className="font-mono text-xs tabular-nums">
                          {bucket.downloads}
                        </TableCell>
                        <TableCell className="font-mono text-xs tabular-nums">
                          {bucket.visits}
                        </TableCell>
                        <TableCell className="font-mono text-xs tabular-nums">
                          {bucket.uploads}
                        </TableCell>
                        <TableCell className="font-mono text-xs tabular-nums text-muted-foreground">
                          {formatBytes(bucket.bytes)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Most pulled artifacts</CardTitle>
          </CardHeader>
          <CardContent>
            <BarList
              emptyLabel="No downloads in this period"
              data={data.topArtifacts.map((artifact) => ({
                id: `${artifact.repository}:${artifact.namespace}:${artifact.name}`,
                label: packageLabel(artifact.format, artifact.namespace, artifact.name),
                value: artifact.downloads,
                caption: `${artifact.repository}, ${formatBytes(artifact.bytes)}`,
                href: `/artifacts/${artifact.repository}?namespace=${encodeURIComponent(artifact.namespace)}&name=${encodeURIComponent(artifact.name)}`,
              }))}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Traffic by repository</CardTitle>
          </CardHeader>
          <CardContent>
            <BarList
              emptyLabel="No repository traffic in this period"
              data={data.repositories.map((entry) => ({
                id: entry.repository,
                label: entry.repository,
                value: entry.downloads,
                caption: `${entry.uploads} up, ${formatBytes(entry.bytes)}`,
                href: `/repositories/${entry.repository}`,
              }))}
            />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function SystemPanel() {
  const { data, error, isPending } = useQuery({
    queryKey: ['health'],
    queryFn: () => api<HealthReport>('/admin/health'),
    refetchInterval: 30_000,
  })

  if (isPending) return <LoadingBlock label="Loading instance health" />
  if (error) return <ErrorBlock message={(error as Error).message} />

  const facts = [
    { label: 'Uptime', value: formatUptime(data.uptime), caption: formatRelative(data.startedAt) },
    { label: 'Artifacts', value: formatCount(data.totals.components), caption: `${formatCount(data.totals.assets)} files` },
    { label: 'Stored', value: formatBytes(data.totals.bytes), caption: `${formatBytes(data.disk.databaseBytes)} database` },
    { label: 'Users', value: formatCount(data.users), caption: `${data.totals.repositories} repositories` },
    { label: 'Downloads today', value: formatCount(data.lastDay.downloads), caption: `${formatCount(data.lastDay.visits)} visits` },
    { label: 'Runtime', value: data.goVersion.replace('go', ''), caption: `${data.goroutines} goroutines, ${formatBytes(data.heapBytes)} heap` },
  ]

  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
      {facts.map((fact) => (
        <StatTile key={fact.label} label={fact.label} value={fact.value} caption={fact.caption} />
      ))}
    </div>
  )
}

export function AdminHealthRoute() {
  const { user, instanceName } = useSession()
  const [range, setRange] = useState<TrafficRange>('7d')

  if (user?.role !== 'admin') return <ErrorBlock message="Administrator access required" />

  return (
    <>
      <PageHeading
        eyebrow="Manage"
        title="Health"
        description={`Traffic, storage and runtime for ${instanceName}.`}
        actions={<RangeFilter value={range} onChange={setRange} />}
      />

      <SystemPanel />
      <TrafficPanel range={range} />
    </>
  )
}
