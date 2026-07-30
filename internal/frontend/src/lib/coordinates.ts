import type { RepositoryFormat } from '@/lib/api'

export function packageLabel(
  format: RepositoryFormat,
  namespace: string,
  name: string,
): string {
  if (format === 'npm') return namespace ? `@${namespace}/${name}` : name
  if (format === 'docker') return namespace ? `${namespace}/${name}` : name
  return `${namespace}:${name}`
}

export function coordinateLabel(
  format: RepositoryFormat,
  namespace: string,
  name: string,
  version?: string | null,
): string {
  const label = packageLabel(format, namespace, name)
  if (!version) return label
  if (format === 'npm') return `${label}@${version}`
  // A docker tag is joined with a colon, the same as Maven, but the separator
  // means something different: it names a moving pointer rather than a version.
  return `${label}:${version}`
}

export function formatLabel(format: RepositoryFormat): string {
  if (format === 'npm') return 'npm'
  if (format === 'p2') return 'p2'
  if (format === 'docker') return 'Docker'
  return 'Maven'
}
