#!/usr/bin/env bash
set -euo pipefail

REPO=${REPO:?repository is required}
DRY_RUN=${DRY_RUN:-false}

number=$(gh pr list \
  --repo "$REPO" \
  --state open \
  --base main \
  --head changeset-release/main \
  --json number,title,author,isDraft \
  --jq '
    [.[]
      | select(.title == "chore: version packages" and .isDraft == false)
      | select(.author.login == "app/gram-bot" or .author.login == "gram-bot[bot]")]
    | if length == 0 then ""
      elif length == 1 then .[0].number | tostring
      else "many" end
  ')
if [[ -z "$number" ]]; then
  echo "No open version pull request."
  exit 0
fi
if [[ "$number" == "many" ]]; then
  echo "More than one version pull request." >&2
  exit 1
fi

# shellcheck disable=SC2016 # GraphQL variables are not shell variables
pr=$(gh api graphql \
  -f query='query($owner:String!, $name:String!, $number:Int!) { repository(owner:$owner, name:$name) { pullRequest(number:$number) { id isInMergeQueue reviewDecision } } }' \
  -f owner="${REPO%%/*}" \
  -f name="${REPO#*/}" \
  -F number="$number" \
  --jq '.data.repository.pullRequest')
if [[ $(echo "$pr" | jq -r .isInMergeQueue) == "true" ]]; then
  echo "Pull request #${number} is already in the merge queue."
  exit 0
fi
review=$(echo "$pr" | jq -r .reviewDecision)
if [[ "$review" == "CHANGES_REQUESTED" ]]; then
  echo "Pull request #${number} has changes requested." >&2
  exit 1
fi
pr_id=$(echo "$pr" | jq -r .id)

# gh pr checks --required reports success when a ruleset check has not been
# posted yet, so this looks up the three checks by name. Returns 0 when all
# three are green, 1 when one failed, 2 while any are still running.
required_checks() {
  local json name bucket missing=0
  json=$(gh pr checks "$number" --repo "$REPO" --json name,bucket,startedAt)
  for name in "CI Gate" "Lint PR Title and Changesets" "Tag branch changes"; do
    # shellcheck disable=SC2016 # $n is a jq variable
    bucket=$(echo "$json" | jq -r --arg n "$name" 'map(select(.name == $n)) | sort_by(.startedAt) | last | .bucket // "missing"')
    case "$bucket" in
      pass) echo "Required check green: ${name}" ;;
      fail|cancel)
        echo "Required check ${bucket}: ${name}" >&2
        return 1
        ;;
      *)
        echo "Required check ${bucket}: ${name}"
        missing=1
        ;;
    esac
  done
  if [[ "$missing" -eq 0 ]]; then
    return 0
  fi
  return 2
}

deadline=$((SECONDS + 1800))
while true; do
  rc=0
  required_checks || rc=$?
  if [[ "$rc" -eq 0 || "$DRY_RUN" == "true" ]]; then
    break
  fi
  if [[ "$rc" -eq 1 ]]; then
    exit 1
  fi
  if (( SECONDS >= deadline )); then
    echo "Required checks were still running after 30 minutes." >&2
    exit 1
  fi
  sleep 60
done
if [[ "$DRY_RUN" == "true" ]]; then
  echo "Dry run: stopping."
  exit 0
fi
current=$(gh pr view "$number" --repo "$REPO" --json headRefOid,reviewDecision)
sha=$(echo "$current" | jq -r .headRefOid)
review=$(echo "$current" | jq -r .reviewDecision)
if [[ "$review" == "CHANGES_REQUESTED" ]]; then
  echo "Pull request #${number} has changes requested." >&2
  exit 1
fi

if [[ "$review" != "APPROVED" ]]; then
  gh api "repos/${REPO}/pulls/${number}/reviews" \
    -f event=APPROVE \
    -f body="Scheduled release approval. Required checks are green on ${sha}." >/dev/null
  echo "Approved pull request #${number}."
fi

# shellcheck disable=SC2016 # GraphQL variables are not shell variables
entry=$(gh api graphql \
  -f query='mutation($id:ID!, $oid:GitObjectID) { enqueuePullRequest(input:{pullRequestId:$id, expectedHeadOid:$oid}) { mergeQueueEntry { position state } } }' \
  -f id="$pr_id" \
  -f oid="$sha" \
  --jq '.data.enqueuePullRequest.mergeQueueEntry')
echo "Queued pull request #${number} at ${sha}: ${entry}"
