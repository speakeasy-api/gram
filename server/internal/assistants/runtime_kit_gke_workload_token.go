package assistants

import _ "embed"

// GKE Agent Identity tokens are served only by the metadata server. This
// sidecar writes them to the token file Kit reads before each exchange.
const kitGKEIdentitySidecarImage = "docker.io/curlimages/curl:8.22.0@sha256:58adaa4e8dca9c988bae2aba4ab3434a0bb2da16bbe3f92dec39ec7785166777"

//go:embed runtime_kit_gke_workload_token.sh
var kitGKEIdentitySidecarScript string
