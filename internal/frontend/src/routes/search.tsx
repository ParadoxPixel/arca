import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { FormatBadge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { EmptyState, ErrorBlock, LoadingBlock, PageHeading } from '@/components/ui/feedback'
import { Coordinate } from '@/components/copy-button'
import { api, type ArtifactSummary } from '@/lib/api'
import { formatRelative, plural } from '@/lib/utils'

export function SearchRoute() {
  const [params, setParams] = useSearchParams()
  const initial = params.get('q') ?? ''
  const [query, setQuery] = useState(initial)
  const [debounced, setDebounced] = useState(initial)

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebounced(query)
      setParams(query ? { q: query } : {}, { replace: true })
    }, 250)
    return () => window.clearTimeout(timer)
  }, [query, setParams])

  const { data, error, isFetching } = useQuery({
    queryKey: ['search', debounced],
    queryFn: () => api<{ results: ArtifactSummary[] }>(`/search?q=${encodeURIComponent(debounced)}`),
    enabled: debounced.trim().length >= 2,
  })

  const results = data?.results ?? []
  const ready = debounced.trim().length >= 2

  return (
    <>
      <PageHeading
        eyebrow="Search"
        title="Find an artifact"
        description="Matches names and namespaces across every repository you can read, in any format."
      />

      <div className="relative">
        <Search className="pointer-events-none absolute left-4 top-1/2 size-4 -translate-y-1/2 text-faint" />
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="com.example"
          aria-label="Search artifacts"
          autoFocus
          className="h-14 w-full rounded-2xl border border-border bg-card pl-12 pr-4 font-mono text-base text-foreground transition-colors placeholder:text-faint hover:border-faint/60 focus-visible:border-primary focus-visible:outline-none"
        />
      </div>

      {error ? <ErrorBlock message={(error as Error).message} /> : null}

      {!ready ? (
        <EmptyState
          title="Type at least two characters"
          description="Results are ranked by most recently published."
        />
      ) : isFetching && results.length === 0 ? (
        <LoadingBlock label="Searching" />
      ) : results.length === 0 ? (
        <EmptyState title="No matches" description={`Nothing published matches "${debounced}".`} />
      ) : (
        <>
          <p className="text-[13px] font-medium text-muted-foreground">{plural(results.length, 'match', 'matches')}</p>
          <Card>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Artifact</TableHead>
                  <TableHead className="w-24">Format</TableHead>
                  <TableHead className="w-44">Repository</TableHead>
                  <TableHead className="w-44">Latest</TableHead>
                  <TableHead className="w-36">Updated</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {results.map((result) => (
                  <TableRow key={`${result.repository}:${result.namespace}:${result.name}`}>
                    <TableCell>
                      <Link
                        to={`/artifacts/${result.repository}?namespace=${encodeURIComponent(result.namespace)}&name=${encodeURIComponent(result.name)}`}
                        className="block"
                      >
                        <Coordinate
                          format={result.format}
                          namespace={result.namespace}
                          name={result.name}
                          className="text-[13px]"
                        />
                      </Link>
                    </TableCell>
                    <TableCell>
                      <FormatBadge format={result.format} />
                    </TableCell>
                    <TableCell>
                      <Link
                        to={`/repositories/${result.repository}`}
                        className="font-mono text-xs text-muted-foreground hover:text-foreground"
                      >
                        {result.repository}
                      </Link>
                    </TableCell>
                    <TableCell className="font-mono text-[13px] text-primary">
                      {result.latest}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatRelative(result.updatedAt)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
        </>
      )}
    </>
  )
}
