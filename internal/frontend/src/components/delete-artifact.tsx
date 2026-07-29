import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Field, Input } from '@/components/ui/input'
import { ErrorBlock } from '@/components/ui/feedback'
import { ApiError, del } from '@/lib/api'

interface Props {
  repository: string
  namespace: string
  name: string
  version?: string
  label?: string
  onDeleted?: () => void
}

export function DeleteArtifactDialog({
  repository,
  namespace,
  name,
  version,
  label,
  onDeleted,
}: Props) {
  const [open, setOpen] = useState(false)
  const [confirmation, setConfirmation] = useState('')
  const queryClient = useQueryClient()

  const query = new URLSearchParams({ namespace, name })
  if (version) query.set('version', version)

  const mutation = useMutation({
    mutationFn: () =>
      del<{ versions: number; files: number }>(`/repositories/${repository}/artifacts?${query}`),
    onSuccess: async () => {
      await queryClient.invalidateQueries()
      setOpen(false)
      setConfirmation('')
      onDeleted?.()
    },
  })

  const target = version ? `${name} ${version}` : name
  const confirmed = version ? true : confirmation === name

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setConfirmation('')
      }}
    >
      <DialogTrigger asChild>
        {label ? (
          <button
            type="button"
            className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-destructive"
          >
            <Trash2 className="size-3" />
            {label}
          </button>
        ) : (
          <button
            type="button"
            className="inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-destructive"
            aria-label={`Delete ${target}`}
          >
            <Trash2 className="size-3.5" />
          </button>
        )}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete {target}</DialogTitle>
          <DialogDescription>
            {version
              ? 'Every file published under this version is removed from the repository and from disk.'
              : 'Every version of this artifact is removed from the repository and from disk.'}{' '}
            Builds resolving it will start failing.
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}
          <div className="rounded-2xl bg-muted px-4 py-3 font-mono text-[13px]">
            <span className="text-muted-foreground">{repository}</span>
            <span className="text-faint"> / </span>
            <span className="text-muted-foreground">{namespace}</span>
            <span className="text-faint">:</span>
            <span>{name}</span>
            {version ? (
              <>
                <span className="text-faint">:</span>
                <span>{version}</span>
              </>
            ) : null}
          </div>
          {version ? null : (
            <Field label={`Type ${name} to confirm`}>
              <Input
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
                className="font-mono"
                autoFocus
              />
            </Field>
          )}
        </DialogBody>
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={!confirmed || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {version ? 'Delete version' : 'Delete every version'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
