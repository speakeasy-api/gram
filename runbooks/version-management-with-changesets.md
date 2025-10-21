# Version management with Changesets

[changesets]: https://github.com/changesets/changesets
[changesets-action]: https://github.com/changesets/action

## About

We use [Changesets][changesets] to manage versioning and changelogs for various packages in
this monorepo.

## When to skip a changeset

Pull request titles that start with `chore: ` can be merged without changesets.

These must contain changes with no tangible effect on the product. Otherwise, changeset is always required.

Example: `chore: fix typo in code comment`.

## Typical workflow

1. Commit some changes to a local branch.
2. Run `pnpm changeset`.
3. Select packages to update with `<SPACE>`. **Never select @gram/client**.
4. The changeset file will be auto-committed.
5. Open a pull request.

> [!IMPORTANT]
>
> Do not bump the version of the SDK, `@gram/client`, with the changesets CLI.
> The SDK version is managed by the `speakeasy` CLI and unfortunately there
> isn't a way to hide it from the package selection step of the changeset CLI.

_You can include multiple changesets in a single pull request if each affected
package contains distinct changes._

## Conventions

Use capital letters and proper grammar in your changelogs. Omit the qualifier prefix.

### ❌

```
---
dashboard: patch
---

fix: prevents double submit on upgrade button
```

### ✅

```
---
dashboard: patch
---

Prevent double submit on upgrade button <feel free to elaborate if it is warranted. you have room to breath here.>
```

## Release flow

Over time, several PRs will be merged and changesets will build up on `main`.
There will also be a "release" pull request that is automatically created by the
[changeset action][changesets-action]. Merging this PR to main constitutes
cutting a release which currently adds git tags and pushes them to remote
repository.
