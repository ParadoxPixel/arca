import { CodeBlock } from '@/components/copy-button'
import type { RepositoryDetail } from '@/lib/api'

export function MavenSetup({ repository }: { repository: RepositoryDetail }) {
  const base = `${window.location.origin}/repository/${repository.name}`
  const isPrivate = repository.visibility === 'private'

  const pom = `<repositories>
  <repository>
    <id>${repository.name}</id>
    <url>${base}</url>
    <releases><enabled>${repository.policy !== 'snapshot'}</enabled></releases>
    <snapshots><enabled>${repository.policy !== 'release'}</enabled></snapshots>
  </repository>
</repositories>`

  const settings = `<settings xmlns="http://maven.apache.org/SETTINGS/1.0.0">
  <servers>
    <server>
      <id>${repository.name}</id>
      <username>YOUR_EMAIL</username>
      <password>YOUR_API_TOKEN</password>
    </server>
  </servers>
</settings>`

  const gradle = `repositories {
    maven {
        url = uri("${base}")${
          isPrivate
            ? `
        credentials {
            username = providers.gradleProperty("arcaUser").get()
            password = providers.gradleProperty("arcaToken").get()
        }`
            : ''
        }
    }
}`

  const gradleProperties = `arcaUser=YOUR_EMAIL
arcaToken=YOUR_API_TOKEN`

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <p className="text-xs font-medium text-muted-foreground">Repository</p>
        <p className="max-w-2xl text-sm text-muted-foreground">
          {isPrivate
            ? 'This repository is private. Add the repository and an API token from your account page, sent as the password with your email as the username.'
            : 'Add the repository so the dependency resolves. Reading needs no credentials.'}
        </p>
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <CodeBlock language="pom.xml" code={pom} />
        <CodeBlock language="build.gradle.kts" code={gradle} />
        {isPrivate ? (
          <>
            <CodeBlock language="~/.m2/settings.xml" code={settings} />
            <CodeBlock language="~/.gradle/gradle.properties" code={gradleProperties} />
          </>
        ) : null}
      </div>
    </div>
  )
}

export function MavenUsage({ repository }: { repository: RepositoryDetail }) {
  const base = `${window.location.origin}/repository/${repository.name}`
  const isProxy = repository.type === 'proxy'
  const isSnapshot = repository.policy === 'snapshot'
  const element = isSnapshot ? 'snapshotRepository' : 'repository'

  const consume = `<repositories>
  <repository>
    <id>${repository.name}</id>
    <url>${base}</url>
    <releases><enabled>${!isSnapshot}</enabled></releases>
    <snapshots><enabled>${repository.policy !== 'release'}</enabled></snapshots>
  </repository>
</repositories>`

  const publish = `

<distributionManagement>
  <${element}>
    <id>${repository.name}</id>
    <url>${base}</url>
  </${element}>
</distributionManagement>`

  const pom = isProxy ? consume : consume + publish

  const settings = `<servers>
  <server>
    <id>${repository.name}</id>
    <username>YOUR_EMAIL</username>
    <password>YOUR_API_TOKEN</password>
  </server>
</servers>`

  const credentials =
    repository.visibility === 'private'
      ? `
        credentials {
            username = providers.gradleProperty("arcaUser").get()
            password = providers.gradleProperty("arcaToken").get()
        }`
      : ''

  const gradle = `repositories {
    maven {
        url = uri("${base}")${credentials}
    }
}`

  return (
    <div className="space-y-5">
      <p className="max-w-2xl text-sm text-muted-foreground">
        {isProxy
          ? 'This repository is read-only. Point a build at it and every dependency it resolves is fetched from the remote once, then served from here.'
          : repository.visibility === 'public'
            ? 'Reading needs no credentials. Publishing needs an API token from your account page.'
            : 'Both reading and publishing need an API token from your account page, sent as the Maven password with your email as the username.'}
      </p>
      <div className="grid gap-4 lg:grid-cols-2">
        <CodeBlock code={pom} language="pom.xml" />
        <CodeBlock code={settings} language="~/.m2/settings.xml" />
        <CodeBlock code={gradle} language="build.gradle.kts" />
        {isProxy ? null : (
          <CodeBlock
            code={`mvn deploy \\
  -DaltDeploymentRepository=${repository.name}::default::${base}`}
            language="one-off deploy"
          />
        )}
      </div>
    </div>
  )
}

export function MavenDependency({
  namespace,
  name,
  version,
}: {
  namespace: string
  name: string
  version: string
}) {
  return (
    <div className="space-y-4">
      <p className="text-xs font-medium text-muted-foreground">Dependency</p>
      <div className="grid gap-4 lg:grid-cols-2">
        <CodeBlock
          language="pom.xml"
          code={`<dependency>
  <groupId>${namespace}</groupId>
  <artifactId>${name}</artifactId>
  <version>${version}</version>
</dependency>`}
        />
        <div className="space-y-4">
          <CodeBlock
            language="build.gradle.kts"
            code={`implementation("${namespace}:${name}:${version}")`}
          />
          <CodeBlock
            language="build.sbt"
            code={`libraryDependencies += "${namespace}" %% "${name}" % "${version}"`}
          />
        </div>
      </div>
    </div>
  )
}
