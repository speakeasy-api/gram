#!/usr/bin/env bash
#MISE description="Smoke-test Monty worker startup in the restricted Linux image"
#MISE dir="{{ config_root }}"
#MISE depends=["build:code-runner-image"]
set -euo pipefail
container="gram-code-runner-smoke-$$"
export GRAM_CODE_RUNNER_TOKEN
GRAM_CODE_RUNNER_TOKEN="$(openssl rand -hex 32)"
trap 'docker rm --force "$container" >/dev/null 2>&1 || true' EXIT
docker run --detach --name "$container" --env GRAM_CODE_RUNNER_TOKEN \
  --publish 127.0.0.1::8081 --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges:true --pids-limit=64 --memory=512m --cpus=2 \
  --tmpfs=/tmp:rw,noexec,nosuid,size=16m --stop-timeout=40 gram-code-runner:test >/dev/null
for ((attempt=0; attempt<30; attempt++)); do
  address="$(docker port "$container" 8081/tcp 2>/dev/null || true)"
  if [[ -n "$address" ]] && curl --max-time 1 --silent --fail "http://$address/health" >/dev/null; then
    # State::new warms a real Monty worker before the health listener starts.
    docker stop --time 40 "$container" >/dev/null
    if [[ "$(docker inspect --format '{{.State.ExitCode}}' "$container")" != 0 ]]; then
      docker logs "$container" >&2
      echo 'Code runner failed during shutdown' >&2
      exit 1
    fi
    echo 'Restricted Linux image started a Monty worker and shut down cleanly'
    exit 0
  fi
  if [[ "$(docker inspect --format '{{.State.Running}}' "$container")" != true ]]; then
    docker logs "$container" >&2
    exit 1
  fi
  sleep 1
done
docker logs "$container" >&2
echo 'Code runner image never became ready' >&2
exit 1
