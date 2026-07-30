import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import {
  Activity,
  Boxes,
  ChevronDown,
  FolderTree,
  Globe,
  HardDrive,
  KeyRound,
  Library,
  Lock,
  LogOut,
  Menu,
  Search,
  Settings,
  Shield,
  Wrench,
  Users,
  X,
} from 'lucide-react'
import { BrandSplotch, Logo } from '@/components/brand'
import { Button } from '@/components/ui/button'
import { ThemeToggle } from '@/components/theme-toggle'
import { useRefreshSession, useSession } from '@/lib/session'
import { api, post, type Repository } from '@/lib/api'
import { FormatIcon } from '@/components/ui/format-icon'
import { categoriesQuery, groupRepositories, needsHeadings } from '@/lib/categories'
import { cn } from '@/lib/utils'

const managePages = [
  { to: '/admin/health', label: 'Health', icon: Activity },
  { to: '/admin/storage', label: 'Storage', icon: HardDrive },
  { to: '/admin/artifacts', label: 'Artifacts', icon: Boxes },
  { to: '/admin/repositories', label: 'Repositories', icon: Shield },
  { to: '/admin/categories', label: 'Categories', icon: FolderTree },
  { to: '/admin/users', label: 'Users', icon: Users },
  { to: '/admin/settings', label: 'Settings', icon: Settings },
  { to: '/admin/maintenance', label: 'Maintenance', icon: Wrench },
]

// The mobile sheet closes when a rail link is followed, but only then. Closing
// on any click inside the rail would swallow the Manage fold toggle.
const RailNavigate = createContext<() => void>(() => {})

function RailSection({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1 px-3 py-3">
      <p className="px-3 pb-1 text-xs font-medium text-faint">{label}</p>
      {children}
    </div>
  )
}

function RailLink({ to, end, children }: { to: string; end?: boolean; children: ReactNode }) {
  const navigated = useContext(RailNavigate)

  return (
    <NavLink
      to={to}
      end={end}
      onClick={navigated}
      className={({ isActive }) =>
        cn(
          'flex items-center gap-2 rounded-xl px-3 py-2 text-[13px] transition-colors',
          isActive
            ? 'bg-card font-medium text-foreground shadow-card'
            : 'text-muted-foreground hover:bg-accent/70 hover:text-foreground',
        )
      }
    >
      {children}
    </NavLink>
  )
}

// ManageSection stays folded by default so seven admin pages do not bury the
// repository list an administrator is usually here for.
function ManageSection() {
  const { pathname } = useLocation()
  const [open, setOpen] = useState(() => pathname.startsWith('/admin'))

  return (
    <div className="space-y-1 px-3 py-3">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className="flex w-full items-center gap-1 rounded-lg px-3 pb-1 text-xs font-medium text-faint transition-colors hover:text-muted-foreground"
      >
        <ChevronDown className={cn('size-3 transition-transform', open ? '' : '-rotate-90')} />
        Manage
      </button>
      {open
        ? managePages.map((page) => (
            <RailLink key={page.to} to={page.to}>
              <page.icon className="size-3.5 shrink-0 text-faint" />
              <span className="flex-1 truncate">{page.label}</span>
            </RailLink>
          ))
        : null}
    </div>
  )
}

