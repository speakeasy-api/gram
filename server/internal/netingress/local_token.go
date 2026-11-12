package netingress

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Local attestation replaces Kubernetes TokenReview in local development,
// where no cluster exists to mint or review service-account tokens. Tokens
// carry the claims a projected service-account token carries (subject,
// audience, expiry) and are HMAC-signed with a key on the developer's machine,
// so the attestation verifier runs unchanged against them. Production never
// accepts them: the private listener only builds a LocalTokenReviewer in the
// local environment.

const (
	// LocalTokenIssuer marks tokens minted for local development, so a token
	// signed with the same key for another purpose is not accepted.
	LocalTokenIssuer = "gram-local-netingress" //nolint:gosec // issuer name, not a credential

	// localTokenKeyBytes is the size of a generated signing key. 32 bytes
	// matches the HMAC-SHA256 block output, the RFC 7518 minimum for HS256.
	localTokenKeyBytes = 32

	// localTokenKeyFileMode keeps the signing key readable only by its owner.
	localTokenKeyFileMode fs.FileMode = 0o600
	// localTokenDirMode is used when creating the directories that hold the
	// key and token files.
	localTokenDirMode fs.FileMode = 0o700
)

// ErrLocalTokenRejected reports a local attestation token that failed
// verification.
var ErrLocalTokenRejected = errors.New("local attestation token rejected")

// LocalTokenReviewer reviews HMAC-signed local attestation tokens with the
// same request and result shape as the Kubernetes TokenReview API.
type LocalTokenReviewer struct {
	key []byte
	now func() time.Time
}

var _ TokenReviewer = (*LocalTokenReviewer)(nil)

// NewLocalTokenReviewer returns a reviewer for tokens signed with key.
func NewLocalTokenReviewer(key []byte) (*LocalTokenReviewer, error) {
	if len(key) < localTokenKeyBytes {
		return nil, fmt.Errorf("local attestation key must be at least %d bytes", localTokenKeyBytes)
	}
	return &LocalTokenReviewer{key: slices.Clone(key), now: time.Now}, nil
}

// Create reviews the token in review. Like the Kubernetes API, a rejected
// token is reported in the returned status rather than as an error. With no
// requested audiences the token's own audiences are reported; otherwise the
// token must carry one of them and only the matches are reported.
func (r *LocalTokenReviewer) Create(_ context.Context, review *authenticationv1.TokenReview, _ metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
	if review == nil {
		return nil, errors.New("token review is required")
	}

	result := review.DeepCopy()
	audiences, subject, err := r.verify(review.Spec.Token, review.Spec.Audiences)
	if err != nil {
		//nolint:exhaustruct // a rejected review carries only the error, as the Kubernetes API does.
		result.Status = authenticationv1.TokenReviewStatus{Authenticated: false, Error: err.Error()}
		return result, nil
	}

	//nolint:exhaustruct // service-account tokens carry no UID, groups or extra attributes here.
	result.Status = authenticationv1.TokenReviewStatus{
		Authenticated: true,
		User:          authenticationv1.UserInfo{Username: subject},
		Audiences:     audiences,
	}
	return result, nil
}

func (r *LocalTokenReviewer) verify(token string, requested []string) ([]string, string, error) {
	claims := jwt.RegisteredClaims{} //nolint:exhaustruct // populated by the parser
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return r.key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(LocalTokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(r.now),
	)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrLocalTokenRejected, err)
	}
	if _, _, err := parseServiceAccountSubject(claims.Subject); err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrLocalTokenRejected, err)
	}

	audiences := []string(claims.Audience)
	if len(requested) > 0 {
		audiences = slices.DeleteFunc(slices.Clone(audiences), func(audience string) bool {
			return !slices.Contains(requested, audience)
		})
		if len(audiences) == 0 {
			return nil, "", fmt.Errorf("%w: token audience does not match", ErrLocalTokenRejected)
		}
	}
	return audiences, claims.Subject, nil
}

