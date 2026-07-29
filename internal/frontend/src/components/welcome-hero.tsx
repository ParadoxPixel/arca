import { BrandSplotch, Logo } from '@/components/brand'
import { Markdown } from '@/lib/markdown'
import { useSession } from '@/lib/session'

export function WelcomeHero() {
  const { instanceName, welcomeText } = useSession()

  if (!welcomeText.trim()) return null

  return (
    <section className="relative overflow-hidden rounded-3xl bg-muted px-7 py-8 text-foreground sm:px-10 sm:py-11">
      <BrandSplotch className="left-full top-0 opacity-15 dark:opacity-25" />
      <div className="relative space-y-5">
        <div className="flex items-center gap-2.5">
          <Logo className="size-6 text-foreground" />
          <span className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
            {instanceName}
          </span>
        </div>
        <Markdown source={welcomeText} className="max-w-2xl" />
      </div>
    </section>
  )
}
