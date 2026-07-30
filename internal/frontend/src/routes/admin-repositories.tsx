import { useEffect, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Plus, Settings2, Trash2, Users } from 'lucide-react'
import { Badge, FormatBadge, PolicyBadge, TypeBadge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogForm,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Checkbox, Field, Input, Select, Textarea } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ErrorBlock, LoadingBlock, PageHeading } from '@/components/ui/feedback'
import {
  ApiError,
  api,
  del,
  patch,
  post,
  put,
  type Category,
  type Grant,
  type ManagedUser,
  type Permission,
  type Policy,
  type Repository,
  type RepositoryDetail,
  type RepositoryFormat,
  type RepositoryType,
  type Visibility,
} from '@/lib/api'
import { categoriesQuery } from '@/lib/categories'
import { useSession } from '@/lib/session'

const iconAction =
  'inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-foreground'

const minutes = (value: number) => Math.round(value / 60)

const NEW_CATEGORY = '__new__'

// CategoryField picks from the categories that already exist, and falls back to
// naming a new one inline so the first repository in a category does not need a
// detour through the categories screen.
function CategoryField({
  value,
  onChange,
  draftName,
  onDraftNameChange,
}: {
  value: string
  onChange: (value: string) => void
  draftName: string
  onDraftNameChange: (value: string) => void
}) {
  const categories = useQuery(categoriesQuery)
  const options = categories.data?.categories ?? []

  return (
    <>
      <Field label="Category" hint="Groups this repository in the sidebar and the index.">
        <Select value={value} onChange={(event) => onChange(event.target.value)}>
          <option value="">No category</option>
          {options.map((category) => (
            <option key={category.id} value={category.id}>
              {category.name}
            </option>
          ))}
          <option value={NEW_CATEGORY}>New category...</option>
        </Select>
      </Field>
      {value === NEW_CATEGORY ? (
        <Field label="New category name">
          <Input
            value={draftName}
            onChange={(event) => onDraftNameChange(event.target.value)}
            placeholder="Internal"
            autoFocus
          />
        </Field>
      ) : null}
    </>
  )
}

async function resolveCategoryID(value: string, draftName: string): Promise<string> {
  if (value !== NEW_CATEGORY) return value

  const created = await post<Category>('/categories', { name: draftName.trim() })
  return created.id
}

// MemberField edits the ordered children of a group. A member is a p2
// repository, optionally narrowed to a path inside it, because an update site
// is usually one directory of a repository rather than the whole of one.
function MemberField({
  members,
  onChange,
  candidates,
}: {
  members: string[]
  onChange: (members: string[]) => void
  candidates: Repository[]
}) {
  const [draft, setDraft] = useState('')

  function add() {
    const target = draft.trim().replace(/^\/+|\/+$/g, '')
    if (!target || members.includes(target)) return
    onChange([...members, target])
    setDraft('')
  }

  function move(index: number, by: number) {
    const to = index + by
    const moved = members[index]
    const other = members[to]
    if (moved === undefined || other === undefined) return

    const next = [...members]
    next[index] = other
    next[to] = moved
    onChange(next)
  }

  return (
    <Field
      label="Members"
      hint="Each child is a p2 repository, optionally with a path inside it. Order is kept as written."
    >
      <div className="space-y-2">
        {members.length > 0 ? (
          <ul className="space-y-1.5">
            {members.map((member, index) => (
              <li key={member} className="flex items-center gap-2 rounded-xl bg-accent px-3 py-2">
                <span className="flex-1 font-mono text-[13px]">{member}</span>
                <button type="button" className={iconAction} onClick={() => move(index, -1)} aria-label="Move up">
                  <ArrowUp className="size-4" />
                </button>
                <button type="button" className={iconAction} onClick={() => move(index, 1)} aria-label="Move down">
                  <ArrowDown className="size-4" />
                </button>
                <button
                  type="button"
                  className={iconAction}
                  onClick={() => onChange(members.filter((entry) => entry !== member))}
                  aria-label="Remove"
                >
                  <Trash2 className="size-4" />
                </button>
              </li>
            ))}
          </ul>
        ) : null}
        <div className="flex gap-2">
          <Select value="" onChange={(event) => setDraft(event.target.value)} className="w-44">
            <option value="">Repository...</option>
            {candidates.map((candidate) => (
              <option key={candidate.id} value={candidate.name}>
                {candidate.name}
              </option>
            ))}
          </Select>
          <Input
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="p2-releases/commons/25.2.5"
            className="flex-1 font-mono"
          />
          <Button type="button" variant="secondary" onClick={add} disabled={!draft.trim()}>
            Add
          </Button>
        </div>
      </div>
    </Field>
  )
}

