import { CodeBlock } from '@/components/copy-button'
import type { RepositoryDetail } from '@/lib/api'
import { coordinateLabel, packageLabel } from '@/lib/coordinates'

function registryURL(repository: string): string {
  return `${window.location.origin}/repository/${repository}/`
}

export function NPMSetup({
  repository,
  namespace,
}: {
  repository: RepositoryDetail
  namespace: string
}) {
  const registry = registryURL(repository.name)
  const isPrivate = repository.visibility === 'private'

  // npm keys credentials on the registry URL with its scheme stripped.
  const credentialKey = registry.replace(/^https?:/, '')

  const scopeLine = namespace ? `@${namespace}:registry=${registry}` : `registry=${registry}`

  const npmrc = isPrivate
    ? `${scopeLine}
${credentialKey}:_authToken=YOUR_API_TOKEN`
    : scopeLine

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <p className="text-xs font-medium text-muted-foreground">Registry</p>
        <p className="max-w-2xl text-sm text-muted-foreground">
          {isPrivate
            ? 'This repository is private. Point npm at the registry and add an API token from your account page, or run npm login to have one issued for you.'
            : 'Point npm at the registry so the package resolves. Reading needs no credentials.'}
        </p>
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <CodeBlock language=".npmrc" code={npmrc} />
        {isPrivate ? (
          <CodeBlock
            language="Terminal"
            code={`npm login --auth-type=legacy --registry=${registry}`}
          />
        ) : null}
      </div>
    </div>
  )
}

export function NPMUsage({ repository }: { repository: RepositoryDetail }) {
  const registry = registryURL(repository.name)
  const isProxy = repository.type === 'proxy'
  const credentialKey = registry.replace(/^https?:/, '')

  // A public proxy is read-only and readable by anyone, so it is the one case
  // that never needs a token.
  const auth =
    isProxy && repository.visibility === 'public'
      ? ''
      : `
${credentialKey}:_authToken=YOUR_API_TOKEN`

  const npmrc = `registry=${registry}${auth}`
  const scoped = `@your-scope:registry=${registry}${auth}`

  return (
    <div className="space-y-5">
      <p className="max-w-2xl text-sm text-muted-foreground">
        {isProxy
          ? 'This repository is read-only. Point npm at it and every package it resolves is fetched from the remote once, then served from here.'
          : repository.visibility === 'public'
            ? 'Reading needs no credentials. Publishing needs an API token from your account page.'
            : 'Both reading and publishing need an API token from your account page, set as _authToken in your .npmrc.'}
      </p>
      <div className="grid gap-4 lg:grid-cols-2">
        <CodeBlock code={npmrc} language=".npmrc (all packages)" />
        <CodeBlock code={scoped} language=".npmrc (one scope only)" />
        {auth ? (
          <CodeBlock
            code={`npm login --auth-type=legacy --registry=${registry}`}
            language="issue a token instead"
          />
        ) : null}
        {isProxy ? null : (
          <CodeBlock code={`npm publish --registry=${registry}`} language="one-off publish" />
        )}
      </div>
    </div>
  )
}

export function NPMDependency({
  namespace,
  name,
  version,
}: {
  namespace: string
  name: string
  version: string
}) {
  const pkg = packageLabel('npm', namespace, name)
  const pinned = coordinateLabel('npm', namespace, name, version)

  return (
    <div className="space-y-4">
      <p className="text-xs font-medium text-muted-foreground">Install</p>
      <div className="grid gap-4 lg:grid-cols-2">
        <CodeBlock language="Terminal" code={`npm install ${pinned}`} />
        <div className="space-y-4">
          <CodeBlock language="pnpm" code={`pnpm add ${pinned}`} />
          <CodeBlock language="package.json" code={`"${pkg}": "^${version}"`} />
        </div>
      </div>
    </div>
  )
}
