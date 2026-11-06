#!/usr/bin/env bash
# Approve the Changesets version pull request and add it to the merge queue.
# The approver token must belong to a different GitHub App than the one that
# opened the pull request. GitHub rejects a self-approval, and gram-bot opens
# this pull request.
set -euo pipefail

REPO=${REPO:?repository is required}
DRY_RUN=${DRY_RUN:-false}
HEAD_BRANCH=changeset-release/main
TITLE='chore: version packages'
BODY_PREFIX='This PR was opened by the [Changesets release](https://github.com/changesets/action) GitHub action.'
# Names from the "Merge to main" ruleset. CI Gate is the aggregate; the other
# two are required on their own and can pass while the aggregate is still running.
REQUIRED_CHECKS=(
  "CI Gate"
  "Lint PR Title and Changesets"
  "Tag branch changes"
)
WAIT_SECONDS=1800
POLL_SECONDS=60

if [[ "$DRY_RUN" != "true" && "$DRY_RUN" != "false" ]]; then
  echo "DRY_RUN must be true or false, got: ${DRY_RUN}" >&2
  exit 1
fi

prs=$(gh pr list \
  --repo "$REPO" \
  --state open \
  --base main \
  --head "$HEAD_BRANCH" \
  --json number,title,author,isDraft)
