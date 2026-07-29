import { api, type Category, type Repository } from '@/lib/api'

export const UNCATEGORIZED = 'Uncategorized'

export interface RepositoryGroup {
  id: string | null
  name: string
  description: string
  repositories: Repository[]
}

export const categoriesQuery = {
  queryKey: ['categories'],
  queryFn: () => api<{ categories: Category[] }>('/categories'),
}

export function groupRepositories(
  repositories: Repository[],
  categories: Category[],
): RepositoryGroup[] {
  const groups: RepositoryGroup[] = categories.map((category) => ({
    id: category.id,
    name: category.name,
    description: category.description,
    repositories: [],
  }))

  const byId = new Map(groups.map((group) => [group.id, group]))
  const loose: RepositoryGroup = {
    id: null,
    name: UNCATEGORIZED,
    description: '',
    repositories: [],
  }

  for (const repository of repositories) {
    const group = repository.categoryId ? byId.get(repository.categoryId) : undefined
    ;(group ?? loose).repositories.push(repository)
  }

  return [...groups, loose].filter((group) => group.repositories.length > 0)
}

// An instance with no categories should look exactly as it did before they
// existed, so a lone Uncategorized group is not worth a heading.
export function needsHeadings(groups: RepositoryGroup[]): boolean {
  return groups.length > 1 || groups.some((group) => group.id !== null)
}
