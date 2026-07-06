package relay

import (
	"net/url"
	"regexp"
	"strings"
)

// Credential material can ride along in MCP server transport: basic-auth
// userinfo and secret-named query parameters in a URL, or secret flags/tokens
// in a stdio launch command. Both become telemetry and Shadow MCP block
// evidence, so they are redacted before leaving the machine. Host, path, and
// non-secret arguments survive so the evidence stays matchable server-side.

var secretParamRE = regexp.MustCompile(`(?i)(key|token|secret|password|passwd|credential|auth)`)

// redactURL strips basic-auth userinfo and fragments and masks secret-named
// query values while preserving the host, path, and benign parameters.
func redactURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	u.Fragment = ""
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if secretParamRE.MatchString(k) {
				q.Set(k, "***")
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

var (
	secretEnvAssignRE = regexp.MustCompile(`(?i)^[A-Za-z_][A-Za-z0-9_]*(key|token|secret|password|passwd|credential|auth)[A-Za-z0-9_]*=`)
	secretAssignRE    = regexp.MustCompile(`(?i)^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*=`)
	secretFlagRE      = regexp.MustCompile(`(?i)^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*$`)
	secretHeaderRE    = regexp.MustCompile(`(?i)^(--?[^=]*=)?(authorization|proxy-authorization|cookie|x-api-key) *:`)
	tokenPrefixRE     = regexp.MustCompile(`(?i)://[^/@]*@|^(sk-|ghp_|gho_|github_pat_|xox[a-z]-|glpat-)`)
)

// redactCommand masks secret flag values and inline tokens in a stdio MCP
// launch command. Tokenization splits on spaces and cannot see through shell
// quoting; the patterns cover the common unquoted shapes, matching the bash
// senders' behavior so a repointed server keeps a stable redacted identity.
func redactCommand(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, `"`, "")
	raw = strings.ReplaceAll(raw, "'", "")
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	maskNext := false
	for _, f := range fields {
		if maskNext {
			// An auth-scheme word between a header and its credential
			// ("authorization: bearer TOKEN") is not the secret; keep the
			// mask pending for the value that follows it.
			if strings.EqualFold(f, "bearer") || strings.EqualFold(f, "basic") {
				out = append(out, f)
				continue
			}
			out = append(out, "***")
			maskNext = false
			continue
		}
		switch {
		case secretEnvAssignRE.MatchString(f):
			i := strings.IndexByte(f, '=')
			out = append(out, f[:i+1]+"***")
		case secretAssignRE.MatchString(f):
			if i := strings.IndexByte(f, '='); i >= 0 {
				out = append(out, f[:i+1]+"***")
			} else {
				out = append(out, "***")
			}
		case secretFlagRE.MatchString(f):
			out = append(out, f)
			maskNext = true
		case secretHeaderRE.MatchString(f):
			// `--header "X-API-Key: abc"` tokenizes the value into the next
			// field after quote stripping; a header with nothing after its
			// colon — or with only an auth scheme, as in
			// "Authorization:Bearer TOKEN" — must keep the mask pending for
			// the credential that follows.
			i := strings.IndexByte(f, ':')
			value := strings.TrimSpace(f[i+1:])
			switch {
			case value == "":
				out = append(out, f[:i]+":")
				maskNext = true
			case strings.EqualFold(value, "bearer") || strings.EqualFold(value, "basic"):
				out = append(out, f[:i]+": "+value)
				maskNext = true
			default:
				out = append(out, f[:i]+": ***")
			}
		case strings.EqualFold(f, "bearer"):
			out = append(out, f)
			maskNext = true
		case strings.Contains(f, "://"):
			// A server URL passed as an argument (npx mcp-remote <url>) can
			// carry credentials in userinfo or its query string; structured
			// redaction keeps the host and path matchable. Checked before
			// tokenPrefixRE so userinfo URLs are stripped, not swallowed
			// whole. A token the parser cannot make sense of could hide
			// credentials anywhere, so it is dropped entirely.
			if _, err := url.Parse(f); err != nil {
				out = append(out, "***")
			} else {
				out = append(out, redactURL(f))
			}
		case tokenPrefixRE.MatchString(f):
			out = append(out, "***")
		default:
			out = append(out, f)
		}
	}
	return strings.Join(out, " ")
}