mapfile -t numbers < <(echo "$prs" | jq -r --arg title "$TITLE" '
  .[]
  | select(.title == $title and .isDraft == false)
  | select(.author.login == "app/gram-bot" or .author.login == "gram-bot[bot]")
  | .number
')

if [[ ${#numbers[@]} -eq 0 ]]; then
  echo "No open ${TITLE} pull request from gram-bot on ${HEAD_BRANCH}."
  exit 0
fi
if [[ ${#numbers[@]} -ne 1 ]]; then
  echo "Expected one version pull request, found ${#numbers[@]}: ${numbers[*]}" >&2
  exit 1
fi

number=${numbers[0]}
echo "Candidate pull request #${number}"

deadline=$((SECONDS + WAIT_SECONDS))
while true; do
  meta=$(gh pr view "$number" --repo "$REPO" --json \
    title,isDraft,state,headRefName,baseRefName,headRefOid,reviewDecision,mergeStateStatus,body)
  state=$(echo "$meta" | jq -r .state)
  if [[ "$state" != "OPEN" ]]; then
    echo "Pull request #${number} is ${state}; nothing to queue."
    exit 0
  fi

  title=$(echo "$meta" | jq -r .title)
  draft=$(echo "$meta" | jq -r .isDraft)
  head=$(echo "$meta" | jq -r .headRefName)
  base=$(echo "$meta" | jq -r .baseRefName)
  sha=$(echo "$meta" | jq -r .headRefOid)
  review=$(echo "$meta" | jq -r .reviewDecision)
  merge_state=$(echo "$meta" | jq -r .mergeStateStatus)
  body_ok=$(echo "$meta" | jq -r --arg prefix "$BODY_PREFIX" '.body | startswith($prefix)')

  if [[ "$title" != "$TITLE" || "$draft" != "false" || "$head" != "$HEAD_BRANCH" || "$base" != "main" || "$body_ok" != "true" ]]; then
    echo "Pull request #${number} no longer matches the Changesets version pull request." >&2
    echo "title=${title} draft=${draft} head=${head} base=${base} body_ok=${body_ok}" >&2
    exit 1
  fi

  # shellcheck disable=SC2016 # GraphQL variables are not shell variables
  queue=$(gh api graphql \
    -f query='query($owner:String!, $name:String!, $number:Int!) { repository(owner:$owner, name:$name) { pullRequest(number:$number) { isInMergeQueue reviewThreads(first:100) { pageInfo { hasNextPage } nodes { isResolved } } } } }' \
    -f owner="${REPO%%/*}" \
    -f name="${REPO#*/}" \
    -F number="$number" \
    --jq '.data.repository.pullRequest')
  in_queue=$(echo "$queue" | jq -r .isInMergeQueue)
  if [[ "$in_queue" == "true" ]]; then
    echo "Pull request #${number} is already in the merge queue."
    exit 0
  fi
  if [[ $(echo "$queue" | jq -r .reviewThreads.pageInfo.hasNextPage) == "true" ]]; then
    echo "Pull request #${number} has more review threads than this check reads." >&2
    exit 1
  fi
  unresolved=$(echo "$queue" | jq '[.reviewThreads.nodes[] | select(.isResolved == false)] | length')
  if [[ "$unresolved" != "0" ]]; then
    echo "Pull request #${number} has ${unresolved} unresolved review thread(s)." >&2
    exit 1
  fi
  if [[ "$merge_state" == "DIRTY" ]]; then
    echo "Pull request #${number} has merge conflicts." >&2
    exit 1
  fi
  if [[ "$review" == "CHANGES_REQUESTED" ]]; then
    echo "Pull request #${number} has changes requested. Leaving it unqueued." >&2
    exit 1
  fi

  checks=$(gh api --paginate \
    "repos/${REPO}/commits/${sha}/check-runs?per_page=100" \
    --jq '.check_runs[] | [.name, .status, (.conclusion // ""), .started_at] | @tsv')
  pending=0
  failed=0
  for check_name in "${REQUIRED_CHECKS[@]}"; do
    latest=$(echo "$checks" | awk -F '\t' -v n="$check_name" '$1 == n { print $4 "\t" $2 "\t" $3 }' | sort | tail -n 1)
    status=${latest#*$'\t'}
    status=${status%%$'\t'*}
    conclusion=${latest##*$'\t'}
    if [[ -z "$latest" ]]; then
      echo "Required check not reported yet: ${check_name}"
      pending=1
      continue
    fi
    if [[ "$status" != "completed" ]]; then
      echo "Required check still running: ${check_name} (${status})"
      pending=1
      continue
    fi
    if [[ "$conclusion" != "success" ]]; then
      echo "Required check is ${conclusion}: ${check_name}" >&2
      failed=1
      continue
    fi
    echo "Required check green: ${check_name}"
  done

  if [[ "$failed" -ne 0 ]]; then
    exit 1
  fi
  if [[ "$pending" -eq 0 ]]; then
    break
  fi
  if [[ "$DRY_RUN" == "true" ]]; then
    echo "Dry run: required checks are still running on ${sha}. Not waiting, approving, or enqueueing."
    exit 0
  fi
  if (( SECONDS >= deadline )); then
    echo "Required checks were still running after ${WAIT_SECONDS}s on ${sha}." >&2
    exit 1
  fi
  echo "Waiting ${POLL_SECONDS}s for required checks on ${sha}."
  sleep "$POLL_SECONDS"
done

echo "Pull request #${number} at ${sha} is ready (review=${review}, merge=${merge_state})."
if [[ "$DRY_RUN" == "true" ]]; then
  if [[ "$review" == "APPROVED" ]]; then
    echo "Dry run: would add pull request #${number} to the merge queue."
  else
    echo "Dry run: would approve pull request #${number} and add it to the merge queue."
  fi
  exit 0
fi

if [[ "$review" != "APPROVED" ]]; then
  gh api "repos/${REPO}/pulls/${number}/reviews" \
    -f event=APPROVE \
    -f body="Scheduled release approval. Required checks are green on ${sha}." >/dev/null
  echo "Approved pull request #${number}."
fi

# shellcheck disable=SC2016 # GraphQL variables are not shell variables
pr_id=$(gh api graphql \
  -f query='query($owner:String!, $name:String!, $number:Int!) { repository(owner:$owner, name:$name) { pullRequest(number:$number) { id } } }' \
  -f owner="${REPO%%/*}" \
  -f name="${REPO#*/}" \
  -F number="$number" \
  --jq '.data.repository.pullRequest.id')
err=$(mktemp)
trap 'rm -f "$err"' EXIT
entry=
for attempt in 1 2 3 4 5; do
  # shellcheck disable=SC2016 # GraphQL variables are not shell variables
  if entry=$(gh api graphql \
    -f query='mutation($id:ID!, $oid:GitObjectID) { enqueuePullRequest(input:{pullRequestId:$id, expectedHeadOid:$oid}) { mergeQueueEntry { position state } } }' \
    -f id="$pr_id" \
    -f oid="$sha" \
    --jq '.data.enqueuePullRequest.mergeQueueEntry' \
    2>"$err"); then
    echo "Queued pull request #${number} at ${sha}: ${entry}"
    exit 0
  fi
  echo "Enqueue attempt ${attempt} did not queue pull request #${number}:" >&2
  cat "$err" >&2
  sleep 5
done
exit 1
