#!/usr/bin/env bash
# Preview environments (the gram-app-previews ApplicationSet in gram-infra)
# deploy the gram, gram-dashboard and gram-pystreams images tagged
# pr-<number>-<8-char head sha> for every PR head commit. Components with
# relevant changes get that tag from their docker build jobs. This script
# covers the unchanged components: it finds the newest main commit whose
# watched paths match the PR merge commit and aliases that commit's sha-tagged
# image with the preview tag, so preview environments start without waiting
# for a rebuild.
#
# Required environment variables:
#   REGISTRY, DOCKER_REPOSITORY_OWNER  - image location, e.g. gcr.io/x + owner
#   PR_NUMBER, HEAD_SHA                - pull request coordinates
#   SERVER_CHANGED, CLIENT_CHANGED, PYSTREAMS_CHANGED
#     - "true" when the changes job decided that component builds
#
# Must run from the repository root of a full clone (needs main history and
# the PR merge commit checked out as HEAD), with docker authenticated to the
# registry.
set -euo pipefail

: "${REGISTRY:?}" "${DOCKER_REPOSITORY_OWNER:?}" "${PR_NUMBER:?}" "${HEAD_SHA:?}"
: "${SERVER_CHANGED:=}" "${CLIENT_CHANGED:=}" "${PYSTREAMS_CHANGED:=}"

IMAGE_BASE="${REGISTRY}/${DOCKER_REPOSITORY_OWNER}"
PREVIEW_TAG="pr-${PR_NUMBER}-$(printf '%s' "$HEAD_SHA" | cut -c1-8)"
MAIN_REF="${MAIN_REF:-origin/main}"
MAX_CANDIDATES="${MAX_CANDIDATES:-50}"

image_exists() {
  docker buildx imagetools inspect "$1" >/dev/null 2>&1
}

filter_paths() {
  yq -r ".${1}[]" .github/filters.yaml
}

retag_component() {
  local image="$1"
  shift

  local pathspecs=()
  local p
  for p in "$@"; do
    pathspecs+=(":(glob)${p}")
  done

  local ref="${IMAGE_BASE}/${image}"
  if image_exists "${ref}:${PREVIEW_TAG}"; then
    echo "${ref}:${PREVIEW_TAG} already exists, nothing to do"
    return 0
  fi

  # HEAD is the PR merge commit, so a main commit that only differs from it in
  # unwatched paths would produce a byte-identical component build.
  local candidate
  for candidate in $(git rev-list --first-parent -n "$MAX_CANDIDATES" "$MAIN_REF"); do
    if ! git diff --quiet "$candidate" HEAD -- "${pathspecs[@]}"; then
      continue
    fi
    if ! image_exists "${ref}:sha-${candidate}"; then
      echo "${image}: no image for matching commit ${candidate}, trying older commits"
      continue
    fi
    echo "${image}: tagging ${ref}:sha-${candidate} as ${PREVIEW_TAG}"
    docker buildx imagetools create --tag "${ref}:${PREVIEW_TAG}" "${ref}:sha-${candidate}"
    return 0
  done

  echo "::error::${image}: no matching image found on the last ${MAX_CANDIDATES} main commits. Preview environments will not start for ${HEAD_SHA}. Re-run this job once main CI has pushed images, or push with the ci:full label to force a build."
  return 1
}

failed=0

if [[ "$SERVER_CHANGED" != "true" ]]; then
  mapfile -t server_paths < <(filter_paths server)
  retag_component gram "${server_paths[@]}" || failed=1
fi

# docker-build-client builds the dashboard image when client OR server changes.
if [[ "$SERVER_CHANGED" != "true" && "$CLIENT_CHANGED" != "true" ]]; then
  mapfile -t dashboard_paths < <(
    filter_paths server
    filter_paths client
  )
  retag_component gram-dashboard "${dashboard_paths[@]}" || failed=1
fi

if [[ "$PYSTREAMS_CHANGED" != "true" ]]; then
  mapfile -t pystreams_paths < <(filter_paths pystreams)
  retag_component gram-pystreams "${pystreams_paths[@]}" || failed=1
fi

exit "$failed"
