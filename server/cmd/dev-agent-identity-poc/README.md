# Native Agent Identity PoC

Requests an unbound Agent Identity token from a managed GKE sandbox and checks it
with the production workload verifier. No application identity records, IAM
accounts or bindings, GCP projects, or core-pool configuration are changed.

## Prerequisites

- GKE **1.37.0-gke.3503000+**, including sandbox nodes; Workload Identity Federation
  enabled; managed Agent Sandbox CRDs; gVisor sandbox nodes; Dataplane V2.
- Agent Registry API enabled and token-exchange quota available. Check with
  `gcloud services list --enabled --project "$DEV_PROJECT" --filter='name:agentregistry.googleapis.com'`.
  If absent, API enablement is a separate cloud write requiring
  `serviceusage.services.enable`:
  `gcloud services enable agentregistry.googleapis.com --project "$DEV_PROJECT"`.
- A matching dev Kubernetes context and a registry-accessible, digest-pinned
  probe image built below. The SDK is **google-auth 2.62.0**, whose signature is
  `fetch_id_token(request, audience, bind_id_token=None)`.

Official documentation:
[identity](https://cloud.google.com/kubernetes-engine/security/about-agent-identity),
[request](https://cloud.google.com/kubernetes-engine/security/request-agent-identity),
[authentication](https://cloud.google.com/kubernetes-engine/security/authenticate-agent-identity),
[SDK package](https://pypi.org/project/google-auth/2.62.0/).

## Build and test

From the repository root:

```sh
mise exec -- go test ./server/cmd/dev-agent-identity-poc
uv run --no-project --python 3.13 --with-requirements server/cmd/dev-agent-identity-poc/requirements.txt \
  python -m unittest discover -s server/cmd/dev-agent-identity-poc -p 'test_*.py'
mise exec -- go build -o /tmp/native-agent-identity-poc ./server/cmd/dev-agent-identity-poc

docker build --platform linux/amd64 -t native-agent-identity-poc:local \
  server/cmd/dev-agent-identity-poc
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges native-agent-identity-poc:local \
  python3 -m unittest -v test_request_token
```

Use `linux/arm64` for ARM sandbox nodes. The image pins its Python base by digest
and dependencies by version and hash; SDK contract tests run during the build.
It runs as UID/GID 1000 with a read-only filesystem and needs no runtime installs.
Publish through your registry workflow, then use the registry manifest digest,
not the local image ID. To update the dependency lock:

```sh
uv pip compile --python-version 3.13 --python-platform linux --generate-hashes \
  --no-header --no-annotate server/cmd/dev-agent-identity-poc/requirements.in \
  -o server/cmd/dev-agent-identity-poc/requirements.txt
```

## Configure

Run in bash. Confirm the context targets the specified cluster and the cluster
and sandbox nodes meet the prerequisites. Obtain trust configuration from GCP,
never from the token under test.

```bash
set -euo pipefail
set +x
DEV_PROJECT='<DEV_PROJECT_ID>'
DEV_CONTEXT='<DEV_KUBECTL_CONTEXT>'
CLUSTER='<CLUSTER_NAME>'
LOCATION='<CONTROL_PLANE_REGION_OR_ZONE>'
PROBE_IMAGE='<REGISTRY/IMAGE>@sha256:<64_HEX_DIGEST>'
POC="kit-agent-poc-$(date -u +%Y%m%d%H%M%S)"
AUDIENCE='https://example.invalid/native-agent-identity-poc'

PROJECT_NUMBER=$(gcloud projects describe "$DEV_PROJECT" --format='value(projectNumber)')
ANCESTORS=$(gcloud projects get-ancestors "$DEV_PROJECT" --format=json)
ORG_ID=$(printf '%s' "$ANCESTORS" | jq -r '.[] | select(.type == "organization") | .id')
gcloud container clusters describe "$CLUSTER" --project "$DEV_PROJECT" \
  --location "$LOCATION" --format='yaml(name,location,currentMasterVersion,workloadIdentityConfig,networkConfig)'
kubectl --context "$DEV_CONTEXT" cluster-info
kubectl --context "$DEV_CONTEXT" get nodes -l workload-type=sandbox \
  -o custom-columns=NAME:.metadata.name,VERSION:.status.nodeInfo.kubeletVersion

if [ -n "$ORG_ID" ]; then
  TRUST_DOMAIN="agents.global.org-${ORG_ID}.system.id.goog"
  SCOPE_ARGS=(-organization-id "$ORG_ID")
else
  TRUST_DOMAIN="agents.global.proj-${PROJECT_NUMBER}.system.id.goog"
  SCOPE_ARGS=(-standalone-project)
fi
```

Expected issuer is
`https://sts.googleapis.com/v1/organizations/ORG_ID/locations/global/workloadIdentityPools/TRUST_DOMAIN`,
or `/v1/projects/PROJECT_NUMBER/...` for a project without an organization.
Expected subject is
`spiffe://TRUST_DOMAIN/resources/container/projects/PROJECT_NUMBER/locations/LOCATION/clusters/CLUSTER/ns/NAMESPACE/sa/native-agent-probe`.
The verifier fetches `ISSUER/openid/jwks` locally over TLS without redirects.

## Create the sandbox

These commands create a fresh namespace, dedicated unlinked KSA, managed template,
zero-spare warm pool, and one claim. The claim creates one active sandbox.

```bash
EXISTING=$(kubectl --context "$DEV_CONTEXT" get namespace "$POC" --ignore-not-found -o name)
[ -z "$EXISTING" ] || { echo 'Refusing existing namespace'; exit 1; }
python3 server/cmd/dev-agent-identity-poc/render.py --namespace "$POC" \
  --trust-domain "$TRUST_DOMAIN" --image "$PROBE_IMAGE" |
  kubectl --context "$DEV_CONTEXT" create -f -

POD=''
for attempt in $(seq 1 60); do
  POD=$(kubectl --context "$DEV_CONTEXT" -n "$POC" get pods -l app=kit-agent-poc \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  [ -n "$POD" ] && break
  sleep 5
done
[ -n "$POD" ] || { echo 'No probe pod created'; exit 1; }
kubectl --context "$DEV_CONTEXT" -n "$POC" wait pod/"$POD" \
  --for=condition=Ready --timeout=180s
kubectl --context "$DEV_CONTEXT" -n "$POC" get pod "$POD" -o yaml
kubectl --context "$DEV_CONTEXT" -n "$POC" get networkpolicy -o yaml
```

Check admission preserved the dedicated KSA, automount false, identity annotation,
gVisor on sandbox nodes, and no KSA token or certificate mounts.

**Isolation:** managed explicit ingress/egress lists replace the default managed
rules; separate Kubernetes NetworkPolicies are additive. This template permits
only metadata TCP/80 at `169.254.169.254/32`, with empty ingress. The SDK uses the
literal IP, so no DNS or internet egress is needed. Keep managed policies and core
admission rules unchanged; do not move the probe onto core nodes or broaden egress.

## Verify and clean up

The token must flow directly to verifier stdin. Do not run the left side alone,
use shell tracing, `tee`, or save token output. SDK errors are sanitized; the
verifier reports only verified claims, expiry, and negative-check results.

```bash
kubectl --context "$DEV_CONTEXT" -n "$POC" exec -i "$POD" -c probe -- \
  python3 - "$AUDIENCE" < server/cmd/dev-agent-identity-poc/request_token.py |
  /tmp/native-agent-identity-poc -project-number "$PROJECT_NUMBER" \
    "${SCOPE_ARGS[@]}" -location "$LOCATION" -cluster "$CLUSTER" \
    -namespace "$POC" -service-account native-agent-probe -audience "$AUDIENCE"

# Cleanup after verification; retain the sandbox if testing renewal after expiry.
case "$POC" in kit-agent-poc-*) ;; *) echo 'Refusing unexpected namespace'; exit 1;; esac
kubectl --context "$DEV_CONTEXT" delete namespace "$POC" --wait=true
rm -f /tmp/native-agent-identity-poc
```

## Verification scope

The production verifier checks signature, exact issuer/subject/audience, and
expiry with its normal skew and one-hour acceptance ceiling. The probe additionally
requires expiry strictly in the future and rejects any `cnf` claim. Only RS256
unbound tokens are supported; replay state is process-local. Negative checks use
the same authentic token with a different expected audience or subject. Tests also
cover wrong issuer/signature, expired tokens, and certificate-bound rejection.

This does not establish proof-of-possession, mTLS, application admission, OAuth
exchange, agent assignment, or end-to-end product behavior. Renewal requires
repeating verification after the original token expires. Keep verified project
identifiers internal, not in public commits or reports.
