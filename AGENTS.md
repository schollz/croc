# Repository instructions

## Version control with GitButler

Use the GitButler CLI (`but`) for all version-control operations. Do not use
Git write commands such as `git add`, `git commit`, `git push`, `git checkout`,
`git merge`, `git rebase`, `git stash`, or `git cherry-pick`.

- Use `but diff` to inspect uncommitted changes.
- Use `but status` for branch, stack, commit, and conflict overviews; add `-fv`
  only when file or hunk IDs are needed.
- Use `but commit -b <branch> -m "<message>" <change-ids>` to create commits.
- Use `but push <top-branch>` to push a branch or stack.
- Use `but pr new <branch>` to create pull requests, especially for stacked
  branches.

Read-only Git commands such as `git log`, `git blame`, and `git show --stat` are
allowed when useful. GitHub Actions and release operations continue to use the
GitHub CLI (`gh`) as documented below.

## Releasing croc

Releases are prepared by the **Prepare Release** GitHub Actions workflow. Do not
create or move a release tag manually: source-based package managers such as
Homebrew build the tagged source and require it to contain the release version.

1. Open **Actions → Prepare Release → Run workflow** on the default branch.
2. Enter a stable semantic-version tag such as `v11.2.5`.
3. Wait for every job to succeed. The workflow stamps `src/version/version.go`,
   commits it, creates the annotated tag, builds and signs all artifacts, and
   creates a draft GitHub release containing those artifacts and their checksums.
4. Review the generated notes and asset list, then publish the draft release.

Publishing the draft triggers the Docker workflow. A failed workflow may be
rerun with the same tag while the release is absent or still a draft; the
workflow rejects a mismatched tag or a release that has already been published.

Before publishing, verify that the workflow's native binary checks report both
`croc version X.Y.Z` and `croc-web version X.Y.Z` for the requested tag.

### Command-line release with `gh`

Run these commands from this repository. The authenticated account must have
permission to run Actions and publish releases:

```bash
gh auth status
gh repo view --json nameWithOwner,defaultBranchRef
```

Choose the new stable tag, dispatch the release workflow against the default
branch, and list the resulting run:

```bash
RELEASE_TAG=v11.2.5
gh workflow run release.yml --ref main -f tag="${RELEASE_TAG}"
gh run list --workflow release.yml --event workflow_dispatch --limit 5 \
  --json databaseId,displayTitle,status,url
```

The run's display title is `Prepare vMAJOR.MINOR.PATCH`. Copy its `databaseId`
from the list and wait for it to finish:

```bash
RELEASE_RUN_ID=123456789
gh run watch "${RELEASE_RUN_ID}" --exit-status
```

If it fails, inspect it with `gh run view "${RELEASE_RUN_ID}" --log-failed`.
After correcting a transient problem, either run
`gh run rerun "${RELEASE_RUN_ID}" --failed` or dispatch the same tag again. A
rerun is permitted only while the release is absent or still a draft.

After a successful run, verify that the release is a draft and contains 22
assets: 19 `croc` platform archives, the `croc-web` archive, the source archive,
and the checksum file.

```bash
gh release view "${RELEASE_TAG}" --json isDraft,url,assets \
  --jq '{isDraft: .isDraft, url: .url, assets: [.assets[].name]}'
test "$(gh release view "${RELEASE_TAG}" --json isDraft --jq .isDraft)" = true
test "$(gh release view "${RELEASE_TAG}" --json assets --jq '.assets | length')" -eq 22
```

Review the generated notes and asset names before publishing. Publishing is the
point that triggers the Docker workflow:

```bash
gh release edit "${RELEASE_TAG}" --draft=false --latest
gh run list --workflow deploy.yml --limit 3 \
  --json databaseId,displayTitle,status,conclusion,url
```

Use `gh run watch RUN_ID --exit-status` to follow the downstream run. Never use
`gh release create` or manually create or move the release tag with Git or
GitButler. The Prepare Release workflow owns the version commit, tag, artifacts,
and draft.
