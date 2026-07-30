import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, Layers, Package } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ErrorBlock, LoadingBlock } from '@/components/ui/feedback'
import { CopyButton } from '@/components/copy-button'
import { api, type DockerChild, type DockerLayer, type DockerManifest } from '@/lib/api'
import { formatBytes, formatRelative, plural } from '@/lib/utils'

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <div className="font-mono text-[13px] break-all">{children}</div>
    </div>
  )
}

function Summary({ manifest }: { manifest: DockerManifest }) {
  const platform = [manifest.os, manifest.architecture, manifest.variant].filter(Boolean).join('/')

  return (
    <Card className="space-y-5 p-5">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="primary">{manifest.isIndex ? 'multi-platform index' : 'image'}</Badge>
        {platform ? <Badge variant="neutral">{platform}</Badge> : null}
        {manifest.isIndex ? (
          <Badge variant="neutral">
            {plural(manifest.children.filter((child) => !child.referenceType).length, 'platform')}
          </Badge>
        ) : (
          <Badge variant="neutral">{plural(manifest.layerCount, 'layer')}</Badge>
        )}
      </div>

      <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
        <Field label="Total size">{formatBytes(manifest.totalSize)}</Field>
        <Field label="Manifest size">{formatBytes(manifest.size)}</Field>
        <Field label="Built">
          {manifest.imageCreated ? formatRelative(manifest.imageCreated) : 'unknown'}
        </Field>
        <Field label="Media type">
          <span className="text-muted-foreground">{manifest.mediaType}</span>
        </Field>
      </div>

      <div className="flex items-center gap-3 rounded-2xl bg-muted py-2.5 pl-4 pr-2.5">
        <code className="min-w-0 flex-1 overflow-x-auto scrollbar-thin whitespace-nowrap font-mono text-xs">
          {manifest.digest}
        </code>
        <CopyButton value={manifest.digest} className="shrink-0" />
      </div>
    </Card>
  )
}

// attestationLabel names a child that is not a platform. Every buildkit
// multi-platform build attaches one, and it declares a platform of unknown/unknown,
// so labelling it is the difference between provenance and an apparent broken build.
function attestationLabel(child: DockerChild): string {
  if (!child.referenceType) return child.platform || 'unknown'
  if (child.referenceType === 'attestation-manifest') return 'attestation'
  return child.referenceType
}

