---
title: Migrating from Nexus
layout: default
nav_order: 6
---

![Arca]({{ site.baseurl }}/assets/img/nexusimport.png)


# Migrating from Nexus

Arca reads a Sonatype Nexus 3 instance and reproduces its repositories here. It only ever sends `GET`
requests to Nexus: there is no code path that can create, change or delete anything there, so it is
safe to point at production.

The migration lives on the **Maintenance** page under Manage, and needs an administrator account.

## What you need

A Nexus account with read access to repository definitions and content. It needs no write permission
of any kind, so the safest thing to hand it is a read-only role.

You do not need an API token. The migration writes straight into this instance rather than calling
back into its own API.

## Look before you leap

Fill in the Nexus URL, the account to read it with, and press **Preview**. Nothing is written on
either side. Arca lists every repository it found and what it intends to do with each one:

| Repository | Becomes | Notes |
| --- | --- | --- |
| `maven-central` | maven2 proxy | Recreated pointing at the same upstream, cache not copied |
| `maven-releases` | maven2 hosted | Assets are copied across |
| `p2-releases` | p2 hosted | Assets are copied across |
| `maven-public` | skipped | groups are not migrated; recreate it as a p2 group, or point clients at the members directly (maven-central, maven-releases) |
| `docker-hosted` | skipped | arca has no docker format |

The password is used for that run and never stored.

## Running it

**Start migration** becomes available once you have previewed. The run continues in the background,
so you can leave the page: the progress table shows each repository moving from waiting to done, with
a running count of files copied, files already present, and failures. **Cancel** stops it.

Only one migration runs at a time.

## How each repository is treated

| Nexus | Becomes |
| --- | --- |
| `maven2` hosted | a hosted `maven2` repository, every asset copied |
| `maven2` proxy | a proxy pointing at the same upstream, cache **not** copied |
| `npm` hosted or proxy | the same, as `npm` |
| `raw` | a `p2` repository, every asset copied |
| group | skipped |
| any other format | skipped |

A proxy holds nothing that cannot be fetched again, so copying its cache would be a slow way to move
files you already have upstream. The recreated proxy refills itself on first use.

Nexus raw repositories become `p2` because that is what they usually hold: Nexus 3 dropped the p2
format, so a p2 site published to Nexus lives in a raw repository. A p2 repository here stores
whatever paths you publish to it, so it serves those sites unchanged. Change **Raw repositories
become** if one of yours holds something else.

A Maven repository's version policy carries across, so a `SNAPSHOT` repository in Nexus arrives as a
snapshot repository here.

Names are lowercased, because arca repository names are. A Nexus name that is still not valid after
that is skipped and left for you to create by hand.

## Groups

Group repositories are not migrated. Which members a group should carry is a decision worth making
deliberately rather than inheriting, and arca only has groups for p2. The preview names the members
of every group it skipped, so you can either point clients at those directly or, for p2, rebuild it
as a [group repository]({{ site.baseurl }}/p2#one-url-over-several-sites) afterwards.

## Interruptions

Start it again. Repositories that already exist are reported as such, and assets already present are
skipped after one lookup, so a rerun transfers only what is missing. That makes it safe to cancel a
long migration, or to run it once to move the bulk and again later to catch up.

It also means the migration will not reconfigure a repository you have already set up by hand. It
fills that repository with the Nexus content but leaves its settings alone. If you want the Nexus
settings instead, delete yours first and run it again.

## If it reports no repositories

Nexus answers the repository list with an empty array rather than an error when the account cannot
read repository definitions, so an unauthenticated run looks like an empty instance. Arca says so
explicitly. Check the username and its privileges.

## Once it is done

Point your builds at the new URLs. A repository keeps its name, so
`https://nexus.example.com/repository/p2-releases/...` becomes
`https://repo.example.com/repository/p2-releases/...` with the path after it unchanged.
