import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link2, Pencil, Plus, Trash2 } from 'lucide-react'
import { Badge, PolicyBadge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogForm,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Checkbox, Field, Input, Select, Textarea } from '@/components/ui/input'
import { EmptyState, ErrorBlock, LoadingBlock } from '@/components/ui/feedback'
import { CopyButton } from '@/components/copy-button'
import {
  ApiError,
  api,
  del,
  patch,
  post,
  type DeploymentLink,
  type MatchType,
  type Policy,
  type Repository,
  type RepositoryFormat,
} from '@/lib/api'
import { useSession } from '@/lib/session'
import { formatBytes, formatRelative } from '@/lib/utils'

const iconAction =
  'inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-foreground'

export function linksQuery(repository: string) {
  return {
    queryKey: ['links', repository],
    queryFn: () => api<{ links: DeploymentLink[] }>(`/repositories/${repository}/links`),
  }
}

// namespaceLabel names the coordinate the way the format does, so a Maven user
// reads "Group ID" rather than the internal word for it.
function namespaceLabel(format: RepositoryFormat): string {
  if (format === 'maven2') return 'Group ID'
  if (format === 'npm') return 'Scope'
  return 'Namespace'
}

function artifactLabel(format: RepositoryFormat): string {
  if (format === 'maven2') return 'Artifact ID'
  if (format === 'npm') return 'Package'
  return 'Artifact'
}

interface LinkDraft {
  slug: string
  description: string
  namespace: string
  artifact: string
  matchType: MatchType
  pattern: string
  policy: Policy
  filename: string
  disabled: boolean
}

function emptyDraft(): LinkDraft {
  return {
    slug: '',
    description: '',
    namespace: '',
    artifact: '',
    matchType: 'template',
    pattern: '{name}-{version}.jar',
    policy: 'release',
    filename: '',
    disabled: false,
  }
}

function draftOf(deployment: DeploymentLink): LinkDraft {
  return {
    slug: deployment.slug,
    description: deployment.description,
    namespace: deployment.namespace,
    artifact: deployment.artifact,
    matchType: deployment.matchType,
    pattern: deployment.pattern,
    policy: deployment.policy,
    filename: deployment.filename,
    disabled: deployment.disabled,
  }
}

// LinkForm is the whole of what a link is, shared by creating one and editing
// one so the two cannot drift apart.
function LinkForm({
  draft,
  onChange,
  format,
}: {
  draft: LinkDraft
  onChange: (draft: LinkDraft) => void
  format: RepositoryFormat
}) {
  const set = <K extends keyof LinkDraft>(key: K, value: LinkDraft[K]) =>
    onChange({ ...draft, [key]: value })

  return (
    <>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={namespaceLabel(format)} hint="Leave empty if the format has none.">
          <Input
            value={draft.namespace}
            onChange={(event) => set('namespace', event.target.value)}
            placeholder="dev.pixelib.pixelscript"
            className="font-mono"
          />
        </Field>
        <Field label={artifactLabel(format)}>
          <Input
            value={draft.artifact}
            onChange={(event) => set('artifact', event.target.value)}
            placeholder="pixelscript-paper"
            className="font-mono"
            autoFocus
          />
        </Field>
      </div>

      <Field label="Match" hint="A template is literal text, with * for anything that varies.">
        <Select
          value={draft.matchType}
          onChange={(event) => set('matchType', event.target.value as MatchType)}
        >
          <option value="template">Template, with placeholders</option>
          <option value="regex">Regular expression</option>
        </Select>
      </Field>

      <Field
        label="Pattern"
        hint={
          draft.matchType === 'template'
            ? 'Placeholders: {version}, {baseVersion}, {name}, {namespace}.'
            : 'Matched against the whole filename. Placeholders are substituted first, quoted.'
        }
      >
        <Input
          value={draft.pattern}
          onChange={(event) => set('pattern', event.target.value)}
          placeholder={draft.matchType === 'template' ? '{name}-{version}.jar' : String.raw`.*-\d+\.jar`}
          className="font-mono"
        />
      </Field>

      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Resolves to" hint="Which versions the link may pick from.">
          <Select value={draft.policy} onChange={(event) => set('policy', event.target.value as Policy)}>
            <option value="release">The latest release</option>
            <option value="snapshot">The latest prerelease</option>
            <option value="mixed">The latest of either</option>
          </Select>
        </Field>
        <Field
          label="Slug"
          hint="The last part of the URL. Left empty, a random unguessable one is minted; anyone who knows the URL can download the file."
        >
          <Input
            value={draft.slug}
            onChange={(event) => set('slug', event.target.value.toLowerCase())}
            placeholder="random"
            className="font-mono"
          />
        </Field>
      </div>

      <Field
        label="Download as"
        hint="Serve the file under a fixed name. Empty keeps the published filename, which carries the version."
      >
        <Input
          value={draft.filename}
          onChange={(event) => set('filename', event.target.value)}
          placeholder="pixelscript-paper.jar"
          className="font-mono"
        />
      </Field>

      <Field label="Description">
        <Textarea
          value={draft.description}
          onChange={(event) => set('description', event.target.value)}
          placeholder="What follows this link, so the next administrator knows what breaks."
        />
      </Field>

      <label className="flex items-start gap-3 rounded-2xl bg-muted p-4 text-sm">
        <Checkbox
          checked={draft.disabled}
          onChange={(event) => set('disabled', event.target.checked)}
          className="mt-0.5"
        />
        <span>
          Disabled
          <span className="mt-0.5 block text-xs text-muted-foreground">
            The URL answers 404 while this is on, without giving the link up.
          </span>
        </span>
      </label>
    </>
  )
}

