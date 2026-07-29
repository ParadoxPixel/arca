import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Globe, Lock } from 'lucide-react'
import { PolicyBadge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { EmptyState, ErrorBlock, LoadingBlock, PageHeading } from '@/components/ui/feedback'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { CopyButton } from '@/components/copy-button'
import { api, type Repository } from '@/lib/api'
import { categoriesQuery, groupRepositories, needsHeadings } from '@/lib/categories'
import { useSession } from '@/lib/session'
import { formatRelative, plural } from '@/lib/utils'

function RepositoryTable({ repositories }: { repositories: Repository[] }) {
  return (
    <Card>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Repository</TableHead>
            <TableHead className="w-32">Policy</TableHead>
            <TableHead className="w-28">Access</TableHead>
            <TableHead className="w-28">Artifacts</TableHead>
            <TableHead className="w-36">Last publish</TableHead>
            <TableHead className="w-12" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {repositories.map((repository) => (
            <TableRow key={repository.id}>
              <TableCell className="py-3">
                <Link
                  to={`/repositories/${repository.name}`}
                  className="font-mono text-sm font-medium text-foreground hover:text-primary"
                >
                  {repository.name}
                </Link>
                {repository.description ? (
                  <p className="mt-0.5 max-w-md truncate text-xs text-muted-foreground">
                    {repository.description}
                  </p>
                ) : null}
              </TableCell>
              <TableCell>
                <PolicyBadge policy={repository.policy} />
              </TableCell>
              <TableCell>
                <span className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
                  {repository.visibility === 'public' ? (
                    <Globe className="size-3" />
                  ) : (
                    <Lock className="size-3" />
                  )}
                  {repository.visibility}
                </span>
              </TableCell>
              <TableCell className="font-mono text-sm tabular-nums text-muted-foreground">
                {repository.components ?? 0}
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {formatRelative(repository.updatedAt)}
              </TableCell>
              <TableCell>
                <CopyButton value={`${window.location.origin}/repository/${repository.name}`} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  )
}

export function RepositoriesRoute() {
  const { user } = useSession()
  const { data, error, isLoading } = useQuery({
    queryKey: ['repositories'],
    queryFn: () => api<{ repositories: Repository[] }>('/repositories'),
  })
  const categories = useQuery(categoriesQuery)

  if (isLoading || categories.isLoading) return <LoadingBlock label="Loading repositories" />
  if (error) return <ErrorBlock message={(error as Error).message} />

  const repositories = data?.repositories ?? []
  const groups = groupRepositories(repositories, categories.data?.categories ?? [])
  const headings = needsHeadings(groups)

  return (
    <>
      <PageHeading
        eyebrow="Index"
        title="Repositories"
        description={
          user
            ? 'Everything you can read, plus everything published publicly.'
            : 'Public repositories, readable without an account.'
        }
      />

      {repositories.length === 0 ? (
        <EmptyState
          title="Nothing published here yet"
          description={
            user?.role === 'admin'
              ? 'Create a repository under Manage, then publish to it with Maven or npm.'
              : 'Sign in to see repositories you have been granted access to.'
          }
        />
      ) : (
        <div className="space-y-8">
          {groups.map((group) => (
            <section key={group.id ?? 'uncategorized'} className="space-y-3">
              {headings ? (
                <div className="space-y-0.5">
                  <h2 className="text-sm font-semibold">{group.name}</h2>
                  {group.description ? (
                    <p className="text-xs text-muted-foreground">{group.description}</p>
                  ) : null}
                </div>
              ) : null}
              <RepositoryTable repositories={group.repositories} />
            </section>
          ))}
        </div>
      )}

      {repositories.length > 0 ? (
        <p className="text-xs text-muted-foreground">
          {plural(repositories.length, 'repository', 'repositories')} · copy a row to get its URL
        </p>
      ) : null}
    </>
  )
}