function CreateRepositoryDialog() {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [format, setFormat] = useState<RepositoryFormat>('maven2')
  const [type, setType] = useState<RepositoryType>('hosted')
  const [policy, setPolicy] = useState<Policy>('release')
  const [visibility, setVisibility] = useState<Visibility>('private')
  const [description, setDescription] = useState('')
  const [remoteUrl, setRemoteUrl] = useState('')
  const [remoteUsername, setRemoteUsername] = useState('')
  const [remotePassword, setRemotePassword] = useState('')
  const [metadataMinutes, setMetadataMinutes] = useState(30)
  const [negativeMinutes, setNegativeMinutes] = useState(30)
  const [retentionDays, setRetentionDays] = useState(0)
  const [categoryId, setCategoryId] = useState('')
  const [categoryName, setCategoryName] = useState('')
  const [members, setMembers] = useState<string[]>([])
  const queryClient = useQueryClient()

  const isProxy = type === 'proxy'
  const isGroup = type === 'group'
  const isNPM = format === 'npm'
  const isP2 = format === 'p2'
  const isDocker = format === 'docker'

  const existing = useQuery({
    queryKey: ['repositories'],
    queryFn: () => api<{ repositories: Repository[] }>('/repositories'),
  })
  const p2Repositories = (existing.data?.repositories ?? []).filter(
    (entry) => entry.format === 'p2' && entry.type !== 'group',
  )

  const mutation = useMutation({
    mutationFn: async () =>
      post('/repositories', {
        name,
        format,
        type,
        policy,
        visibility,
        description,
        categoryId: await resolveCategoryID(categoryId, categoryName),
        ...(isGroup ? { members } : {}),
        ...(isProxy
          ? {
              remoteUrl,
              remoteUsername,
              remotePassword,
              metadataTtlSeconds: metadataMinutes * 60,
              negativeTtlSeconds: negativeMinutes * 60,
              cacheRetentionDays: retentionDays,
            }
          : {}),
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['repositories'] })
      await queryClient.invalidateQueries({ queryKey: ['categories'] })
      setOpen(false)
      setName('')
      setDescription('')
      setRemoteUrl('')
      setRemoteUsername('')
      setRemotePassword('')
      setCategoryName('')
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus /> New repository
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogForm
          onSubmit={(event: FormEvent) => {
            event.preventDefault()
            mutation.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>New repository</DialogTitle>
            <DialogDescription>
              {isDocker
                ? `Docker registry, pulled as ${window.location.host}/${name || 'name'}/<image>`
                : `${isNPM ? 'npm registry' : isP2 ? 'p2 update site' : 'Maven 2 layout'}, served at /repository/${name || 'name'}`}
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
            <Field label="Name" hint="Lowercase letters, digits, dots, dashes and underscores.">
              <Input
                value={name}
                onChange={(event) => setName(event.target.value.toLowerCase())}
                placeholder="maven-releases"
                className="font-mono"
                autoFocus
              />
            </Field>
            <Field label="Format" hint="Fixed once the repository exists. Nothing migrates between formats.">
              <Select
                value={format}
                onChange={(event) => setFormat(event.target.value as RepositoryFormat)}
              >
                <option value="maven2">Maven 2, jars and poms</option>
                <option value="npm">npm, packages and tarballs</option>
                <option value="p2">p2, Eclipse bundles and features</option>
                <option value="docker">Docker, images and OCI artifacts</option>
              </Select>
            </Field>
            <Field label="Type">
              <Select
                value={type}
                onChange={(event) => setType(event.target.value as RepositoryType)}
              >
                <option value="hosted">Hosted, artifacts published here</option>
                <option value="proxy">Proxy, mirrors a remote repository</option>
                <option value="group">Group, one URL over several p2 sites</option>
              </Select>
            </Field>
            {isGroup ? (
              <MemberField members={members} onChange={setMembers} candidates={p2Repositories} />
            ) : null}
            <CategoryField
              value={categoryId}
              onChange={setCategoryId}
              draftName={categoryName}
              onDraftNameChange={setCategoryName}
            />
            {isProxy ? (
              <>
                <Field
                  label="Remote URL"
                  hint={
                    isNPM
                      ? 'The registry base URL this repository mirrors.'
                      : isP2
                        ? 'The host root to mirror, without a path. Update sites point at children above their own directory, which only resolves from the root.'
                        : isDocker
                          ? 'The registry to pull through. Docker Hub needs no path.'
                          : 'The Maven 2 base URL this repository mirrors.'
                  }
                >
                  <Input
                    value={remoteUrl}
                    onChange={(event) => setRemoteUrl(event.target.value)}
                    placeholder={
                      isNPM
                        ? 'https://registry.npmjs.org'
                        : isP2
                          ? 'https://download.eclipse.org'
                          : isDocker
                            ? 'https://registry-1.docker.io'
                            : 'https://repo1.maven.org/maven2'
                    }
                    className="font-mono"
                  />
                </Field>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Remote username" hint="Leave empty for anonymous upstreams.">
                    <Input
                      value={remoteUsername}
                      onChange={(event) => setRemoteUsername(event.target.value)}
                      autoComplete="off"
                    />
                  </Field>
                  <Field label="Remote password">
                    <Input
                      type="password"
                      value={remotePassword}
                      onChange={(event) => setRemotePassword(event.target.value)}
                      autoComplete="new-password"
                    />
                  </Field>
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Metadata cache" hint="Minutes before metadata is rechecked.">
                    <Input
                      type="number"
                      min={0}
                      value={metadataMinutes}
                      onChange={(event) => setMetadataMinutes(Number(event.target.value))}
                    />
                  </Field>
                  <Field label="Not-found cache" hint="Minutes a missing path is remembered.">
                    <Input
                      type="number"
                      min={0}
                      value={negativeMinutes}
                      onChange={(event) => setNegativeMinutes(Number(event.target.value))}
                    />
                  </Field>
                </div>
                <Field
                  label="Evict after"
                  hint="Days without a download before a cached file is deleted. 0 keeps everything."
                >
                  <Input
                    type="number"
                    min={0}
                    value={retentionDays}
                    onChange={(event) => setRetentionDays(Number(event.target.value))}
                  />
                </Field>
              </>
            ) : null}
            <Field label="Version policy">
              <Select value={policy} onChange={(event) => setPolicy(event.target.value as Policy)}>
                <option value="release">{isNPM ? 'Releases only' : 'Release only, immutable'}</option>
                <option value="snapshot">
                  {isNPM ? 'Prereleases only' : 'Snapshot only, redeployable'}
                </option>
                <option value="mixed">Mixed</option>
              </Select>
            </Field>
            <Field label="Visibility">
              <Select
                value={visibility}
                onChange={(event) => setVisibility(event.target.value as Visibility)}
              >
                <option value="private">Private</option>
                <option value="public">Public, anyone can read</option>
              </Select>
            </Field>
            <Field label="Description">
              <Textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </Field>
          </DialogBody>
          <DialogFooter>
            <Button
              type="submit"
              disabled={
                !name.trim() ||
                (isProxy && !remoteUrl.trim()) ||
                (isGroup && members.length === 0) ||
                (categoryId === NEW_CATEGORY && !categoryName.trim()) ||
                mutation.isPending
              }
            >
              Create repository
            </Button>
          </DialogFooter>
        </DialogForm>
      </DialogContent>
    </Dialog>
  )
}

function SettingsDialog({ repository }: { repository: Repository }) {
  const [open, setOpen] = useState(false)
  const [policy, setPolicy] = useState<Policy>(repository.policy)
  const [visibility, setVisibility] = useState<Visibility>(repository.visibility)
  const [description, setDescription] = useState(repository.description)
  const [allowRedeploy, setAllowRedeploy] = useState(repository.allowRedeploy)
  const [remoteUrl, setRemoteUrl] = useState(repository.remoteUrl)
  const [remoteUsername, setRemoteUsername] = useState(repository.remoteUsername)
  const [remotePassword, setRemotePassword] = useState('')
  const [metadataMinutes, setMetadataMinutes] = useState(minutes(repository.metadataTtlSeconds))
  const [negativeMinutes, setNegativeMinutes] = useState(minutes(repository.negativeTtlSeconds))
  const [retentionDays, setRetentionDays] = useState(repository.cacheRetentionDays)
  const [categoryId, setCategoryId] = useState(repository.categoryId ?? '')
  const [categoryName, setCategoryName] = useState('')
  const [members, setMembers] = useState<string[]>([])
  const queryClient = useQueryClient()

  const isProxy = repository.type === 'proxy'
  const isGroup = repository.type === 'group'

  const detail = useQuery({
    queryKey: ['repository', repository.name],
    queryFn: () => api<RepositoryDetail>(`/repositories/${repository.name}`),
    enabled: open && isGroup,
  })
  const existing = useQuery({
    queryKey: ['repositories'],
    queryFn: () => api<{ repositories: Repository[] }>('/repositories'),
    enabled: open && isGroup,
  })
  const p2Repositories = (existing.data?.repositories ?? []).filter(
    (entry) => entry.format === 'p2' && entry.type !== 'group' && entry.name !== repository.name,
  )

  useEffect(() => {
    if (detail.data?.members) setMembers(detail.data.members)
  }, [detail.data])
  // npm publishes are immutable regardless, so the redeploy switch would be a lie.
  const allowsRedeploy = !isProxy && repository.format !== 'npm'

  const mutation = useMutation({
    mutationFn: async () =>
      patch(`/repositories/${repository.name}`, {
        policy,
        visibility,
        description,
        allowRedeploy,
        categoryId: await resolveCategoryID(categoryId, categoryName),
        ...(isGroup ? { members } : {}),
        ...(isProxy
          ? {
              remoteUrl,
              remoteUsername,
              metadataTtlSeconds: metadataMinutes * 60,
              negativeTtlSeconds: negativeMinutes * 60,
              cacheRetentionDays: retentionDays,
              ...(remotePassword ? { remotePassword } : {}),
            }
          : {}),
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['repositories'] })
      await queryClient.invalidateQueries({ queryKey: ['categories'] })
      setRemotePassword('')
      setCategoryName('')
      setOpen(false)
    },
  })

  const purge = useMutation({
    mutationFn: () => del(`/repositories/${repository.name}/cache`),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['repositories'] })
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button type="button" className={iconAction} aria-label={`Settings for ${repository.name}`}>
          <Settings2 className="size-3.5" />
        </button>
      </DialogTrigger>
      <DialogContent>
        <DialogForm
          onSubmit={(event: FormEvent) => {
            event.preventDefault()
            mutation.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>{repository.name}</DialogTitle>
            <DialogDescription>
              {isProxy
                ? 'Proxy repository settings'
                : isGroup
                  ? 'Group repository settings'
                  : 'Repository settings'}
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
            {purge.error ? <ErrorBlock message={(purge.error as ApiError).message} /> : null}
            {isGroup ? (
              <MemberField members={members} onChange={setMembers} candidates={p2Repositories} />
            ) : null}
            <CategoryField
              value={categoryId}
              onChange={setCategoryId}
              draftName={categoryName}
              onDraftNameChange={setCategoryName}
            />
            {isProxy ? (
              <>
                <Field label="Remote URL">
                  <Input
                    value={remoteUrl}
                    onChange={(event) => setRemoteUrl(event.target.value)}
                    className="font-mono"
                  />
                </Field>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Remote username">
                    <Input
                      value={remoteUsername}
                      onChange={(event) => setRemoteUsername(event.target.value)}
                      autoComplete="off"
                    />
                  </Field>
                  <Field label="Remote password" hint="Leave empty to keep the stored one.">
                    <Input
                      type="password"
                      value={remotePassword}
                      onChange={(event) => setRemotePassword(event.target.value)}
                      autoComplete="new-password"
                    />
                  </Field>
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Metadata cache" hint="Minutes before metadata is rechecked.">
                    <Input
                      type="number"
                      min={0}
                      value={metadataMinutes}
                      onChange={(event) => setMetadataMinutes(Number(event.target.value))}
                    />
                  </Field>
                  <Field label="Not-found cache" hint="Minutes a missing path is remembered.">
                    <Input
                      type="number"
                      min={0}
                      value={negativeMinutes}
                      onChange={(event) => setNegativeMinutes(Number(event.target.value))}
                    />
                  </Field>
                </div>
                <Field
                  label="Evict after"
                  hint="Days without a download before a cached file is deleted. 0 keeps everything."
                >
                  <Input
                    type="number"
                    min={0}
                    value={retentionDays}
                    onChange={(event) => setRetentionDays(Number(event.target.value))}
                  />
                </Field>
              </>
            ) : null}
            <Field label="Version policy">
              <Select value={policy} onChange={(event) => setPolicy(event.target.value as Policy)}>
                <option value="release">Release only</option>
                <option value="snapshot">Snapshot only</option>
                <option value="mixed">Mixed</option>
              </Select>
            </Field>
            <Field label="Visibility">
              <Select
                value={visibility}
                onChange={(event) => setVisibility(event.target.value as Visibility)}
              >
                <option value="private">Private</option>
                <option value="public">Public</option>
              </Select>
            </Field>
            <Field label="Description">
              <Textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </Field>
            {allowsRedeploy ? (
              <label className="flex items-start gap-3 rounded-2xl bg-muted p-4 text-sm">
                <Checkbox
                  checked={allowRedeploy}
                  onChange={(event) => setAllowRedeploy(event.target.checked)}
                  className="mt-0.5"
                />
                <span>
                  {repository.format === 'docker'
                    ? 'Allow tags to be moved to another image'
                    : 'Allow overwriting existing releases'}
                  <span className="mt-0.5 block text-xs text-muted-foreground">
                    {repository.format === 'docker'
                      ? 'Normal Docker practice, since a tag like latest is meant to move. Turn it off to make every tag here permanent.'
                      : 'Off by default so a published version can never change underneath a build.'}
                  </span>
                </span>
              </label>
            ) : null}
          </DialogBody>
          <DialogFooter>
            {isProxy ? (
              <Button
                type="button"
                variant="outline"
                disabled={purge.isPending}
                onClick={() => purge.mutate()}
              >
                {purge.isSuccess ? 'Cache purged' : 'Purge cache'}
              </Button>
            ) : null}
            <Button
              type="submit"
              disabled={
                (categoryId === NEW_CATEGORY && !categoryName.trim()) || mutation.isPending
              }
            >
              Save changes
            </Button>
          </DialogFooter>
        </DialogForm>
      </DialogContent>
    </Dialog>
  )
}

function GrantsDialog({ repository }: { repository: Repository }) {
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<Record<string, Permission | 'none'>>({})
  const queryClient = useQueryClient()

  const users = useQuery({
    queryKey: ['users'],
    queryFn: () => api<{ users: ManagedUser[] }>('/users'),
    enabled: open,
  })

  const grants = useQuery({
    queryKey: ['grants', repository.name],
    queryFn: () => api<{ grants: Grant[] }>(`/repositories/${repository.name}/grants`),
    enabled: open,
  })

  const members = (users.data?.users ?? []).filter((user) => user.role !== 'admin')
  const current: Record<string, Permission | 'none'> = {}
  for (const user of members) {
    current[user.id] =
      draft[user.id] ??
      grants.data?.grants.find((grant) => grant.userId === user.id)?.permission ??
      'none'
  }

  const mutation = useMutation({
    mutationFn: () =>
      put(`/repositories/${repository.name}/grants`, {
        grants: Object.entries(current)
          .filter(([, permission]) => permission !== 'none')
          .map(([userId, permission]) => ({ userId, permission })),
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['grants', repository.name] })
      setOpen(false)
      setDraft({})
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button
          type="button"
          className={iconAction}
          aria-label={`Permissions for ${repository.name}`}
        >
          <Users className="size-3.5" />
        </button>
      </DialogTrigger>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{repository.name}</DialogTitle>
          <DialogDescription>
            Administrators always have full access.
            {repository.visibility === 'public'
              ? ' This repository is public, so anyone can already read it.'
              : ''}
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          {users.isLoading || grants.isLoading ? (
            <LoadingBlock label="Loading users" />
          ) : members.length === 0 ? (
            <p className="py-6 text-center text-sm text-muted-foreground">
              No non-administrator users to grant access to yet.
            </p>
          ) : (
            <div className="max-h-80 space-y-1 overflow-y-auto scrollbar-thin rounded-2xl bg-muted p-2">
              {members.map((user) => (
                <div key={user.id} className="flex items-center justify-between gap-4 rounded-xl bg-card px-3 py-2">
                  <div className="min-w-0">
                    <p className="truncate text-sm">{user.name}</p>
                    <p className="truncate font-mono text-xs text-muted-foreground">{user.email}</p>
                  </div>
                  <Select
                    className="h-8 w-32 shrink-0 text-xs"
                    value={current[user.id] ?? 'none'}
                    onChange={(event) =>
                      setDraft((previous) => ({
                        ...previous,
                        [user.id]: event.target.value as Permission | 'none',
                      }))
                    }
                  >
                    <option value="none">No access</option>
                    <option value="read">Read</option>
                    <option value="write">Read and publish</option>
                    <option value="admin">Full access</option>
                  </Select>
                </div>
              ))}
            </div>
          )}
        </DialogBody>
        <DialogFooter>
          <Button onClick={() => mutation.mutate()} disabled={mutation.isPending}>
            Save permissions
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function DeleteDialog({ repository }: { repository: Repository }) {
  const [open, setOpen] = useState(false)
  const [confirmation, setConfirmation] = useState('')
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => del(`/repositories/${repository.name}`),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['repositories'] })
      setOpen(false)
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button
          type="button"
          className="inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-destructive"
          aria-label={`Delete ${repository.name}`}
        >
          <Trash2 className="size-3.5" />
        </button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete {repository.name}</DialogTitle>
          <DialogDescription>
            Every artifact and file in this repository is removed and cannot be recovered.
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
          <Field label={`Type ${repository.name} to confirm`}>
            <Input
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              className="font-mono"
            />
          </Field>
        </DialogBody>
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={confirmation !== repository.name || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            Delete repository
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function AdminRepositoriesRoute() {
  const { user } = useSession()
  const { data, error, isLoading } = useQuery({
    queryKey: ['repositories'],
    queryFn: () => api<{ repositories: Repository[] }>('/repositories'),
  })

  if (user?.role !== 'admin') return <ErrorBlock message="Administrator access required" />
  if (isLoading) return <LoadingBlock label="Loading repositories" />
  if (error) return <ErrorBlock message={(error as Error).message} />

  return (
    <>
      <PageHeading
        eyebrow="Manage"
        title="Repositories"
        description="Create repositories and control who can read and publish."
        actions={<CreateRepositoryDialog />}
      />

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="w-36">Category</TableHead>
              <TableHead className="w-24">Format</TableHead>
              <TableHead className="w-28">Type</TableHead>
              <TableHead className="w-32">Policy</TableHead>
              <TableHead className="w-32">Visibility</TableHead>
              <TableHead className="w-28">Artifacts</TableHead>
              <TableHead className="w-32" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {(data?.repositories ?? []).map((repository) => (
              <TableRow key={repository.id}>
                <TableCell className="font-mono text-[13px]">
                  {repository.name}
                  {repository.type === 'proxy' && repository.remoteUrl ? (
                    <span className="mt-0.5 block truncate text-xs text-muted-foreground">
                      {repository.remoteUrl}
                    </span>
                  ) : null}
                </TableCell>
                <TableCell className="text-[13px] text-muted-foreground">
                  {repository.categoryName || <span className="text-faint">None</span>}
                </TableCell>
                <TableCell>
                  <FormatBadge format={repository.format} />
                </TableCell>
                <TableCell>
                  <TypeBadge type={repository.type} />
                </TableCell>
                <TableCell>
                  <PolicyBadge policy={repository.policy} />
                </TableCell>
                <TableCell>
                  <Badge variant={repository.visibility === 'public' ? 'primary' : 'neutral'}>
                    {repository.visibility}
                  </Badge>
                </TableCell>
                <TableCell className="font-mono text-xs tabular-nums text-muted-foreground">
                  {repository.components ?? 0}
                </TableCell>
                <TableCell>
                  <div className="flex items-center justify-end gap-0.5">
                    <GrantsDialog repository={repository} />
                    <SettingsDialog repository={repository} />
                    <DeleteDialog repository={repository} />
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </>
  )
}
