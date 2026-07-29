import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Badge, FormatBadge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ErrorBlock, LoadingBlock, PageHeading } from '@/components/ui/feedback'
import { BarList, StackedBar, StatTile, formatCount } from '@/components/ui/chart'
import { StackedCoordinate } from '@/components/copy-button'
import { DeleteArtifactDialog } from '@/components/delete-artifact'
import { api, type StorageReport } from '@/lib/api'
import { formatBytes, formatRelative } from '@/lib/utils'
import { useSession } from '@/lib/session'

function Disk({ report }: { report: StorageReport }) {
  const { disk, totals } = report

  return (
    <Card>
      <CardHeader>
        <CardTitle>Disk</CardTitle>
        <CardDescription>
          <span className="font-mono text-xs">{disk.root}</span>, measured{' '}
          {formatRelative(disk.measuredAt)}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <StackedBar
          format={formatBytes}
          slices={[
            {
              key: 'release',
              label: 'Releases',
              value: totals.releaseBytes,
              className: 'bg-release',
            },
            {
              key: 'snapshot',
              label: 'Snapshots',
              value: totals.snapshotBytes,
              className: 'bg-snapshot',
            },
            {
              key: 'orphan',
              label: 'Untracked',
              value: disk.orphanBytes,
              className: 'bg-faint',
            },
          ]}
        />
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <StatTile label="On disk" value={formatBytes(disk.bytes)} caption={`${formatCount(disk.files)} files`} />
          <StatTile label="Tracked" value={formatBytes(disk.trackedBytes)} caption={`${formatCount(totals.assets)} assets`} />
          <StatTile
            label="Untracked"
            value={formatBytes(disk.orphanBytes)}
            caption={disk.orphanBytes > 0 ? 'Files with no database row' : 'Everything accounted for'}
          />
          <StatTile label="Database" value={formatBytes(disk.databaseBytes)} caption="SQLite and WAL" />
        </div>
      </CardContent>
    </Card>
  )
}

export function AdminStorageRoute() {
  const { user } = useSession()
  const { data, error, isPending } = useQuery({
    queryKey: ['storage'],
    queryFn: () => api<StorageReport>('/admin/storage'),
  })

  if (user?.role !== 'admin') return <ErrorBlock message="Administrator access required" />
  if (isPending) return <LoadingBlock label="Measuring storage" />
  if (error) return <ErrorBlock message={(error as Error).message} />

  return (
    <>
      <PageHeading
        eyebrow="Manage"
        title="Storage"
        description="Where the bytes are, and which artifacts are worth reclaiming."
      />

      <Disk report={data} />

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>By repository</CardTitle>
          </CardHeader>
          <CardContent>
            <BarList
              format={formatBytes}
              emptyLabel="No repositories yet"
              data={data.repositories.map((entry) => ({
                id: entry.repository,
                label: entry.repository,
                value: entry.bytes,
                caption: `${formatCount(entry.components)} artifacts`,
                href: `/repositories/${entry.repository}`,
              }))}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>By file type</CardTitle>
          </CardHeader>
          <CardContent>
            <BarList
              format={formatBytes}
              emptyLabel="Nothing published yet"
              data={data.extensions.map((entry) => ({
                id: entry.extension,
                label: `.${entry.extension}`,
                value: entry.bytes,
                caption: `${formatCount(entry.files)} files`,
              }))}
            />
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Largest artifacts</CardTitle>
          <CardDescription>The 20 heaviest versions across every repository.</CardDescription>
        </CardHeader>
        <Table className="[&_td]:px-4 [&_th]:px-4">
          <TableHeader>
            <TableRow>
              <TableHead>Artifact</TableHead>
              <TableHead className="w-24">Format</TableHead>
              <TableHead>Version</TableHead>
              <TableHead>Repository</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead>Updated</TableHead>
              <TableHead className="w-12" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.largest.map((component) => (
              <TableRow key={`${component.repository}:${component.namespace}:${component.name}:${component.version}`}>
                <TableCell>
                  <Link
                    to={`/artifacts/${component.repository}?namespace=${encodeURIComponent(component.namespace)}&name=${encodeURIComponent(component.name)}`}
                    className="block min-w-0"
                  >
                    <StackedCoordinate
                      format={component.format}
                      namespace={component.namespace}
                      name={component.name}
                    />
                  </Link>
                </TableCell>
                <TableCell>
                  <FormatBadge format={component.format} />
                </TableCell>
                <TableCell className="whitespace-nowrap font-mono text-[13px]">
                  <span className="flex items-center gap-2">
                    {component.version}
                    {component.isSnapshot ? <Badge variant="snapshot">snap</Badge> : null}
                  </span>
                </TableCell>
                <TableCell className="whitespace-nowrap font-mono text-xs text-muted-foreground">
                  {component.repository}
                </TableCell>
                <TableCell className="whitespace-nowrap text-right font-mono text-xs tabular-nums">
                  {formatBytes(component.bytes)}
                </TableCell>
                <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                  {formatRelative(component.updatedAt)}
                </TableCell>
                <TableCell>
                  <DeleteArtifactDialog
                    repository={component.repository}
                    namespace={component.namespace}
                    name={component.name}
                    version={component.version}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </>
  )
}
