---
# Dashboard flow checks: can a user still finish each core product flow?
# See e2e/flows/README.md. Compile with `gh aw compile flow-checks`; the
# generated flow-checks.lock.yml is what Actions runs.
#
# The deterministic part runs as ordinary steps: boot the stack, replay every
# e2e/flows/*/flow.spec.ts. Only if a flow fails does a Claude Code agent run,
# inside GitHub Agentic Workflows' AWF sandbox: it re-walks the failing
# stories, says whether the UI moved (healed), the flow broke, or the PR
# changed it on purpose, and posts one PR comment through a safe output.
name: Flow checks
on:
  pull_request:
    paths:
      - client/dashboard/**
      - e2e/flows/**
      - e2e/flows.config.ts
      - .mise-tasks/test/flows.sh
      - .mise-tasks/flows/**
      - .mise-tasks/zero/env-deps.mts
      - .github/prompts/heal-flow.md
      - .github/workflows/flow-checks.*
  workflow_dispatch:

permissions:
  contents: read

concurrency:
  group: flow-checks-${{ github.ref }}
  cancel-in-progress: true

runs-on: blacksmith-8vcpu-ubuntu-2404
timeout-minutes: 40

engine:
  id: claude

# The agent reaches the local stack's dashboard, server and dev-idp on the
# runner (host ports need the docker-sudo-iptables profile) and no other host:
# no `network.allowed` entries beyond the defaults and the browser download.
sandbox:
  agent:
    id: awf
    runtime: docker-sudo-iptables
    allow-host-ports: [5173, 8080, 35291]
network:
  allowed:
    - defaults
    - playwright

checkout:
  # The agent compares the branch with main to tell a deliberate flow change
  # from a broken one.
  fetch-depth: 0

tools:
  playwright:
    version: "0.1.17"
  edit:
  # Inside the sandbox, which is the boundary: the agent has to run the
  # scripts it writes.
  bash: true

steps:
  - name: Setup Mise
    uses: jdx/mise-action@9149ea85001c7435d5a66bb127d6a1b6227cb0a5 # v5.0.0
    with:
      minimum_release_age: 1d
      install: true
      cache: true
      cache_key_prefix: mise-blacksmith-v1
      env: false
  - name: Restore Go module cache
    uses: ./.github/workflows/composite/go-mod-cache
  # Same answers as wake-stack-e2e.yml: zero must not prompt on a runner.
  - name: Answer zero's questions before it asks them
    run: |
      cat > mise.local.toml <<'TOML'
      [env]
      CLICKHOUSE_MIGRATION_ENGINE = "golang-migrate"
      TOML
      git config user.email "flow-checks@example.com"
      git config user.name "Flow Checks"
  # The agent runs in a container, where `localhost` is the container, so the
  # stack serves on the name the container reaches the runner by. The runner
  # resolves that name to itself, so the replay and the services agree.
  # The server and dev-idp listen on every interface: the sandbox's network
  # does not exist yet, so its gateway cannot be bound. This runner is
  # single-use, and the sandbox's firewall admits only the three stack ports.
  - name: Serve the stack to the agent's sandbox
    run: |
      echo "127.0.0.1 host.docker.internal" | sudo tee -a /etc/hosts
      mise run flows:sandbox-host host.docker.internal --bind-ip 0.0.0.0
  # ATLAS_TOKEN exists in this step only.
  - name: Boot the stack
    env:
      ATLAS_TOKEN: ${{ secrets.ATLAS_TOKEN }}
    run: ./zero --agent
  - name: Replay flows
    id: replay
    run: |
      status=0
      mise run test:flows || status=$?
      failing=$(jq -r '[.. | objects | select(.specs?) | .specs[]
        | select(.ok == false) | .file | split("/")[0]
        | select(test("\\A[a-z0-9-]+\\z"))] | unique | join(" ")' \
        e2e/.flows/results.json 2>/dev/null || true)
      mkdir -p /tmp/gh-aw/agent
      echo "$failing" > /tmp/gh-aw/agent/failing-flows.txt
      # Every flow passed: skip the agent entirely, at no cost.
      if [ "$status" = 0 ]; then
        echo '{"type":"noop","message":"Every flow replayed cleanly."}' >> "$GH_AW_SAFE_OUTPUTS"
      fi
      # The agent replays flows with the same environment.
      mise exec -- bash -c 'printf "GRAM_SITE_URL=%s\nGRAM_SERVER_URL=%s\n" "$GRAM_SITE_URL" "$GRAM_SERVER_URL"' |
        tee -a "$GITHUB_ENV" > /tmp/gh-aw/agent/stack.env
      # The stack's certificate is signed by a CA the runner trusts and the
      # sandbox's browser does not. The firewall, not TLS, bounds where it goes.
      echo "PLAYWRIGHT_MCP_IGNORE_HTTPS_ERRORS=true" >> "$GITHUB_ENV"

safe-outputs:
  add-comment:
    max: 1
    hide-older-comments: true
  upload-artifact:
    max-uploads: 1
    retention-days: 7
    allowed-paths:
      - "/tmp/gh-aw/agent/healed/**"
---

# Heal failing flow checks

The replay of the dashboard flow checks failed on this pull request. The
failing flows are listed in `/tmp/gh-aw/agent/failing-flows.txt`. If it is
empty, the replay failed before any flow reported a result: say so in the
comment and stop.

The local stack is running; its URLs are in `/tmp/gh-aw/agent/stack.env`
(`GRAM_SITE_URL` is the dashboard) and in your environment.

Commands in this run:

- Browser: `playwright-cli -s=<flow> <command>`.
- Replay one flow: `cd e2e && aube exec playwright test --config flows.config.ts flows/<flow>/`.
- Type-check: `aube run -F @gram/e2e type-check`.
- Branch diff: `git diff origin/main...HEAD -- client/ e2e/flows/`.

{{#runtime-import .github/prompts/heal-flow.md}}

## Report

When every failing flow has a verdict:

1. If any flow is `healed`, copy each healed script to
   `/tmp/gh-aw/agent/healed/<flow>/flow.spec.ts` and call `upload_artifact`
   once with `name: "healed-flows"` and `path: "/tmp/gh-aw/agent/healed"`.
2. Call `add_comment` once, on this pull request, with:
   - a table: Flow | Verdict | Step | Summary;
   - one line per verdict meaning: _healed_ (the UI moved, the flow still
     works; the new script is in the `healed-flows` artifact of this run),
     _broken_ (a user can no longer finish the flow), _changed_ (this PR
     changes the flow on purpose; update its `story.md`);
   - a fenced bash block, for each failing flow, with the commands to check
     or heal it locally:
     `mise run test:flows flows/<flow>/` and `mise run flows:heal <flow>`;
   - the line: This check does not block merging.
