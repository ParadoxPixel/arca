import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle } from '@/components/ui/card'
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
import { Field, Input, Select } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { EmptyState, ErrorBlock, LoadingBlock, PageHeading } from '@/components/ui/feedback'
import { CodeBlock } from '@/components/copy-button'
import { ApiError, api, del, post, type ApiToken } from '@/lib/api'
import { useSession } from '@/lib/session'
import { formatDate, formatRelative } from '@/lib/utils'

function CreateTokenDialog() {
  const { instanceName } = useSession()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [expiresInDays, setExpiresInDays] = useState('0')
  const [issued, setIssued] = useState<{ token: string; username: string } | null>(null)
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () =>
      post<{ token: string; username: string }>('/tokens', {
        name,
        expiresInDays: Number(expiresInDays),
      }),
    onSuccess: async (result) => {
      setIssued(result)
      setName('')
      await queryClient.invalidateQueries({ queryKey: ['tokens'] })
    },
  })

  const close = (next: boolean) => {
    setOpen(next)
    if (!next) {
      setIssued(null)
      mutation.reset()
    }
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus /> New token
        </Button>
      </DialogTrigger>
      <DialogContent>
        {issued ? (
          <>
            <DialogHeader>
              <DialogTitle>Token created</DialogTitle>
              <DialogDescription>
                Copy it now. Only a hash is stored, so it cannot be shown again.
              </DialogDescription>
            </DialogHeader>
            <DialogBody>
              <CodeBlock code={issued.token} language="token" />
              <CodeBlock
                language="~/.m2/settings.xml"
                code={`<server>
  <id>${instanceName}</id>
  <username>${issued.username}</username>
  <password>${issued.token}</password>
</server>`}
              />
            </DialogBody>
            <DialogFooter>
              <Button onClick={() => close(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <DialogForm
            onSubmit={(event: FormEvent) => {
              event.preventDefault()
              mutation.mutate()
            }}
          >
            <DialogHeader>
              <DialogTitle>New API token</DialogTitle>
              <DialogDescription>
                Use it as the Maven password with your email as the username, or as _authToken for npm.
              </DialogDescription>
            </DialogHeader>
            <DialogBody>
              {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
              <Field label="Name" hint="Something that identifies where it is used.">
                <Input
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="ci-pipeline"
                  autoFocus
                />
              </Field>
              <Field label="Expiry">
                <Select
                  value={expiresInDays}
                  onChange={(event) => setExpiresInDays(event.target.value)}
                >
                  <option value="0">Never</option>
                  <option value="30">30 days</option>
                  <option value="90">90 days</option>
                  <option value="365">1 year</option>
                </Select>
              </Field>
            </DialogBody>
            <DialogFooter>
              <Button type="submit" disabled={!name.trim() || mutation.isPending}>
                Create token
              </Button>
            </DialogFooter>
          </DialogForm>
        )}
      </DialogContent>
    </Dialog>
  )
}

function Tokens() {
  const queryClient = useQueryClient()
  const { data, error, isLoading } = useQuery({
    queryKey: ['tokens'],
    queryFn: () => api<{ tokens: ApiToken[] }>('/tokens'),
  })

  const revoke = useMutation({
    mutationFn: (id: string) => del(`/tokens/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tokens'] }),
  })

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle>API tokens</CardTitle>
        <CreateTokenDialog />
      </CardHeader>
      {isLoading ? (
        <LoadingBlock label="Loading tokens" />
      ) : error ? (
        <div className="px-6 pb-6">
          <ErrorBlock message={(error as Error).message} />
        </div>
      ) : (data?.tokens.length ?? 0) === 0 ? (
        <div className="px-6 pb-6">
          <EmptyState
            title="No tokens yet"
            description="Create one to publish artifacts or read private repositories from a build."
          />
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="w-40">Prefix</TableHead>
              <TableHead className="w-36">Last used</TableHead>
              <TableHead className="w-32">Expires</TableHead>
              <TableHead className="w-12" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {data!.tokens.map((token) => (
              <TableRow key={token.id}>
                <TableCell className="font-medium">{token.name}</TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  {token.preview}…
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {formatRelative(token.last_used_at)}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {token.expires_at ? formatDate(token.expires_at) : 'never'}
                </TableCell>
                <TableCell>
                  <button
                    type="button"
                    aria-label={`Revoke ${token.name}`}
                    onClick={() => revoke.mutate(token.id)}
                    className="inline-flex size-6 items-center justify-center text-faint transition-colors hover:bg-accent hover:text-destructive"
                  >
                    <Trash2 className="size-3" />
                  </button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Card>
  )
}

function ChangePassword() {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [done, setDone] = useState(false)

  const mutation = useMutation({
    mutationFn: () => post('/auth/password', { currentPassword, newPassword }),
    onSuccess: () => {
      setCurrentPassword('')
      setNewPassword('')
      setDone(true)
    },
  })

  return (
    <Card className="max-w-md">
      <form
        onSubmit={(event: FormEvent) => {
          event.preventDefault()
          setDone(false)
          mutation.mutate()
        }}
      >
        <CardHeader>
          <CardTitle>Password</CardTitle>
        </CardHeader>
        <div className="space-y-5 px-6 pb-6">
          <p className="text-sm text-muted-foreground">
            Changing your password signs out every other session.
          </p>
          {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
          {done ? <p className="text-[13px] font-medium text-release">Password updated</p> : null}
          <Field label="Current password">
            <Input
              type="password"
              value={currentPassword}
              onChange={(event) => setCurrentPassword(event.target.value)}
              autoComplete="current-password"
            />
          </Field>
          <Field label="New password" hint="At least 10 characters.">
            <Input
              type="password"
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
              autoComplete="new-password"
            />
          </Field>
          <Button
            type="submit"
            disabled={!currentPassword || newPassword.length < 10 || mutation.isPending}
          >
            Update password
          </Button>
        </div>
      </form>
    </Card>
  )
}

export function AccountRoute() {
  const { user } = useSession()
  if (!user) return <ErrorBlock message="Sign in to manage your account" />

  return (
    <>
      <PageHeading
        eyebrow={user.role === 'admin' ? 'Administrator' : 'Account'}
        title={user.name}
        description={user.email}
      />
      <Tokens />
      <ChangePassword />
    </>
  )
}
