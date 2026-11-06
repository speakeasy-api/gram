#!/usr/bin/env bash
# Approve the Changesets version pull request and add it to the merge queue.
# The token has to be a different GitHub App than gram-bot, which opened the
# pull request. GitHub rejects a self-approval.
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
  -f query='query($owner:String!, $name:String!, $number:Int!) { repository(owner:$owner, name:$name) { pullRequest(number:$number) { id isInMergeQueue reviewDecision headRefOid } } }' \
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
sha=$(echo "$pr" | jq -r .headRefOid)
pr_id=$(echo "$pr" | jq -r .id)

# gh pr checks --required reports success when a ruleset check has not been
# posted yet, so this looks up the three checks by name.
check_status() {
  local json="$1" name="$2"
  echo "$json" | jq -r --arg n "$name" 'map(select(.name == $n)) | sort_by(.startedAt) | last | .bucket // "missing"'
}

# Returns 0 when all three are green, 1 when one failed, 2 while any are still running.
required_checks() {
  local json name bucket missing=0
  json=$(gh pr checks "$number" --repo "$REPO" --json name,bucket,startedAt)
  for name in "CI Gate" "Lint PR Title and Changesets" "Tag branch changes"; do
    bucket=$(check_status "$json" "$name")
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

rc=0
required_checks || rc=$?
if [[ "$DRY_RUN" == "true" ]]; then
  echo "Dry run: pull request #${number} at ${sha} (review=${review})."
  if [[ "$rc" -eq 0 ]]; then
    echo "Dry run: would approve, if needed, and add it to the merge queue."
  elif [[ "$rc" -eq 1 ]]; then
    echo "Dry run: a required check failed. Would not approve."
  else
    echo "Dry run: required checks are still running. The real run waits, then approves and queues."
  fi
  exit 0
fi
if [[ "$rc" -eq 1 ]]; then
  exit 1
fi
deadline=$((SECONDS + 1800))
while [[ "$rc" -ne 0 ]]; do
  if (( SECONDS >= deadline )); then
    echo "Required checks were still running after 30 minutes on ${sha}." >&2
    exit 1
  fi
  sleep 60
  current=$(gh pr view "$number" --repo "$REPO" --json headRefOid,reviewDecision)
  sha=$(echo "$current" | jq -r .headRefOid)
  review=$(echo "$current" | jq -r .reviewDecision)
  if [[ "$review" == "CHANGES_REQUESTED" ]]; then
    echo "Pull request #${number} has changes requested." >&2
    exit 1
  fi
  rc=0
  required_checks || rc=$?
  if [[ "$rc" -eq 1 ]]; then
    exit 1
  fi
done

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
