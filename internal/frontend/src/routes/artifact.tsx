import { useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, Download } from 'lucide-react'
import { Badge, FormatBadge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ErrorBlock, LoadingBlock } from '@/components/ui/feedback'
import { CopyButton } from '@/components/copy-button'
import { DeleteArtifactDialog } from '@/components/delete-artifact'
import { MavenDependency, MavenSetup } from '@/components/snippets/maven'
import { NPMDependency, NPMSetup } from '@/components/snippets/npm'
import {
  api,
  type ArtifactDetail,
  type BrowseEntry,
  type RepositoryDetail,
  type RepositoryFormat,
} from '@/lib/api'
import { coordinateLabel } from '@/lib/coordinates'
import { cn, formatBytes, formatRelative, plural } from '@/lib/utils'

function Files({
  repository,
  namespace,
  name,
  version,
}: {
  repository: string
  namespace: string
  name: string
  version: string
}) {
  const query = new URLSearchParams({ namespace, name, version })
  const { data, error, isLoading } = useQuery({
    queryKey: ['artifact-files', repository, namespace, name, version],
    queryFn: () =>
      api<{ prefix: string; files: BrowseEntry[] }>(
        `/repositories/${repository}/artifacts/files?${query}`,
      ),
  })

  if (isLoading) return <LoadingBlock label="Loading files" />
  if (error) return <ErrorBlock message={(error as Error).message} />

  return (
    <Card>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>File</TableHead>
            <TableHead className="w-28">Size</TableHead>
            <TableHead className="w-36">Published</TableHead>
            <TableHead className="w-12" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {(data?.files ?? []).map((file) => (
            <TableRow key={file.path}>
              <TableCell>
                <a
                  href={`/repository/${repository}/${file.path}`}
                  className="flex items-center gap-2 font-mono text-[13px] hover:text-primary"
                >
                  <span aria-hidden className="size-1.5 rounded-full bg-faint" />
                  {file.name}
                </a>
              </TableCell>
              <TableCell className="font-mono text-xs tabular-nums text-muted-foreground">
                {formatBytes(file.size ?? 0)}
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {formatRelative(file.updatedAt)}
              </TableCell>
              <TableCell>
                <a
                  href={`/repository/${repository}/${file.path}`}
                  download
                  aria-label={`Download ${file.name}`}
                  className="inline-flex size-6 items-center justify-center text-faint transition-colors hover:bg-accent hover:text-foreground"
                >
                  <Download className="size-3" />
                </a>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  )
}

function Snippets({
  format,
  repository,
  namespace,
  name,
  version,
}: {
  format: RepositoryFormat
  repository: string
  namespace: string
  name: string
  version: string
}) {
  const { data } = useQuery({
    queryKey: ['repository', repository],
    queryFn: () => api<RepositoryDetail>(`/repositories/${repository}`),
  })

  if (format === 'npm') {
    return (
      <div className="space-y-7">
        <NPMDependency namespace={namespace} name={name} version={version} />
        {data ? <NPMSetup repository={data} namespace={namespace} /> : null}
      </div>
    )
  }

  return (
    <div className="space-y-7">
      <MavenDependency namespace={namespace} name={name} version={version} />
      {data ? <MavenSetup repository={data} /> : null}
    </div>
  )
}

export function ArtifactRoute() {
  const routeParams = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const repository = routeParams.name!
  const namespace = params.get('namespace') ?? ''
  const name = params.get('name') ?? ''
  const [selected, setSelected] = useState<string | null>(params.get('version'))

  const detail = useQuery({
    queryKey: ['repository', repository],
    queryFn: () => api<RepositoryDetail>(`/repositories/${repository}`),
  })

  const { data, error, isLoading } = useQuery({
    queryKey: ['artifact', repository, namespace, name],
    queryFn: () =>
      api<ArtifactDetail>(
        `/repositories/${repository}/artifacts?namespace=${encodeURIComponent(namespace)}&name=${encodeURIComponent(name)}`,
      ),
    // An empty namespace is valid: an unscoped npm package has none.
    enabled: name.length > 0,
  })

  if (!name) return <ErrorBlock message="Missing name parameter" />
  if (isLoading) return <LoadingBlock label="Loading artifact" />
  if (error) return <ErrorBlock message={(error as Error).message} />
  if (!data) return null

  const version = selected ?? data.versions[0]?.version ?? ''
  const format = data.format
  const npm = format === 'npm'
  const coordinates = coordinateLabel(format, namespace, name, version)

  return (
    <>
      <div className="space-y-5">
        <Link
          to={`/repositories/${repository}`}
          className="inline-flex items-center gap-1.5 text-[13px] font-medium text-primary transition-opacity hover:opacity-70"
        >
          <ChevronLeft className="size-3.5" />
          {repository}
        </Link>

        <div className="space-y-1">
          {npm ? (
            <h1 className="font-mono text-4xl font-semibold">
              {namespace ? <span className="text-muted-foreground">@{namespace}/</span> : null}
              {name}
            </h1>
          ) : (
            <>
              <p className="font-mono text-sm text-muted-foreground">{namespace}</p>
              <h1 className="font-mono text-4xl font-semibold">{name}</h1>
            </>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <FormatBadge format={format} />
          {data.release ? <Badge variant="release">release {data.release}</Badge> : null}
          {data.latest && data.latest !== data.release ? (
            <Badge variant="snapshot">latest {data.latest}</Badge>
          ) : null}
          <Badge variant="neutral">{plural(data.versions.length, 'version')}</Badge>
        </div>

        <div className="flex items-center gap-3 rounded-2xl bg-tint py-2.5 pl-5 pr-2.5">
          <code className="min-w-0 flex-1 overflow-x-auto scrollbar-thin whitespace-nowrap font-mono text-[15px]">
            {npm ? (
              namespace ? (
                <span className="text-tint-foreground/70">@{namespace}/</span>
              ) : null
            ) : (
              <>
                <span className="text-tint-foreground/70">{namespace}</span>
                <span className="text-tint-foreground/40">:</span>
              </>
            )}
            <span className="font-medium text-tint-foreground">{name}</span>
            <span className="text-tint-foreground/40">{npm ? '@' : ':'}</span>
            <span className="font-medium text-tint-foreground">{version}</span>
          </code>
          <CopyButton value={coordinates} className="shrink-0 hover:bg-card/60" />
        </div>
      </div>

      <div className="grid gap-7 lg:grid-cols-[15rem_1fr]">
        <div className="space-y-2.5">
          <p className="text-xs font-medium text-muted-foreground">Versions</p>
          <div className="max-h-[26rem] space-y-1 overflow-y-auto scrollbar-thin rounded-2xl bg-muted p-1.5">
            {data.versions.map((entry) => (
              <button
                key={entry.version}
                type="button"
                onClick={() => setSelected(entry.version)}
                className={cn(
                  'flex w-full items-center gap-2 rounded-xl px-3 py-2 text-left transition-colors',
                  entry.version === version
                    ? 'bg-card font-medium text-foreground shadow-card'
                    : 'text-muted-foreground hover:bg-accent/70 hover:text-foreground',
                )}
              >
                <span className="flex-1 truncate font-mono text-[13px]">{entry.version}</span>
                {entry.isSnapshot ? (
                  <span className="shrink-0 rounded-full bg-snapshot-soft px-1.5 py-0.5 text-[10px] font-medium text-snapshot">
                    snap
                  </span>
                ) : null}
              </button>
            ))}
          </div>
          <p className="px-1 text-xs text-muted-foreground">
            Published {formatRelative(data.versions.find((v) => v.version === version)?.updatedAt)}
          </p>

          {detail.data?.permission === 'admin' ? (
            <div className="rounded-2xl bg-muted p-2">
              <p className="px-1.5 pb-1 text-xs text-muted-foreground">Delete</p>
              <div className="flex items-center gap-1">
                <DeleteArtifactDialog
                  repository={repository}
                  namespace={namespace}
                  name={name}
                  version={version}
                  label="This version"
                  onDeleted={() => {
                    if (data.versions.length > 1) {
                      setSelected(null)
                      return
                    }
                    navigate(`/repositories/${repository}`)
                  }}
                />
                <DeleteArtifactDialog
                  repository={repository}
                  namespace={namespace}
                  name={name}
                  label="All versions"
                  onDeleted={() => navigate(`/repositories/${repository}`)}
                />
              </div>
            </div>
          ) : null}
        </div>

        <Tabs defaultValue="install">
          <TabsList>
            <TabsTrigger value="install">Install</TabsTrigger>
            <TabsTrigger value="files">Files</TabsTrigger>
          </TabsList>
          <TabsContent value="install">
            <Snippets
              format={format}
              repository={repository}
              namespace={namespace}
              name={name}
              version={version}
            />
          </TabsContent>
          <TabsContent value="files">
            <Files
              repository={repository}
              namespace={namespace}
              name={name}
              version={version}
            />
          </TabsContent>
        </Tabs>
      </div>
    </>
  )
}
