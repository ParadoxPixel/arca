# npm demo package

A dependency-free package plus a script that publishes it to a local Arca instance and installs it
back, so an npm repository can be checked end to end with the real npm client.

## Run it

Start the server:

```
make dev
```

Sign in to the UI, create a repository with format `npm` (the script assumes it is called
`npm-hosted`), then:

```
EMAIL=you@example.com PASSWORD=your-password ./examples/npm-demo/verify.sh
```

The script logs in over the registry's own login endpoint, publishes a release and a prerelease,
reads them back with `npm view`, installs the release into a throwaway project, runs the installed
code, and checks that republishing an existing version is refused.

Override the defaults with environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `REGISTRY_HOST` | `http://localhost:5173` | Where the server is reachable. Use `http://localhost:8080` to skip the vite proxy. |
| `REPOSITORY` | `npm-hosted` | Name of the npm repository to publish to. |
| `EMAIL`, `PASSWORD` | none | An account with write access to that repository. |

The package needs a repository whose policy is `mixed` or `snapshot` for the prerelease step to
pass. A `release` repository rejects prereleases, which is the intended behaviour.

## Using the registry by hand

`npm login` works if you ask for the legacy flow, because the browser-based flow is not implemented:

```
npm login --auth-type=legacy --registry=http://localhost:5173/repository/npm-hosted/
```

An API token from the account page avoids logging in at all. The `_authToken` key is the registry
URL without its scheme, and the trailing slash matters:

```
registry=http://localhost:5173/repository/npm-hosted/
//localhost:5173/repository/npm-hosted/:_authToken=cnx_...
```

## What is not covered

Installing a package that depends on something from the public registry. A hosted repository only
serves what was published to it, so `npm install` fails to resolve external dependencies. For that,
create a second repository of type `proxy` with a `remoteUrl` of `https://registry.npmjs.org` and
point npm at it instead.
