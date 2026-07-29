import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, CircleCheck, CircleDashed, Loader2, Minus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, Input, Select } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { ErrorBlock, PageHeading, Spinner } from '@/components/ui/feedback'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import {
  ApiError,
  api,
  del,
  post,
  type MigrationPreview,
  type MigrationRepository,
  type MigrationRequest,
  type MigrationStatus,
  type RepositoryFormat,
  type Visibility,
} from '@/lib/api'
import { useSession } from '@/lib/session'

const emptyForm: MigrationRequest = {
  url: '',
  username: '',
  password: '',
  visibility: 'private',
  rawFormat: 'p2',
}

function StateIcon({ state }: { state: MigrationRepository['state'] }) {
  switch (state) {
    case 'running':
      return <Loader2 className="size-4 animate-spin text-primary" />
    case 'done':
      return <CircleCheck className="size-4 text-release" />
    case 'failed':
      return <CircleAlert className="size-4 text-destructive" />
    case 'skipped':
      return <Minus className="size-4 text-faint" />
    default:
      return <CircleDashed className="size-4 text-faint" />
  }
}

function PlanTable({ repositories, live }: { repositories: MigrationRepository[]; live: boolean }) {
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            {live ? <TableHead className="w-10" /> : null}
            <TableHead>Repository</TableHead>
            <TableHead className="w-56">Becomes</TableHead>
            <TableHead>{live ? 'Progress' : 'Notes'}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {repositories.map((entry) => (
            <TableRow key={entry.name}>
              {live ? (
                <TableCell>
                  <StateIcon state={entry.state} />
                </TableCell>
              ) : null}
              <TableCell>
                <p className="font-mono text-[13px]">{entry.name}</p>
                {entry.source !== entry.name ? (
                  <p className="text-xs text-faint">from {entry.source}</p>
                ) : null}
              </TableCell>
              <TableCell>
                {entry.action === 'skip' ? (
                  <Badge variant="neutral">skipped</Badge>
                ) : (
                  <span className="flex flex-wrap items-center gap-1.5">
                    <Badge variant="primary">{entry.format}</Badge>
                    <Badge variant="neutral">{entry.type}</Badge>
                    {entry.existed ? <Badge variant="neutral">already existed</Badge> : null}
                  </span>
                )}
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {entry.action === 'skip' ? (
                  entry.reason
                ) : live ? (
                  <Progress entry={entry} />
                ) : entry.copyingAssets ? (
                  'Assets are copied across'
                ) : (
                  'Recreated pointing at the same upstream, cache not copied'
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function Progress({ entry }: { entry: MigrationRepository }) {
  if (!entry.copyingAssets) return <>Cache not copied</>
  if (entry.state === 'pending') return <>Waiting</>

  return (
    <span className="tabular-nums">
      {entry.copied} copied
      {entry.present > 0 ? `, ${entry.present} already present` : ''}
      {entry.failed > 0 ? `, ${entry.failed} failed` : ''}
    </span>
  )
}

function RunSummary({ status }: { status: MigrationStatus }) {
  const totals = status.repositories.reduce(
    (sum, entry) => ({
      copied: sum.copied + entry.copied,
      present: sum.present + entry.present,
      failed: sum.failed + entry.failed,
    }),
    { copied: 0, present: 0, failed: 0 },
  )

  return (
    <p className="text-sm text-muted-foreground">
      {totals.copied} files copied, {totals.present} already present
      {totals.failed > 0 ? `, ${totals.failed} failed` : ''}
      {status.current ? `, currently slurping ${status.current}...` : ''}
    </p>
  )
}

function MigrationCard() {
  const queryClient = useQueryClient()
  const [form, setForm] = useState<MigrationRequest>(emptyForm)
  const [preview, setPreview] = useState<MigrationRepository[] | null>(null)

  const { data: status } = useQuery({
    queryKey: ['migration'],
    queryFn: () => api<MigrationStatus>('/admin/migration'),
    refetchInterval: (query) => (query.state.data?.state === 'running' ? 1000 : false),
  })

  const running = status?.state === 'running'

  // A migration creates repositories behind the rest of the app, so the lists
  // it fed are refreshed once the run stops.
  const wasRunning = useRef(false)
  useEffect(() => {
    if (wasRunning.current && !running) {
      queryClient.invalidateQueries({ queryKey: ['repositories'] })
      queryClient.invalidateQueries({ queryKey: ['categories'] })
    }
    wasRunning.current = running
  }, [running, queryClient])

  function update(patch: Partial<MigrationRequest>) {
    setForm((current) => ({ ...current, ...patch }))
    setPreview(null)
  }

  const previewMutation = useMutation({
    mutationFn: () => post<MigrationPreview>('/admin/migration/preview', form),
    onSuccess: (result) => setPreview(result.repositories),
  })

  const startMutation = useMutation({
    mutationFn: () => post<MigrationStatus>('/admin/migration', form),
    onSuccess: (started) => {
      queryClient.setQueryData(['migration'], started)
      setPreview(null)
    },
  })

  const cancelMutation = useMutation({
    mutationFn: () => del('/admin/migration'),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['migration'] }),
  })

  function submit(event: FormEvent) {
    event.preventDefault()
    previewMutation.mutate()
  }

  const error = (previewMutation.error ?? startMutation.error) as ApiError | null
  const ready = form.url.trim().length > 0

  return (
    <Card>
      <CardHeader>
        <CardTitle>Migrate from Nexus</CardTitle>
        <CardDescription>
          Copies repositories out of a Sonatype Nexus instance. It only ever reads from Nexus, so
          nothing there is created, changed or deleted. Preview first: nothing is written here until
          you start the migration.
        </CardDescription>
      </CardHeader>

      <div className="space-y-6 px-6 pb-6">
        <form className="space-y-4" onSubmit={submit}>
          <Field label="Nexus URL" hint="The base URL, without a repository path.">
            <Input
              value={form.url}
              onChange={(event) => update({ url: event.target.value })}
              placeholder="https://nexus.example.com"
              className="font-mono"
              disabled={running}
            />
          </Field>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Username" hint="Needs read access only.">
              <Input
                value={form.username}
                onChange={(event) => update({ username: event.target.value })}
                autoComplete="off"
                disabled={running}
              />
            </Field>
            <Field label="Password" hint="Used for this run and never stored.">
              <Input
                type="password"
                value={form.password}
                onChange={(event) => update({ password: event.target.value })}
                autoComplete="new-password"
                disabled={running}
              />
            </Field>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Visibility" hint="Applied to every repository this creates.">
              <Select
                value={form.visibility}
                onChange={(event) => update({ visibility: event.target.value as Visibility })}
                disabled={running}
              >
                <option value="private">Private, granted per user</option>
                <option value="public">Public, readable without an account</option>
              </Select>
            </Field>
            <Field label="Raw repositories become" hint="Nexus raw repositories usually hold p2 sites.">
              <Select
                value={form.rawFormat}
                onChange={(event) => update({ rawFormat: event.target.value as RepositoryFormat })}
                disabled={running}
              >
                <option value="p2">p2</option>
                <option value="maven2">Maven 2</option>
                <option value="npm">npm</option>
              </Select>
            </Field>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" variant="secondary" disabled={!ready || running || previewMutation.isPending}>
              {previewMutation.isPending ? <Spinner /> : null}
              Preview
            </Button>
            <Button
              type="button"
              disabled={!preview || running || startMutation.isPending}
              onClick={() => startMutation.mutate()}
            >
              Start migration
            </Button>
            {running ? (
              <Button type="button" variant="destructive" onClick={() => cancelMutation.mutate()}>
                Cancel
              </Button>
            ) : null}
          </div>
        </form>

        {error ? <ErrorBlock message={error.message} /> : null}

        {preview ? (
          <div className="space-y-3">
            <p className="text-sm text-muted-foreground">
              {preview.filter((entry) => entry.action === 'create').length} to create,{' '}
              {preview.filter((entry) => entry.action === 'skip').length} skipped. Nothing has been
              written yet.
            </p>
            <PlanTable repositories={preview} live={false} />
          </div>
        ) : null}

        {status && status.state !== 'idle' && !preview ? (
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-3">
              <Badge variant={statusVariant(status.state)}>{status.state}</Badge>
              <RunSummary status={status} />
            </div>
            {status.message ? <p className="text-sm text-muted-foreground">{status.message}</p> : null}
            <PlanTable repositories={status.repositories} live />
            {status.errors.length > 0 ? (
              <details className="rounded-2xl bg-destructive-soft px-5 py-4">
                <summary className="cursor-pointer text-[13px] font-semibold text-destructive">
                  {status.errors.length} transfer{status.errors.length === 1 ? '' : 's'} failed
                </summary>
                <ul className="mt-2 space-y-1 font-mono text-xs text-destructive/90">
                  {status.errors.map((message) => (
                    <li key={message}>{message}</li>
                  ))}
                </ul>
              </details>
            ) : null}
          </div>
        ) : null}
      </div>
    </Card>
  )
}

function statusVariant(state: MigrationStatus['state']) {
  if (state === 'done') return 'release' as const
  if (state === 'failed') return 'danger' as const
  if (state === 'running') return 'primary' as const
  return 'neutral' as const
}

export function AdminMaintenanceRoute() {
  const { user } = useSession()
  if (user?.role !== 'admin') return <ErrorBlock message="Administrator access required" />

  return (
    <>
      <PageHeading
        eyebrow="Manage"
        title="Maintenance"
        description="One-off operations on this instance."
      />
      <MigrationCard />
    </>
  )
}