function RailContent({ onNavigate = () => {} }: { onNavigate?: () => void }) {
  const { instanceName, user } = useSession()
  const { data } = useQuery({
    queryKey: ['repositories'],
    queryFn: () => api<{ repositories: Repository[] }>('/repositories'),
  })
  const categories = useQuery(categoriesQuery)

  const repositories = data?.repositories ?? []
  const groups = groupRepositories(repositories, categories.data?.categories ?? [])
  const headings = needsHeadings(groups)

  return (
    <RailNavigate.Provider value={onNavigate}>
      <div className="flex h-full flex-col">
        <Link
          to="/"
          onClick={onNavigate}
          className="mx-3 mt-4 flex items-center gap-2.5 rounded-xl px-3 py-2.5 transition-colors hover:bg-accent/70"
        >
          <Logo className="size-5" />
          <span className="truncate font-semibold tracking-tight">{instanceName}</span>
        </Link>

        <div className="flex-1 overflow-y-auto scrollbar-thin pb-4">
          <RailSection label="Browse">
            <RailLink to="/" end>
              <Library className="size-3.5 shrink-0 text-faint" />
              <span className="flex-1 truncate">All repositories</span>
            </RailLink>
            <RailLink to="/search">
              <Search className="size-3.5 shrink-0 text-faint" />
              <span className="flex-1 truncate">Search artifacts</span>
            </RailLink>
          </RailSection>

          {groups.map((group) => (
            <RailSection
              key={group.id ?? 'uncategorized'}
              label={headings ? group.name : 'Repositories'}
            >
              {group.repositories.map((repository) => (
                <RailLink key={repository.id} to={`/repositories/${repository.name}`}>
                  <FormatIcon format={repository.format} className="text-faint" />
                  <span className="flex-1 truncate font-mono text-[13px]">{repository.name}</span>
                  {repository.visibility === 'public' ? (
                    <Globe className="size-3.5 shrink-0 text-faint" aria-label="Public" />
                  ) : (
                    <Lock className="size-3.5 shrink-0 text-faint" aria-label="Private" />
                  )}
                </RailLink>
              ))}
            </RailSection>
          ))}
          {data && repositories.length === 0 ? (
            <RailSection label="Repositories">
              <p className="px-3 py-2 text-[13px] text-faint">None visible to you</p>
            </RailSection>
          ) : null}

          {user?.role === 'admin' ? <ManageSection /> : null}

          {user ? (
            <RailSection label="You">
              <RailLink to="/account">
                <KeyRound className="size-3.5 shrink-0 text-faint" />
                <span className="flex-1 truncate">Account and tokens</span>
              </RailLink>
            </RailSection>
          ) : null}
        </div>

        {/*<div className="mx-3 rounded-2xl bg-tint px-4 py-3.5 text-tint-foreground">*/}
        {/*  <p className="text-xs font-medium opacity-80">Repository endpoint</p>*/}
        {/*  <p className="mt-1 break-all font-mono text-[11px] opacity-70">*/}
        {/*    {window.location.origin}/repository/*/}
        {/*  </p>*/}
        {/*</div>*/}

        <BuildStamp />
      </div>
    </RailNavigate.Provider>
  )
}

// BuildStamp says which build is running. The exact time is a title rather than
// visible text, because the version is what anyone actually reads.
function BuildStamp() {
  const { build } = useSession()
  if (!build?.version) return null

  const built = build.buildTime ? new Date(build.buildTime) : null
  const tooltip = [
    build.commit ? `commit ${build.commit}` : null,
    built && !Number.isNaN(built.valueOf()) ? `built ${built.toUTCString()}` : null,
  ]
    .filter(Boolean)
    .join('\n')

  return (
    <a
      title={tooltip || undefined}
      className="mx-3 mb-4 mt-2 px-1 font-mono text-[11px] text-faint"
      href={"https://github.com/pixelib/arca"}
    >
      Arca {build.version}
      {built && !Number.isNaN(built.valueOf())
        ? ` · ${built.toISOString().slice(0, 10)}`
        : null}
    </a>
  )
}

function Rail() {
  return (
    <aside className="sticky top-0 hidden h-dvh w-64 shrink-0 flex-col lg:flex">
      <RailContent />
    </aside>
  )
}

