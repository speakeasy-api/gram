package mockoidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Provider struct {
	cfg        *Config
	logger     *slog.Logger
	issuer     string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	keyID      string

	hmacKey []byte

	mu           sync.Mutex
	codes        map[string]*codeEntry
	accessTokens map[string]*tokenEntry
}

type codeEntry struct {
	clientID            string
	redirectURI         string
	subject             string
	user                User
	scope               string
	nonce               string
	codeChallenge       string
	codeChallengeMethod string
	expiresAt           time.Time
}

type tokenEntry struct {
	subject   string
	user      User
	scope     string
	expiresAt time.Time
}

const (
	codeTTL  = 60 * time.Second
	tokenTTL = time.Hour
)

func NewProvider(cfg *Config, logger *slog.Logger, issuer string, privateKey *rsa.PrivateKey) (*Provider, error) {
	hmacKey := make([]byte, 32)
	if _, err := rand.Read(hmacKey); err != nil {
		return nil, fmt.Errorf("generate hmac key: %w", err)
	}

	pub := &privateKey.PublicKey
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	sum := sha256.Sum256(der)
	kid := base64.RawURLEncoding.EncodeToString(sum[:])

	return &Provider{
		cfg:          cfg,
		logger:       logger,
		issuer:       issuer,
		privateKey:   privateKey,
		publicKey:    pub,
		keyID:        kid,
		hmacKey:      hmacKey,
		codes:        make(map[string]*codeEntry),
		accessTokens: make(map[string]*tokenEntry),
	}, nil
}

func LoadOrGeneratePrivateKey(privatePath string, logger *slog.Logger) (*rsa.PrivateKey, error) {
	if privatePath == "" {
		logger.Warn("no private key path provided; generating ephemeral RSA-2048 key (JWKS will change every restart)")
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("generate rsa key: %w", err)
		}
		return key, nil
	}

	data, err := os.ReadFile(privatePath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("private key file %s does not contain a PEM block", privatePath)
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse pkcs1 private key: %w", err)
		}
		return key, nil
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse pkcs8 private key: %w", err)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA (got %T)", key)
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM type %q in private key", block.Type)
	}
}

func (p *Provider) Issuer() string { return p.issuer }
func (p *Provider) KeyID() string  { return p.keyID }

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

func (p *Provider) JWKS() jwks {
	n := base64.RawURLEncoding.EncodeToString(p.publicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.publicKey.E)).Bytes())
	return jwks{
		Keys: []jwk{{
			Kty: "RSA",
			Use: "sig",
			Alg: "RS256",
			Kid: p.keyID,
			N:   n,
			E:   e,
		}},
	}
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (p *Provider) MintCode(entry *codeEntry) (string, error) {
	code, err := randomToken(32)
	if err != nil {
		return "", err
	}
	entry.expiresAt = time.Now().Add(codeTTL)

	p.mu.Lock()
	p.codes[code] = entry
	p.mu.Unlock()
	return code, nil
}

func (p *Provider) ConsumeCode(code string) (*codeEntry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.codes[code]
	if !ok {
		return nil, false
	}
	delete(p.codes, code)
	if time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry, true
}

func (p *Provider) MintAccessToken(user User, scope string) (string, time.Time, error) {
	tok, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(tokenTTL)
	p.mu.Lock()
	p.accessTokens[tok] = &tokenEntry{
		subject:   user.Subject(),
		user:      user,
		scope:     scope,
		expiresAt: expires,
	}
	p.mu.Unlock()
	return tok, expires, nil
}

func (p *Provider) LookupAccessToken(tok string) (*tokenEntry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.accessTokens[tok]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(p.accessTokens, tok)
		return nil, false
	}
	return entry, true
}

func (p *Provider) SignIDToken(user User, clientID, nonce string, expires time.Time) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":            p.issuer,
		"sub":            user.Subject(),
		"aud":            clientID,
		"exp":            expires.Unix(),
		"iat":            now.Unix(),
		"email":          user.Email,
		"email_verified": user.EmailVerified,
		"name":           user.Name,
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	if user.Picture != "" {
		claims["picture"] = user.Picture
	}
	if user.HD != "" {
		claims["hd"] = user.HD
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = p.keyID

	signed, err := tok.SignedString(p.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign id token: %w", err)
	}
	return signed, nil
}

func (p *Provider) Sweep() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, v := range p.codes {
		if now.After(v.expiresAt) {
			delete(p.codes, k)
		}
	}
	for k, v := range p.accessTokens {
		if now.After(v.expiresAt) {
			delete(p.accessTokens, k)
		}
	}
}

func (p *Provider) RunSweeper(stop <-chan struct{}) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			p.Sweep()
		}
	}
}
