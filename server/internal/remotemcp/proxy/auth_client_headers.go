package proxy

// IsCredentialHeader reports whether a server header carries upstream
// credential material: Authorization, a secret value, or a value forwarded
// from the caller's request. Under the typed auth client resolver these
// belong to the selected client, never to the server.
func IsCredentialHeader(name string, secret bool, fromRequest bool) bool {
	return secret || fromRequest || headerKey(name) == "authorization"
}

// MergeAuthClientHeaders orders a resolved auth client's headers against the
// server's. Under [HeaderPolicyRemote] the server's headers come first and the
// client's follow, so the client wins a shared name; the resolved bearer is
// still applied after both. Under [HeaderPolicyTunneled] the given headers
// route the request, so a client header never takes one of their names.
func MergeAuthClientHeaders(policy HeaderPolicy, headers []ConfiguredHeader, client []ConfiguredHeader) []ConfiguredHeader {
	merged := make([]ConfiguredHeader, 0, len(headers)+len(client))
	if policy == HeaderPolicyTunneled {
		taken := headerKeys(headers)
		for _, h := range client {
			if !taken[headerKey(h.Name)] {
				merged = append(merged, h)
			}
		}
		return append(merged, headers...)
	}
	owned := headerKeys(client)
	for _, h := range headers {
		if !owned[headerKey(h.Name)] {
			merged = append(merged, h)
		}
	}
	return append(merged, client...)
}

func headerKeys(headers []ConfiguredHeader) map[string]bool {
	keys := make(map[string]bool, len(headers))
	for _, h := range headers {
		keys[headerKey(h.Name)] = true
	}
	return keys
}
