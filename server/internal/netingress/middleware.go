package netingress

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const AttestationHeader = middleware.PrivateIngressAttestationHeader

type WorkloadVerifier interface {
	Verify(ctx context.Context, token, source string) (Ingress, error)
}

func Middleware(verifier WorkloadVerifier, parsers IdentityParsers, telemetry ...*Telemetry) func(http.Handler) http.Handler {
	var metrics *Telemetry
	if len(telemetry) > 0 {
		metrics = telemetry[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			started := time.Now()
			record := func(result, reason, provider string) {
				metrics.Record(request.Context(), OperationAdmission, result, reason, provider, time.Since(started))
			}
			token, ok := bearerToken(request.Header.Values(AttestationHeader))
			request.Header.Del(AttestationHeader)
			if !ok {
				record(ResultDenied, ReasonMissingAttestation, "")
				http.Error(w, "workload attestation required", http.StatusUnauthorized)
				return
			}
			if verifier == nil {
				record(ResultError, ReasonVerifierUnavailable, "")
				http.Error(w, "private ingress unavailable", http.StatusServiceUnavailable)
				return
			}

			source, err := transportSource(request.RemoteAddr)
			if err != nil {
				record(ResultError, ReasonInvalidSource, "")
				http.Error(w, "private ingress unavailable", http.StatusServiceUnavailable)
				return
			}
			ingress, err := verifier.Verify(request.Context(), token, source)
			if err != nil {
				if errors.Is(err, ErrAttestationRejected) {
					record(ResultDenied, ReasonAttestationRejected, "")
					http.Error(w, "invalid workload attestation", http.StatusUnauthorized)
				} else {
					record(ResultError, ReasonDependencyFailed, "")
					http.Error(w, "private ingress unavailable", http.StatusServiceUnavailable)
				}
				return
			}

			host, err := requestorigin.CanonicalHost(request.Host)
			if err != nil || host != ingress.DNSName {
				record(ResultDenied, ReasonHostMismatch, ingress.Provider)
				http.NotFound(w, request)
				return
			}

			StripUnsupportedTailscaleHeaders(request.Header)
			identity, err := parsers.Parse(ingress.Provider, request.Header)
			if err != nil {
				if errors.Is(err, ErrUnsupportedProvider) {
					record(ResultError, ReasonProviderUnsupported, ingress.Provider)
					http.Error(w, "private ingress unavailable", http.StatusServiceUnavailable)
				} else {
					record(ResultDenied, ReasonIdentityInvalid, ingress.Provider)
					http.Error(w, "invalid network identity", http.StatusUnauthorized)
				}
				return
			}
			if ingress.IdentityRequired && identity == nil {
				record(ResultDenied, ReasonIdentityRequired, ingress.Provider)
				http.Error(w, "network identity required", http.StatusUnauthorized)
				return
			}
			DeleteTailscaleIdentityHeaders(request.Header)

			baseURL, err := requestorigin.HTTPSBaseURL(ingress.DNSName)
			if err != nil {
				record(ResultError, ReasonOriginInvalid, ingress.Provider)
				http.Error(w, "private ingress unavailable", http.StatusServiceUnavailable)
				return
			}
			record(ResultAllowed, ReasonNone, ingress.Provider)
			origin := requestorigin.Origin{
				Surface:          requestorigin.SurfacePrivateNetwork,
				BaseURL:          baseURL,
				OrganizationID:   ingress.OrganizationID,
				NetworkIngressID: ingress.ID,
				NetworkIdentity:  identity,
			}
			next.ServeHTTP(w, request.WithContext(requestorigin.WithContext(request.Context(), origin)))
		})
	}
}

func transportSource(remoteAddr string) (string, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil || net.ParseIP(host) == nil {
		return "", errors.New("invalid private ingress transport source")
	}
	return host, nil
}

func bearerToken(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	value := values[0]
	space := strings.IndexByte(value, ' ')
	if space <= 0 || !strings.EqualFold(value[:space], "Bearer") {
		return "", false
	}
	token := value[space+1:]
	if token == "" || strings.TrimSpace(token) != token {
		return "", false
	}
	return token, true
}