// Platforms is the part a plain file listing can never show: an index holds no
// layers of its own, only one child manifest per platform.
function Platforms({
  platforms,
  onSelect,
}: {
  platforms: DockerChild[]
  onSelect: (digest: string) => void
}) {
  return (
    <div className="space-y-2.5">
      <p className="text-xs font-medium text-muted-foreground">Platforms</p>
      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Platform</TableHead>
              <TableHead className="w-28">Size</TableHead>
              <TableHead className="w-24">Layers</TableHead>
              <TableHead className="w-44">Digest</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {platforms.map((child) => (
              <TableRow
                key={child.digest}
                onClick={() => (child.indexed ? onSelect(child.digest) : undefined)}
                className={child.indexed ? 'cursor-pointer' : undefined}
              >
                <TableCell className="font-mono text-[13px]">
                  {child.indexed ? (
                    <span className="inline-flex items-center gap-1.5 hover:text-primary">
                      <ChevronRight className="size-3 text-faint" />
                      {attestationLabel(child)}
                    </span>
                  ) : (
                    attestationLabel(child)
                  )}
                  {child.referenceType ? (
                    <span className="ml-2 rounded-full bg-muted px-1.5 py-0.5 font-sans text-[10px] font-medium text-muted-foreground">
                      not a platform
                    </span>
                  ) : null}
                </TableCell>
                <TableCell className="font-mono text-xs tabular-nums text-muted-foreground">
                  {child.indexed ? formatBytes(child.totalSize) : formatBytes(child.size)}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {child.indexed ? child.layerCount : '-'}
                </TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  {child.short}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
      <p className="px-1 text-xs text-muted-foreground">
        {platforms.some((child) => !child.indexed)
          ? 'A platform without a layer count has not been indexed here yet, which is normal while an index is still being pushed.'
          : 'Open a platform to see its layers and the configuration it was built with. An index holds neither of its own.'}
      </p>
    </div>
  )
}

// A foreign layer's source comes from a pushed manifest, so it is not necessarily a
// URL that parses. The raw value is better than a crash.
function sourceHost(urls: string[]): string {
  const [first] = urls
  if (!first) return 'elsewhere'
  try {
    return new URL(first).host
  } catch {
    return first
  }
}

function LayerNote({ layer }: { layer: DockerLayer }) {
  if (layer.foreign) {
    return (
      <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
        fetched from {sourceHost(layer.urls)}
      </span>
    )
  }
  if (!layer.stored) {
    return (
      <span className="rounded-full bg-destructive-soft px-1.5 py-0.5 text-[10px] font-medium text-destructive">
        missing
      </span>
    )
  }
  if (layer.sharedWith > 0) {
    return (
      <span className="rounded-full bg-primary-soft px-1.5 py-0.5 text-[10px] font-medium text-primary">
        shared with {plural(layer.sharedWith, 'image')}
      </span>
    )
  }
  return null
}

function LayerTable({ manifest }: { manifest: DockerManifest }) {
  const shared = manifest.layers.filter((layer) => layer.sharedWith > 0)
  const unique = manifest.layers
    .filter((layer) => layer.sharedWith === 0 && !layer.foreign)
    .reduce((total, layer) => total + layer.size, 0)

  return (
    <div className="space-y-2.5">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="text-xs font-medium text-muted-foreground">Layers</p>
        {shared.length > 0 ? (
          <p className="text-xs text-muted-foreground">
            {formatBytes(unique)} of {formatBytes(manifest.totalSize)} is unique to this
            tag, so deleting it reclaims only that much
          </p>
        ) : null}
      </div>
      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10">#</TableHead>
              <TableHead>Digest</TableHead>
              <TableHead className="w-28">Size</TableHead>
              <TableHead>Media type</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {manifest.layers.map((layer) => (
              <TableRow key={`${layer.position}-${layer.digest}`}>
                <TableCell className="font-mono text-xs tabular-nums text-faint">
                  {layer.position + 1}
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-mono text-[13px]">{layer.short}</span>
                    <LayerNote layer={layer} />
                  </div>
                </TableCell>
                <TableCell className="font-mono text-xs tabular-nums text-muted-foreground">
                  {formatBytes(layer.size)}
                </TableCell>
                <TableCell className="truncate text-xs text-muted-foreground">
                  {layer.mediaType.replace('application/vnd.', '')}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  )
}

function List({ label, values }: { label: string; values: string[] }) {
  if (values.length === 0) return null
  return (
    <Field label={label}>
      <div className="space-y-0.5">
        {values.map((value) => (
          <p key={value} className="text-muted-foreground">
            {value}
          </p>
        ))}
      </div>
    </Field>
  )
}

function Configuration({ manifest }: { manifest: DockerManifest }) {
  const config = manifest.config
  if (!config) {
    return (
      <p className="text-sm text-muted-foreground">
        The config blob for this image is not stored here, so its build settings cannot
        be read. That is normal in a repository that was migrated rather than pushed.
      </p>
    )
  }

  const labels = Object.entries(manifest.labels)
  const declared =
    config.entrypoint.length +
    config.cmd.length +
    config.env.length +
    config.exposedPorts.length +
    labels.length +
    (config.workingDir ? 1 : 0) +
    (config.user ? 1 : 0)

  // An image built by "docker import", or any scratch image, declares none of this.
  // Saying so beats rendering an empty card.
  if (declared === 0) {
    return (
      <div className="space-y-2.5">
        <p className="text-xs font-medium text-muted-foreground">Configuration</p>
        <p className="text-sm text-muted-foreground">
          This image declares no entrypoint, command, environment or labels.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-5">
      <p className="text-xs font-medium text-muted-foreground">Configuration</p>
      <Card className="grid gap-5 p-5 sm:grid-cols-2">
        <List label="Entrypoint" values={config.entrypoint} />
        <List label="Command" values={config.cmd} />
        {config.workingDir ? <Field label="Working directory">{config.workingDir}</Field> : null}
        {config.user ? <Field label="User">{config.user}</Field> : null}
        <List label="Exposed ports" values={config.exposedPorts} />
        <List label="Environment" values={config.env} />
        {labels.length > 0 ? (
          <div className="space-y-1 sm:col-span-2">
            <p className="text-xs font-medium text-muted-foreground">Labels</p>
            <div className="space-y-0.5">
              {labels.map(([key, value]) => (
                <p key={key} className="font-mono text-[13px] break-all">
                  <span className="text-muted-foreground">{key}</span>
                  <span className="text-faint"> = </span>
                  {value}
                </p>
              ))}
            </div>
          </div>
        ) : null}
      </Card>
    </div>
  )
}

// History is longer than the layer list, because an instruction that only changes
// metadata produces an entry with no filesystem layer behind it.
function History({ manifest }: { manifest: DockerManifest }) {
  const history = manifest.config?.history ?? []
  if (history.length === 0) return null

  return (
    <div className="space-y-2.5">
      <p className="text-xs font-medium text-muted-foreground">Build history</p>
      <Card className="divide-y divide-border">
        {history.map((step, index) => (
          <div key={index} className="flex items-start gap-3 px-4 py-2.5">
            <span aria-hidden className="mt-1 shrink-0 text-faint">
              {step.emptyLayer ? <Package className="size-3" /> : <Layers className="size-3" />}
            </span>
            <code className="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-xs">
              {step.createdBy || step.comment || '(no instruction recorded)'}
            </code>
            {step.emptyLayer ? (
              <span className="shrink-0 text-[10px] text-faint">no layer</span>
            ) : null}
          </div>
        ))}
      </Card>
    </div>
  )
}

export function DockerManifestView({
  repository,
  namespace,
  name,
  reference,
}: {
  repository: string
  namespace: string
  name: string
  reference: string
}) {
  const [selected, setSelected] = useState<string | null>(null)
  const showing = selected ?? reference

  const query = new URLSearchParams({ namespace, name, reference: showing })
  const { data, error, isLoading } = useQuery({
    queryKey: ['docker-manifest', repository, namespace, name, showing],
    queryFn: () => api<DockerManifest>(`/repositories/${repository}/docker/manifests?${query}`),
    enabled: showing.length > 0,
  })

  if (isLoading) return <LoadingBlock label="Loading manifest" />
  if (error) return <ErrorBlock message={(error as Error).message} />
  if (!data) return null

  return (
    <div className="space-y-7">
      {selected ? (
        <button
          type="button"
          onClick={() => setSelected(null)}
          className="inline-flex items-center gap-1.5 text-[13px] font-medium text-primary transition-opacity hover:opacity-70"
        >
          <ChevronLeft className="size-3.5" />
          back to {reference}
        </button>
      ) : null}
      <Summary manifest={data} />
      {data.isIndex ? (
        <Platforms platforms={data.children} onSelect={setSelected} />
      ) : (
        <>
          <LayerTable manifest={data} />
          <Configuration manifest={data} />
          <History manifest={data} />
        </>
      )}
    </div>
  )
}
