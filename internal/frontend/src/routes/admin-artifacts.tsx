import { useState } from 'react'
import { Link } from 'react-router-dom'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { Badge, FormatBadge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Select } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { EmptyState, ErrorBlock, LoadingBlock, PageHeading } from '@/components/ui/feedback'
import { StackedCoordinate } from '@/components/copy-button'
import { DeleteArtifactDialog } from '@/components/delete-artifact'
import { api, type ComponentUsage, type Repository } from '@/lib/api'
import { formatBytes, formatRelative, plural } from '@/lib/utils'
import { useSession } from '@/lib/session'

export function AdminArtifactsRoute() {
  const { user } = useSession()
  const [repository, setRepository] = useState('')
  const [search, setSearch] = useState('')

  const repositories = useQuery({
    queryKey: ['repositories'],
    queryFn: () => api<{ repositories: Repository[] }>('/repositories'),
  })

  const query = new URLSearchParams()
  if (repository) query.set('repository', repository)
  if (search.trim()) query.set('q', search.trim())

  const { data, error, isPending, isFetching } = useQuery({
    queryKey: ['components', repository, search.trim()],
    queryFn: () => api<{ components: ComponentUsage[] }>(`/admin/components?${query}`),
    placeholderData: keepPreviousData,
  })

  if (user?.role !== 'admin') return <ErrorBlock message="Administrator access required" />

  const components = data?.components ?? []

  return (
    <>
      <PageHeading
        eyebrow="Manage"
        title="Artifacts"
        description="Find a published version and remove it from the repository and from disk."
      />

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative min-w-0 flex-1 sm:max-w-xs">
          <Search className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-faint" />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Filter by namespace or name"
            aria-label="Filter artifacts"
            className="h-9 w-full rounded-full border border-transparent bg-muted pl-10 pr-4 text-[13px] text-foreground transition-colors placeholder:text-faint hover:bg-accent focus-visible:border-primary focus-visible:bg-card focus-visible:outline-none"
          />
        </div>
        <Select
          className="h-9 w-52"
          value={repository}
          onChange={(event) => setRepository(event.target.value)}
          aria-label="Filter by repository"
        >
          <option value="">Every repository</option>
          {(repositories.data?.repositories ?? []).map((entry) => (
            <option key={entry.id} value={entry.name}>
              {entry.name}
            </option>
          ))}
        </Select>
        <p className="text-xs text-muted-foreground">{plural(components.length, 'version')}</p>
      </div>

      {isPending ? (
        <LoadingBlock label="Loading artifacts" />
      ) : error ? (
        <ErrorBlock message={(error as Error).message} />
      ) : components.length === 0 ? (
        <EmptyState
          title="No artifacts match"
          description="Publish something with Maven, or widen the filter."
        />
      ) : (
        <Card className={isFetching ? 'opacity-50 transition-opacity' : 'transition-opacity'}>
          <Table className="[&_td]:px-4 [&_th]:px-4">
            <TableHeader>
              <TableRow>
                <TableHead>Artifact</TableHead>
                <TableHead className="w-24">Format</TableHead>
                <TableHead>Version</TableHead>
                <TableHead>Repository</TableHead>
                <TableHead className="text-right">Files</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead>Published</TableHead>
                <TableHead className="w-12" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {components.map((component) => (
                <TableRow
                  key={`${component.repository}:${component.namespace}:${component.name}:${component.version}`}
                >
                  <TableCell>
                    <Link
                      to={`/artifacts/${component.repository}?namespace=${encodeURIComponent(component.namespace)}&name=${encodeURIComponent(component.name)}&version=${encodeURIComponent(component.version)}`}
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
                  <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                    {component.files}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-right font-mono text-xs tabular-nums">
                    {formatBytes(component.bytes)}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                    {formatRelative(component.updatedAt)}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center justify-end gap-0.5">
                      <DeleteArtifactDialog
                        repository={component.repository}
                        namespace={component.namespace}
                        name={component.name}
                        version={component.version}
                      />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </>
  )
}