// LocalTokenClaims describes the identity a local attestation token asserts.
type LocalTokenClaims struct {
	// Namespace is the service account's namespace.
	Namespace string

	// ServiceAccount is the service account name.
	ServiceAccount string

	// Audience is the audience the token is valid for.
	Audience string

	// TTL is how long the token stays valid from issue.
	TTL time.Duration
}

// MintLocalToken signs a local attestation token for claims, issued at now.
func MintLocalToken(key []byte, claims LocalTokenClaims, now time.Time) (string, error) {
	if len(key) < localTokenKeyBytes {
		return "", fmt.Errorf("local attestation key must be at least %d bytes", localTokenKeyBytes)
	}
	if claims.Audience == "" || claims.TTL <= 0 {
		return "", errors.New("local attestation token needs an audience and a positive lifetime")
	}
	subject := fmt.Sprintf("system:serviceaccount:%s:%s", claims.Namespace, claims.ServiceAccount)
	if _, _, err := parseServiceAccountSubject(subject); err != nil {
		return "", fmt.Errorf("local attestation token identity: %w", err)
	}

	//nolint:exhaustruct // JWT ID and not-before are not used by attestation.
	registered := jwt.RegisteredClaims{
		Issuer:    LocalTokenIssuer,
		Subject:   subject,
		Audience:  jwt.ClaimStrings{claims.Audience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(claims.TTL)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, registered).SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign local attestation token: %w", err)
	}
	return signed, nil
}

// LoadOrCreateLocalTokenKey reads the signing key at path, creating a random
// one first if the file does not exist. Concurrent callers agree on one key:
// each writes a candidate to a temporary file and hard-links it into place,
// which succeeds for exactly one of them and never exposes a partial key.
func LoadOrCreateLocalTokenKey(path string) ([]byte, error) {
	if key, err := ReadLocalTokenKey(path); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return key, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, localTokenDirMode); err != nil {
		return nil, fmt.Errorf("create local attestation key directory: %w", err)
	}

	key := make([]byte, localTokenKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate local attestation key: %w", err)
	}

	candidate, err := os.CreateTemp(dir, ".key-*")
	if err != nil {
		return nil, fmt.Errorf("create local attestation key: %w", err)
	}
	defer func() { _ = os.Remove(candidate.Name()) }()

	_, writeErr := candidate.Write(key)
	chmodErr := candidate.Chmod(localTokenKeyFileMode)
	closeErr := candidate.Close()
	if err := errors.Join(writeErr, chmodErr, closeErr); err != nil {
		return nil, fmt.Errorf("write local attestation key: %w", err)
	}

	switch err := os.Link(candidate.Name(), path); {
	case errors.Is(err, fs.ErrExist):
		return ReadLocalTokenKey(path)
	case err != nil:
		return nil, fmt.Errorf("install local attestation key: %w", err)
	}
	return key, nil
}

// ReadLocalTokenKey reads an existing signing key.
func ReadLocalTokenKey(path string) ([]byte, error) {
	// #nosec G304 -- path is developer-supplied process configuration, not request input.
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local attestation key: %w", err)
	}
	if len(key) < localTokenKeyBytes {
		return nil, fmt.Errorf("local attestation key must be at least %d bytes", localTokenKeyBytes)
	}
	return key, nil
}

// WriteLocalToken replaces the token file at path atomically, so a reader
// never sees a partial token.
func WriteLocalToken(path, token string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, localTokenDirMode); err != nil {
		return fmt.Errorf("create local attestation token directory: %w", err)
	}

	file, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return fmt.Errorf("create local attestation token: %w", err)
	}
	_, writeErr := file.WriteString(token)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(file.Name())
		return fmt.Errorf("write local attestation token: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		_ = os.Remove(file.Name())
		return fmt.Errorf("install local attestation token: %w", err)
	}
	return nil
}
