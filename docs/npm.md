---
title: Using it from npm
layout: default
nav_order: 4
---

# Using it from npm

Create a repository with a format of `npm`. The format is fixed once the repository exists, because
nothing migrates between formats.

## Pointing npm at it

For a whole project:

```
registry=https://repo.example.com/repository/npm-hosted/
//repo.example.com/repository/npm-hosted/:_authToken=cnx_...
```

For one scope, leaving everything else on the public registry:

```
@your-scope:registry=https://repo.example.com/repository/npm-hosted/
//repo.example.com/repository/npm-hosted/:_authToken=cnx_...
```

The `_authToken` key is the registry URL with the scheme stripped, and the trailing slash matters.
A public repository needs no token to read.

## Logging in

```sh
npm login --auth-type=legacy --registry=https://repo.example.com/repository/npm-hosted/
```

The legacy flag is required. npm's browser based login posts to an endpoint this server does not
implement, which is deliberate: leaving it unrouted makes npm fall back to the flow that works.

An API token from the Account page does the same job without logging in.

## What works

`npm publish`, `npm install`, `npm view`, `npm dist-tag`, `npm search`, `npm deprecate` and
`npm unpublish`, plus `npm token` for creating, listing and revoking tokens.

A published version is immutable regardless of the repository's `allowRedeploy` setting. npm clients
cache by integrity, so a replaced tarball is never picked up, and pretending otherwise would only
produce confusing failures.

The repository policy maps onto semver: `release` rejects prereleases, `snapshot` accepts only
prereleases, and `mixed` takes both.

## Storage layout

Packages are stored under their coordinates rather than npm's `-` convention, so `@types/node@20.1.0`
lives at `@types/node/20.1.0/`. That is what lets browsing, search, storage insights and per version
deletion be the same code paths Maven uses.

A scoped package maps to a namespace and a name, so `@types/node` is namespace `types`, name `node`.
Searching for `types` finds every `@types/*` package.
