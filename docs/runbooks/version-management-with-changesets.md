# Version management with Changesets

[changesets]: https://github.com/changesets/changesets
[changesets-action]: https://github.com/changesets/action

We use [Changesets][changesets] to manage versioning and changelogs for various packages in
this monorepo.

> [!NOTE]
>
> If you are making a change that does not affect the product in a tangible way
> then creating a changeset is not necessary and this guide can be skipped. The
> PR title should have a `chore:` qualifier to pass the changeset linter that
> would otherwise fail if a changeset is not present.

The typical development workflow is:

1. Make some changes to the codebase
2. Commit them to a branch
3. Run `pnpm changeset` and you will be prompted to create a changeset file
4. If it isn't auto-committed then commit the changeset file to the same branch
5. Open a pull request

> [!IMPORTANT]
>
> Do not bump the version of the SDK, `@gram/client`, with the changesets CLI.
> The SDK version is managed by the `speakeasy` CLI and unfortunately there
> isn't a way to hide it from the package selection step of the changeset CLI.

_You can include multiple changesets in a single pull request if each affected
package contains distinct changes._

## Release flow

Over time, several PRs will be merged and changesets will build up on `main`.
There will also be a "release" pull request that is automatically created by the
[changeset action][changesets-action]. Merging this PR to main constitutes
cutting a release which currently adds git tags and pushes them to remote
repository.
