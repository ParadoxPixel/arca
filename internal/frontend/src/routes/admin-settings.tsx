import { useEffect, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Field, Input } from '@/components/ui/input'
import { ErrorBlock, LoadingBlock, PageHeading } from '@/components/ui/feedback'
import { ThemeColorField, isThemeColor } from '@/components/theme-color-field'
import { ApiError, api, patch, type InstanceSettings } from '@/lib/api'
import { useSession } from '@/lib/session'

export function AdminSettingsRoute() {
  const { user, themeColor } = useSession()
  const queryClient = useQueryClient()

  const { data, error, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: () => api<InstanceSettings>('/admin/settings'),
    enabled: user?.role === 'admin',
  })

  const [instanceName, setInstanceName] = useState('')
  const [color, setColor] = useState('')
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (!data) return
    setInstanceName(data.instanceName)
    setColor(data.themeColor)
  }, [data])

  // Previewing on the live document is the only honest way to judge an accent
  // that only ever appears behind the chrome. The saved value comes back when
  // the page is left.
  useEffect(() => {
    if (!isThemeColor(color)) return
    document.documentElement.style.setProperty('--brand', color)
  }, [color])

  useEffect(
    () => () => document.documentElement.style.setProperty('--brand', themeColor),
    [themeColor],
  )

  const mutation = useMutation({
    mutationFn: () => patch<InstanceSettings>('/admin/settings', { instanceName, themeColor: color }),
    onSuccess: async (settings) => {
      queryClient.setQueryData(['settings'], settings)
      await queryClient.invalidateQueries({ queryKey: ['bootstrap'] })
      setSaved(true)
    },
  })

  if (user?.role !== 'admin') return <ErrorBlock message="Administrator access required" />
  if (isLoading) return <LoadingBlock label="Loading settings" />
  if (error) return <ErrorBlock message={(error as Error).message} />

  const changed = data ? instanceName !== data.instanceName || color !== data.themeColor : false
  const valid = instanceName.trim().length > 0 && isThemeColor(color)

  return (
    <>
      <PageHeading
        eyebrow="Manage"
        title="Settings"
        description="Name this instance and pick the accent it is branded with."
      />

      <Card className="p-6">
        <form
          className="space-y-6"
          onSubmit={(event: FormEvent) => {
            event.preventDefault()
            mutation.mutate()
          }}
        >
          {mutation.error ? <ErrorBlock message={(mutation.error as ApiError).message} /> : null}

          <Field label="Instance name" hint="Shown in the sidebar, the browser tab and Maven snippets.">
            <Input
              value={instanceName}
              onChange={(event) => {
                setInstanceName(event.target.value)
                setSaved(false)
              }}
              maxLength={80}
            />
          </Field>

          <ThemeColorField
            value={color}
            onChange={(value) => {
              setColor(value)
              setSaved(false)
            }}
          />

          <div className="flex items-center gap-3">
            <Button type="submit" disabled={!changed || !valid || mutation.isPending}>
              {mutation.isPending ? 'Saving' : 'Save changes'}
            </Button>
            {saved && !changed ? (
              <span className="text-[13px] text-muted-foreground">Saved</span>
            ) : null}
          </div>
        </form>
      </Card>
    </>
  )
}
