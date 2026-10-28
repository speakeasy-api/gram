# Managed Sandbox workload identity PoC

This PoC does not change application identity records or deploy application code.
It tests the Google metadata ID-token route with the **production workload
assertion verifier**, without logging or storing bearer tokens.

## What is proved

- `main.go` reads a token from stdin, fetches Google's fixed public JWKS, and
  invokes the production verifier with an independently supplied numeric subject.
- It checks exact issuer, subject, audience, lifetime, and signature; then checks
  cached-token reuse and rejection of another subject/audience.
- Replay memory is process-local and for this probe only, not a production store.
- `render.py` creates an isolated namespace, IAM-linked KSA, managed
  SandboxTemplate, zero-spare SandboxWarmPool, and SandboxClaim (v1beta1). There are no projected tokens or keys.
- Egress permits only the metadata endpoint at `169.254.169.254:80`, for GKE
  Dataplane V2. Ingress is empty. No core admission policies are modified.

## Build and local checks

From the repository root:

```sh
mise exec -- go test ./server/cmd/dev-kit-identity-poc
mise exec -- go build -o /tmp/kit-identity-poc ./server/cmd/dev-kit-identity-poc
python3 -m unittest discover -s server/cmd/dev-kit-identity-poc -p 'test_*.py'
```

## Isolated dev setup (operator-run)

These commands CREATE IAM and Kubernetes resources. Supply the dev project,
assistant-cluster context, and a digest-pinned image containing `/bin/sh`, `sleep`,
and `curl`. Do not use production or an existing application IAM account.
The dedicated IAM account receives no project/resource roles.

```sh
export DEV_PROJECT='<DEV_PROJECT_ID>'
export DEV_CONTEXT='<DEV_ASSISTANT_CLUSTER_CONTEXT>'
export PROBE_IMAGE='<IMAGE>@sha256:<DIGEST>'
export POC="kit-wif-poc-$(date +%s)"
export IAM_EMAIL="$POC@$DEV_PROJECT.iam.gserviceaccount.com"
export AUDIENCE='https://example.invalid/kit-identity-poc'

# Verify the exact target before creating anything.
kubectl --context "$DEV_CONTEXT" cluster-info
gcloud projects describe "$DEV_PROJECT" --format='value(projectId)'

gcloud iam service-accounts create "$POC" --project "$DEV_PROJECT" \
  --display-name='Temporary Kit workload identity PoC'
gcloud iam service-accounts add-iam-policy-binding "$IAM_EMAIL" \
  --project "$DEV_PROJECT" --role=roles/iam.workloadIdentityUser \
  --member="serviceAccount:$DEV_PROJECT.svc.id.goog[$POC/probe]"

python3 server/cmd/dev-kit-identity-poc/render.py --namespace "$POC" \
  --iam-email "$IAM_EMAIL" --image "$PROBE_IMAGE" |
  kubectl --context "$DEV_CONTEXT" apply -f -

# Wait for the controller to create a pod, then for readiness.
for attempt in $(seq 1 60); do
  POD=$(kubectl --context "$DEV_CONTEXT" -n "$POC" get pods \
    -l app=kit-wif-poc -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  test -n "$POD" && break
  sleep 5
done
test -n "$POD" || { echo 'No probe pod created'; exit 1; }
kubectl --context "$DEV_CONTEXT" -n "$POC" wait pod/"$POD" \
  --for=condition=Ready --timeout=180s
```

## Probe

Obtain the expected subject from IAM, **not from the token being verified**.
The raw token flows only through a pipe; do not enable shell tracing or tee it.
IAM propagation can take time; retry a failed probe explicitly after inspecting
only sanitized errors. Never replace the expected subject with a token claim.

```sh
SUBJECT=$(gcloud iam service-accounts describe "$IAM_EMAIL" \
  --project "$DEV_PROJECT" --format='value(uniqueId)')
set -o pipefail
kubectl --context "$DEV_CONTEXT" -n "$POC" exec "$POD" -c probe -- \
  curl --fail --silent --show-error --max-time 15 \
    -H 'Metadata-Flavor: Google' \
    --get --data-urlencode "audience=$AUDIENCE" \
    'http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/identity' |
  /tmp/kit-identity-poc -subject "$SUBJECT" -audience "$AUDIENCE"
```

Successful output contains only issuer, subject, audience, expiry, and three
boolean verification results. Repeat after the original token expiry to prove
renewal; success before expiry proves cached reuse, not renewal.

## Cleanup (operator-run)

Only delete the new PoC resources identified above. Do not substitute an
application namespace or service account.

```sh
kubectl --context "$DEV_CONTEXT" delete namespace "$POC" --wait=true
gcloud iam service-accounts delete "$IAM_EMAIL" --project "$DEV_PROJECT"
```

## Scope limits

This is not a full OAuth exchange, agent assignment, inference, MCP, rotation
controller, or Slack end-to-end test. The existing workload issuer/admission/
agent-assignment model supports the verified external subject, but this PoC
intentionally writes no database records. Platform MCP needs no change for this
internal diagnostic command. No new durable schema is introduced.
