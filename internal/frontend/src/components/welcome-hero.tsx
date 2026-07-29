import { BrandSplotch, Logo } from '@/components/brand'
import { Markdown } from '@/lib/markdown'
import { useSession } from '@/lib/session'

export function WelcomeHero() {
  const { instanceName, welcomeText } = useSession()

  if (!welcomeText.trim()) return null

  return (
    <section className="relative overflow-hidden rounded-3xl bg-panel px-7 py-8 text-panel-foreground sm:px-10 sm:py-11">
      <BrandSplotch className="left-0 top-0 opacity-70 dark:opacity-40" />
      <div className="relative space-y-6">
        <div className="flex items-center gap-2.5">
          <Logo className="size-7 text-panel-foreground" />
          <span className="text-[13px] font-medium text-panel-muted">{instanceName}</span>
        </div>
        <Markdown source={welcomeText} className="max-w-2xl" />
      </div>
    </section>
  )
}