function MobileRail({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { pathname } = useLocation()

  useEffect(() => {
    onClose()
  }, [pathname, onClose])

  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  return (
    <div className="lg:hidden">
      <button
        type="button"
        aria-label="Close navigation"
        onClick={onClose}
        className="fixed inset-0 z-40 bg-foreground/25 backdrop-blur-[3px]"
      />
      <aside className="fixed inset-y-0 left-0 z-50 flex w-72 max-w-[85vw] flex-col overflow-hidden bg-background shadow-float">
        <BrandSplotch className="left-full top-0" />
        <button
          type="button"
          aria-label="Close navigation"
          onClick={onClose}
          className="absolute right-3 top-4 z-10 inline-flex size-8 items-center justify-center rounded-full text-faint transition-colors hover:bg-accent hover:text-foreground"
        >
          <X className="size-4" />
        </button>
        <div className="relative z-0 flex-1 overflow-hidden pr-10">
          <RailContent onNavigate={onClose} />
        </div>
      </aside>
    </div>
  )
}

function UserMenu() {
  const { user } = useSession()
  const refresh = useRefreshSession()
  const navigate = useNavigate()

  if (!user) {
    return (
      <Button size="sm" asChild>
        <Link to="/login">Sign in</Link>
      </Button>
    )
  }

  const item =
    'flex cursor-pointer items-center gap-2.5 rounded-xl px-3 py-2 text-[13px] text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground data-[highlighted]:bg-accent data-[highlighted]:text-foreground'

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <Button variant="ghost" size="sm" className="gap-2 pl-1.5 pr-3">
          <span className="flex size-6 items-center justify-center rounded-full bg-solid text-[11px] font-semibold text-solid-foreground">
            {user.name.trim()[0]?.toUpperCase() ?? '?'}
          </span>
          <span className="hidden max-w-32 truncate sm:inline">{user.name}</span>
          <ChevronDown className="size-3.5 opacity-60" />
        </Button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="end"
          sideOffset={8}
          className="z-50 min-w-60 rounded-2xl bg-card p-1.5 shadow-float dark:ring-1 dark:ring-border"
        >
          <div className="px-3 pb-2 pt-1.5">
            <p className="truncate text-sm font-medium text-foreground">{user.name}</p>
            <p className="truncate font-mono text-xs text-muted-foreground">{user.email}</p>
          </div>
          <DropdownMenu.Separator className="my-1.5 h-px bg-border" />
          <DropdownMenu.Item
            className={item}
            onSelect={() => {
              void post('/auth/logout')
                .then(refresh)
                .then(() => navigate('/'))
            }}
          >
            <LogOut className="size-4" /> Sign out
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}

function SearchBar() {
  const [params] = useSearchParams()
  const [query, setQuery] = useState(params.get('q') ?? '')
  const input = useRef<HTMLInputElement>(null)
  const navigate = useNavigate()

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const typing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement
      if (event.key === '/' && !typing) {
        event.preventDefault()
        input.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (query.trim().length >= 2) navigate(`/search?q=${encodeURIComponent(query.trim())}`)
  }

  return (
    <form onSubmit={submit} className="relative min-w-0 flex-1 md:max-w-sm">
      <Search className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-faint" />
      <input
        ref={input}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        placeholder="Find an artifact"
        aria-label="Search artifacts"
        className="h-9 w-full rounded-full border border-transparent bg-muted pl-10 pr-10 text-[13px] text-foreground transition-colors placeholder:text-faint hover:bg-accent focus-visible:border-primary focus-visible:bg-card focus-visible:outline-none"
      />
      <kbd className="pointer-events-none absolute right-3 top-1/2 hidden -translate-y-1/2 font-mono text-[11px] text-faint md:block">
        /
      </kbd>
    </form>
  )
}

export function AppShell() {
  const { instanceName } = useSession()
  const [navOpen, setNavOpen] = useState(false)
  const closeNav = useCallback(() => setNavOpen(false), [])

  return (
    <div className="relative flex min-h-dvh overflow-x-clip">
      <BrandSplotch className="left-64 top-0 hidden lg:block" />
      <Rail />
      <MobileRail open={navOpen} onClose={closeNav} />

      <div className="relative flex min-w-0 flex-1 flex-col lg:py-3 lg:pr-3">
        <div className="flex min-w-0 flex-1 flex-col bg-card lg:rounded-3xl lg:shadow-card lg:dark:ring-1 lg:dark:ring-border">
          <header className="sticky top-0 z-40 flex h-16 items-center gap-3 px-4 sm:px-6 lg:px-8">
            <button
              type="button"
              onClick={() => setNavOpen(true)}
              aria-label="Open navigation"
              className="-ml-1 inline-flex size-9 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground lg:hidden"
            >
              <Menu className="size-5" />
            </button>
            <Link to="/" className="flex items-center gap-2 lg:hidden">
              <Logo className="size-5" />
              <span className="hidden truncate font-semibold sm:inline">{instanceName}</span>
            </Link>
            <SearchBar />
            <div className="flex shrink-0 items-center gap-1">
              <ThemeToggle />
              <UserMenu />
            </div>
          </header>

          <main className="flex-1 px-4 pb-14 pt-4 sm:px-6 lg:px-8">
            <div className="mx-auto w-full max-w-5xl space-y-8">
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </div>
  )
}
