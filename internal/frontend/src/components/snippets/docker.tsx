import { CodeBlock } from '@/components/copy-button'
import type { RepositoryDetail } from '@/lib/api'

// A Docker client builds its own URLs from the image reference, so the registry is
// the bare host: no scheme and no path. The repository name is the first segment of
// the image instead, which is how one host serves several repositories on one port.
function registryHost(): string {
  return window.location.host
}

function imageReference(repository: string, image: string): string {
  return `${registryHost()}/${repository}/${image}`
}

function LoginBlock() {
  return (
    <CodeBlock
      language="Terminal"
      code={`docker login ${registryHost()} --username YOUR_EMAIL
# use an API token from your account page as the password`}
    />
  )
}

export function DockerUsage({ repository }: { repository: RepositoryDetail }) {
  const host = registryHost()
  const isProxy = repository.type === 'proxy'
  const isPublic = repository.visibility === 'public'
  const example = imageReference(repository.name, 'your-image')

  return (
    <div className="space-y-5">
      <div className="space-y-1">
        <p className="text-xs font-medium text-muted-foreground">Registry</p>
        <p className="max-w-2xl text-sm text-muted-foreground">
          {isProxy
            ? `This repository is read-only. Pull through it and every image is fetched from the remote once, then served from here. The repository name is the first segment of the image, so images resolve under ${host}/${repository.name}/.`
            : `Images live under ${host}/${repository.name}/. The repository name is part of the image reference, which is how one host serves several repositories without a port or a hostname of its own.`}
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {isPublic && isProxy ? null : <LoginBlock />}
        <CodeBlock language="Pull" code={`docker pull ${example}:1.0.0`} />
        {isProxy ? null : (
          <>
            <CodeBlock
              language="Push"
              code={`docker tag your-image:1.0.0 ${example}:1.0.0
docker push ${example}:1.0.0`}
            />
            <CodeBlock language="Dockerfile" code={`FROM ${example}:1.0.0`} />
          </>
        )}
      </div>

      {isPublic ? (
        <p className="max-w-2xl text-sm text-muted-foreground">
          Pulling needs no credentials.
          {isProxy ? '' : ' Pushing needs an API token from your account page.'}
        </p>
      ) : (
        <p className="max-w-2xl text-sm text-muted-foreground">
          This repository is private, so both pulling and pushing need a docker login
          with an API token from your account page.
        </p>
      )}
    </div>
  )
}

export function DockerDependency({
  repository,
  image,
  tag,
  digest,
}: {
  repository: string
  image: string
  tag: string
  digest?: string
}) {
  const reference = imageReference(repository, image)

  return (
    <div className="space-y-4">
      <p className="text-xs font-medium text-muted-foreground">Pull</p>
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-4">
          <CodeBlock language="By tag" code={`docker pull ${reference}:${tag}`} />
          <CodeBlock language="Dockerfile" code={`FROM ${reference}:${tag}`} />
        </div>
        <div className="space-y-4">
          {digest ? (
            <CodeBlock language="By digest (immutable)" code={`docker pull ${reference}@${digest}`} />
          ) : null}
          <CodeBlock
            language="compose.yaml"
            code={`services:
  app:
    image: ${reference}:${tag}`}
          />
        </div>
      </div>
      {digest ? (
        <p className="max-w-2xl text-xs text-muted-foreground">
          A tag can be moved to another image. Pulling by digest is the only reference
          that always resolves to these exact bytes.
        </p>
      ) : null}
    </div>
  )
}
