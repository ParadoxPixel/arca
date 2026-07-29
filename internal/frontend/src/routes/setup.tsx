import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowLeft, ArrowRight, Check } from 'lucide-react'
import { BrandSplotch, Mark } from '@/components/brand'
import { Button } from '@/components/ui/button'
import { Checkbox, Field, Input, Select } from '@/components/ui/input'
import { ErrorBlock } from '@/components/ui/feedback'
import { ThemeColorField, isThemeColor } from '@/components/theme-color-field'
import { WelcomeEditor } from '@/components/welcome-editor'
import { ApiError, post } from '@/lib/api'
import { useRefreshSession, useSession } from '@/lib/session'
import { cn } from '@/lib/utils'

const STEPS = [
  {
    title: 'Instance',
    blurb: 'Name this deployment and pick the accent it is branded with.',
  },
  {
    title: 'Welcome',
    blurb: 'The hero on the landing page. Write what this instance is for, in markdown.',
  },
  {
    title: 'Administrator',
    blurb: 'This account gets full access. You can add more people afterwards.',
  },
  { title: 'Repositories', blurb: 'Optionally create a starting set of Maven and npm repositories.' },
] as const

export function SetupRoute() {
  const session = useSession()
  const [step, setStep] = useState(0)
  const [instanceName, setInstanceName] = useState('arca')
  const [themeColor, setThemeColor] = useState('#4f46e5')
  const [welcomeText, setWelcomeText] = useState(session.welcomeText)
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [seedRepositories, setSeedRepositories] = useState(true)
  const [publicByDefault, setPublicByDefault] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const refresh = useRefreshSession()
  const navigate = useNavigate()

  useEffect(() => {
    if (isThemeColor(themeColor)) document.documentElement.style.setProperty('--brand', themeColor)
  }, [themeColor])

  const stepValid =
    step === 0
      ? instanceName.trim().length > 0 && isThemeColor(themeColor)
      : step === 2
        ? name.trim().length > 0 &&
          /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email) &&
          password.length >= 10 &&
          password === confirmation
        : true

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (step < STEPS.length - 1) {
      if (stepValid) setStep(step + 1)
      return
    }

    setBusy(true)
    setError(null)
    try {
      await post('/setup', {
        instanceName: instanceName.trim(),
        themeColor,
        welcomeText,
        name: name.trim(),
        email: email.trim().toLowerCase(),
        password,
        seedRepositories,
        publicByDefault,
      })
      await refresh()
      navigate('/', { replace: true })
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : 'Setup could not be completed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid min-h-dvh gap-3 p-3 lg:grid-cols-[24rem_1fr]">
      <aside className="relative flex flex-col justify-between gap-10 overflow-hidden rounded-3xl bg-panel px-8 py-10 text-panel-foreground">
        <BrandSplotch className="left-full top-0 opacity-60 dark:opacity-50" />
        <div className="relative space-y-7">
          <Mark className="size-10 text-panel-foreground" />
          <div className="space-y-2.5">
            <p className="text-[13px] font-medium text-panel-muted">First run</p>
            <h1 className="text-3xl font-semibold">Set up your repository server</h1>
          </div>
        </div>

        <ol className="relative space-y-1.5">
          {STEPS.map((entry, index) => (
            <li
              key={entry.title}
              className={cn(
                'flex items-center gap-3 rounded-xl px-4 py-2.5 transition-colors',
                index === step ? 'bg-panel-accent text-panel-foreground' : 'text-panel-muted',
              )}
            >
              <span className="text-xs tabular-nums opacity-70">{index + 1}</span>
              <span className="text-sm">{entry.title}</span>
              {index < step ? (
                <Check className="ml-auto size-3.5 text-panel-foreground opacity-80" />
              ) : null}
            </li>
          ))}
        </ol>

        <p className="relative max-w-xs text-[13px] leading-relaxed text-panel-muted">
          Artifacts are stored on disk and metadata in SQLite. There is nothing else to configure
          once this is finished.
        </p>
      </aside>

      <main className="flex items-center px-2 py-10 sm:px-8 lg:px-16">
        <form
          onSubmit={submit}
          className={cn('w-full space-y-7', step === 1 ? 'max-w-2xl' : 'max-w-md')}
        >
          <div className="space-y-1.5">
            <p className="text-[13px] font-medium text-primary">
              Step {step + 1} of {STEPS.length}
            </p>
            <h2 className="text-2xl font-semibold">{STEPS[step]!.title}</h2>
            <p className="text-[15px] text-muted-foreground">{STEPS[step]!.blurb}</p>
          </div>

          {error ? <ErrorBlock message={error} /> : null}

          {step === 0 ? (
            <div className="space-y-5">
              <Field label="Instance name">
                <Input
                  value={instanceName}
                  onChange={(event) => setInstanceName(event.target.value)}
                  placeholder="acme artifacts"
                  autoFocus
                />
              </Field>
              <ThemeColorField
                value={themeColor}
                onChange={setThemeColor}
                hint="Branding for this deployment. It shows as the accent behind the navigation, and can be changed later under Manage."
              />
            </div>
          ) : null}

          {step === 1 ? (
            <WelcomeEditor value={welcomeText} onChange={setWelcomeText} rows={14} />
          ) : null}

          {step === 2 ? (
            <div className="space-y-5">
              <Field label="Full name">
                <Input
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  autoComplete="name"
                  autoFocus
                />
              </Field>
              <Field label="Email" hint="Used to sign in, and as the Maven username.">
                <Input
                  type="email"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  autoComplete="username"
                  className="font-mono"
                />
              </Field>
              <Field label="Password" hint="At least 10 characters.">
                <Input
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  autoComplete="new-password"
                />
              </Field>
              <Field label="Confirm password">
                <Input
                  type="password"
                  value={confirmation}
                  onChange={(event) => setConfirmation(event.target.value)}
                  autoComplete="new-password"
                />
              </Field>
              {password && confirmation && password !== confirmation ? (
                <p className="text-xs font-medium text-destructive">Passwords do not match</p>
              ) : null}
            </div>
          ) : null}

          {step === 3 ? (
            <div className="space-y-5">
              <label className="flex items-start gap-3 rounded-2xl bg-muted p-4">
                <Checkbox
                  checked={seedRepositories}
                  onChange={(event) => setSeedRepositories(event.target.checked)}
                  className="mt-0.5"
                />
                <span className="space-y-1">
                  <span className="block text-sm font-medium">Create starter repositories</span>
                  <span className="block font-mono text-xs text-muted-foreground">
                    maven-releases · maven-snapshots · npm-hosted
                  </span>
                </span>
              </label>

              <Field
                label="Default visibility"
                hint="Public repositories can be read without credentials."
              >
                <Select
                  value={publicByDefault ? 'public' : 'private'}
                  onChange={(event) => setPublicByDefault(event.target.value === 'public')}
                  disabled={!seedRepositories}
                >
                  <option value="private">Private</option>
                  <option value="public">Public</option>
                </Select>
              </Field>
            </div>
          ) : null}

          <div className="flex items-center justify-between pt-2">
            <Button
              type="button"
              variant="ghost"
              onClick={() => setStep(Math.max(0, step - 1))}
              disabled={step === 0 || busy}
            >
              <ArrowLeft /> Back
            </Button>
            <Button type="submit" disabled={!stepValid || busy}>
              {step === STEPS.length - 1 ? (busy ? 'Finishing' : 'Finish setup') : 'Continue'}
              {step === STEPS.length - 1 ? null : <ArrowRight />}
            </Button>
          </div>
        </form>
      </main>
    </div>
  )
}
