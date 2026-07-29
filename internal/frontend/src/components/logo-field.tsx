import { useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ImageUp, Trash2 } from 'lucide-react'
import { Mark } from '@/components/brand'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/input'
import { ApiError, del, upload } from '@/lib/api'
import { useSession } from '@/lib/session'

const maximumBytes = 512 * 1024

export function LogoField() {
  const { instanceName, logoUrl } = useSession()
  const queryClient = useQueryClient()
  const picker = useRef<HTMLInputElement>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['bootstrap'] })

  const send = useMutation({
    mutationFn: (file: File) => upload<{ logoUrl: string }>('/admin/branding/logo', file),
    onSuccess: refresh,
    onError: (cause) =>
      setError(cause instanceof ApiError ? cause.message : 'The logo could not be uploaded'),
  })

  const remove = useMutation({
    mutationFn: () => del<void>('/admin/branding/logo'),
    onSuccess: refresh,
  })

  const choose = (file: File | undefined) => {
    setError(null)
    if (!file) return
    if (file.size > maximumBytes) {
      setError('The logo must be at most 512 KB')
      return
    }
    send.mutate(file)
  }

  return (
    <div className="space-y-1.5">
      <Label>Logo</Label>
      <div className="flex flex-wrap items-center gap-4 rounded-2xl bg-muted p-4">
        <div className="flex size-16 shrink-0 items-center justify-center rounded-2xl bg-card">
          {logoUrl ? (
            <img src={logoUrl} alt={instanceName} className="size-11 object-contain" />
          ) : (
            <Mark className="size-8 text-faint" />
          )}
        </div>

        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => picker.current?.click()}
              disabled={send.isPending}
            >
              <ImageUp /> {logoUrl ? 'Replace' : 'Upload'}
            </Button>
            {logoUrl ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => remove.mutate()}
                disabled={remove.isPending}
              >
                <Trash2 /> Remove
              </Button>
            ) : null}
          </div>
          <p className="text-xs text-muted-foreground">
            PNG, JPEG, GIF, WebP or SVG, up to 512 KB. Shown in the sidebar and on the landing page,
            in place of the default mark.
          </p>
          {error ? <p className="text-xs font-medium text-destructive">{error}</p> : null}
        </div>
      </div>

      <input
        ref={picker}
        type="file"
        accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml"
        className="hidden"
        onChange={(event) => {
          choose(event.target.files?.[0])
          event.target.value = ''
        }}
      />
    </div>
  )
}