function EditLinkDialog({
  repository,
  deployment,
}: {
  repository: Repository
  deployment?: DeploymentLink
}) {
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<LinkDraft>(deployment ? draftOf(deployment) : emptyDraft())
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () =>
      deployment
        ? patch(`/repositories/${repository.name}/links/${deployment.id}`, draft)
        : post(`/repositories/${repository.name}/links`, draft),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['links', repository.name] })
      setOpen(false)
      if (!deployment) setDraft(emptyDraft())
    },
  })

  // Reopening after a cancelled edit should show what is stored, not the
  // abandoned draft.
  function change(next: boolean) {
    if (next) setDraft(deployment ? draftOf(deployment) : emptyDraft())
    mutation.reset()
    setOpen(next)
  }

  return (
    <Dialog open={open} onOpenChange={change}>
      <DialogTrigger asChild>
        {deployment ? (
          <button type="button" className={iconAction} aria-label={`Edit ${deployment.slug}`}>
            <Pencil className="size-3.5" />
          </button>
        ) : (
          <Button size="sm" variant="secondary">
            <Plus /> New link
          </Button>
        )}
      </DialogTrigger>
      <DialogContent>
        <DialogForm
          onSubmit={(event: FormEvent) => {
            event.preventDefault()
            mutation.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>{deployment ? deployment.slug : 'New deployment link'}</DialogTitle>
            <DialogDescription>
              A fixed URL that always downloads one file of the newest version, without
              authentication, whatever the repository's visibility.
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
            <LinkForm draft={draft} onChange={setDraft} format={repository.format} />
          </DialogBody>
          <DialogFooter>
            <Button
              type="submit"
              disabled={!draft.artifact.trim() || !draft.pattern.trim() || mutation.isPending}
            >
              {deployment ? 'Save link' : 'Create link'}
            </Button>
          </DialogFooter>
        </DialogForm>
      </DialogContent>
    </Dialog>
  )
}

function DeleteLinkButton({
  repository,
  deployment,
}: {
  repository: string
  deployment: DeploymentLink
}) {
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => del(`/repositories/${repository}/links/${deployment.id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['links', repository] }),
  })

  return (
    <button
      type="button"
      className="inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-destructive"
      aria-label={`Delete ${deployment.slug}`}
      disabled={mutation.isPending}
      onClick={() => mutation.mutate()}
    >
      <Trash2 className="size-3.5" />
    </button>
  )
}

// LinkRow shows the URL first and what it currently resolves to underneath,
// because the second is the thing that quietly changes.
function LinkRow({
  repository,
  deployment,
  manageable,
}: {
  repository: Repository
  deployment: DeploymentLink
  manageable: boolean
}) {
  return (
    <li className="space-y-2.5 rounded-2xl bg-muted p-4">
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-[13px] font-medium">{deployment.slug}</span>
            <PolicyBadge policy={deployment.policy} />
            {deployment.disabled ? <Badge variant="danger">disabled</Badge> : null}
          </div>
          <a
            href={deployment.url}
            className="block truncate font-mono text-xs text-primary hover:underline"
          >
            {deployment.url}
          </a>
        </div>
        <CopyButton value={deployment.url} className="shrink-0" />
        {manageable ? (
          <>
            <EditLinkDialog repository={repository} deployment={deployment} />
            <DeleteLinkButton repository={repository.name} deployment={deployment} />
          </>
        ) : null}
      </div>

      {deployment.description ? (
        <p className="text-xs text-muted-foreground">{deployment.description}</p>
      ) : null}

      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {deployment.resolved ? (
          <>
            <span>
              Serving{' '}
              <span className="font-mono text-foreground">{deployment.resolved.filename}</span> from{' '}
              <span className="font-mono text-primary">{deployment.resolved.version}</span>
            </span>
            <span>{formatBytes(deployment.resolved.size)}</span>
          </>
        ) : (
          <span className="text-destructive">
            Nothing published matches{' '}
            <span className="font-mono">{deployment.pattern}</span> yet
          </span>
        )}
        <span>
          {deployment.downloads} download{deployment.downloads === 1 ? '' : 's'}
          {deployment.lastUsedAt > 0 ? `, last ${formatRelative(deployment.lastUsedAt)}` : ''}
        </span>
      </div>
    </li>
  )
}

// linksApply reports whether a repository can hold links at all. A group has no
// artifacts of its own, and a docker repository holds blobs a registry client
// assembles rather than files to hand out one at a time.
export function linksApply(repository: Repository): boolean {
  return repository.type !== 'group' && repository.format !== 'docker'
}

// DeploymentLinks is the list itself, reused by the manage dialog and the
// repository page. Managing links takes an administrator, which is what the API
// asks for, rather than admin permission on this one repository.
export function DeploymentLinks({ repository }: { repository: Repository }) {
  const { user } = useSession()
  const manageable = user?.role === 'admin'
  const { data, error, isLoading } = useQuery(linksQuery(repository.name))

  if (isLoading) return <LoadingBlock label="Loading links" />
  if (error) return <ErrorBlock message={(error as Error).message} />

  const links = data?.links ?? []
  if (links.length === 0) {
    return (
      <EmptyState
        title="No deployment links yet"
        description="A deployment link is a fixed URL that downloads one file of the newest version of an artifact, without authentication. It is how a private repository publishes exactly one file to a build that has no credentials."
        action={manageable ? <EditLinkDialog repository={repository} /> : undefined}
      />
    )
  }

  return (
    <div className="space-y-4">
      {manageable ? (
        <div className="flex justify-end">
          <EditLinkDialog repository={repository} />
        </div>
      ) : null}
      <ul className="space-y-2.5">
        {links.map((deployment) => (
          <LinkRow
            key={deployment.id}
            repository={repository}
            deployment={deployment}
            manageable={manageable}
          />
        ))}
      </ul>
    </div>
  )
}

export function LinksDialog({ repository }: { repository: Repository }) {
  const [open, setOpen] = useState(false)

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button
          type="button"
          className={iconAction}
          aria-label={`Deployment links for ${repository.name}`}
        >
          <Link2 className="size-3.5" />
        </button>
      </DialogTrigger>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{repository.name}</DialogTitle>
          <DialogDescription>
            Deployment links. Each one downloads a file from the newest matching version, without
            authentication, even though this repository is {repository.visibility}.
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          {open ? <DeploymentLinks repository={repository} /> : null}
        </DialogBody>
      </DialogContent>
    </Dialog>
  )
}
