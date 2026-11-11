// Package geminivault exports managed Gemini app conversations through Google Vault.
package geminivault

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Required for the GCS transfer-integrity checksum.
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	vaultScope             = "https://www.googleapis.com/auth/ediscovery"
	storageScope           = "https://www.googleapis.com/auth/devstorage.read_only"
	orgUnitScope           = "https://www.googleapis.com/auth/admin.directory.orgunit.readonly"
	maxAPIBytes      int64 = 4 << 20
	maxDownloadBytes int64 = 128 << 20
	minRSAKeyBits          = 2048
	maxOrgUnits            = 10000
)

// ErrMatterNotOpen reports a matter Google Vault returned but cannot export
// from: closed or deleted matters accept no new exports.
var ErrMatterNotOpen = errors.New("vault matter must be OPEN; reopen it in Google Vault before exporting")

// Config is everything a client needs. ServiceAccountJSON is the secret key
// file; the rest is non-secret and stored beside it. MatterID is needed for
// export calls and OrgUnitID only to create exports, so setup can list org
// units and create the matter before either is chosen.
type Config struct {
	ServiceAccountJSON string
	// DelegatedUser is the Workspace admin the service account acts as. Vault
	// authorizes every call against this user's license and privileges.
	DelegatedUser string
	MatterID      string
	// OrgUnitID scopes exports. Vault documents ENTIRE_ORG as Gmail-only, so
	// organization-wide coverage means selecting the root organizational unit.
	OrgUnitID string
}

// ParseServiceAccount validates a Google service account key file locally,
// without contacting Google's token service. Errors deliberately exclude the
// supplied JSON and private key.
func ParseServiceAccount(raw string) error {
	var sa struct {
		Type     string `json:"type"`
		Email    string `json:"client_email"`
		Key      string `json:"private_key"`
		TokenURI string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(raw), &sa); err != nil || sa.Type != "service_account" || strings.TrimSpace(sa.Email) == "" {
		return errors.New("invalid Google service account key file")
	}
	if sa.TokenURI != "https://accounts.google.com/o/oauth2/token" && sa.TokenURI != "https://oauth2.googleapis.com/token" {
		return errors.New("service account token_uri must be a standard Google token endpoint")
	}
	block, rest := pem.Decode([]byte(sa.Key))
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return errors.New("invalid service account RSA private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if err != nil || !ok || rsaKey.Validate() != nil {
		return errors.New("invalid service account RSA private key")
	}
	// Google still accepts uploaded RSA_1024 service-account keys, which are
	// below the RFC 7518 minimum for RS256 signatures.
	if rsaKey.N.BitLen() < minRSAKeyBits {
		return errors.New("service account RSA private key must be at least 2048 bits")
	}
	return nil
}

// ValidateDelegatedUser checks the admin email locally. It must be a bare
// address: it becomes the JWT subject Google's token service resolves.
func ValidateDelegatedUser(email string) error {
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || parsed.Name != "" || len(email) > 320 {
		return errors.New("delegated admin must be a Workspace user email address")
	}
	return nil
}

type Client struct {
	// httpClient carries the Vault and Cloud Storage scopes; directoryClient
	// only the org-unit read scope, so export traffic never holds directory
	// access and a missing directory delegation cannot break exports.
	httpClient       *guardian.HTTPClient
	directoryClient  *guardian.HTTPClient
	matterID         string
	orgUnitID        string
	baseURL          string
	storageURL       string
	directoryURL     string
	maxAPIBytes      int64
	maxDownloadBytes int64
}

// NewClient authenticates with domain-wide delegation as cfg.DelegatedUser.
// Token exchanges and API requests both use guardian transports.
func NewClient(cfg Config, policy *guardian.Policy) (*Client, error) {
	if err := ParseServiceAccount(cfg.ServiceAccountJSON); err != nil {
		return nil, err
	}
	if err := ValidateDelegatedUser(cfg.DelegatedUser); err != nil {
		return nil, err
	}
	if cfg.MatterID != "" {
		if _, err := uuid.Parse(cfg.MatterID); err != nil {
			return nil, errors.New("vault matter ID must be a UUID")
		}
	}
	if policy == nil {
		return nil, errors.New("vault client requires guardian policy")
	}
	// No retries: export and matter creation are not idempotent. Never follow
	// redirects with service-account assertions, access tokens, or exported
	// conversation data.
	base := policy.PooledClient(guardian.WithCheckRedirect(denyRedirect))
	base.Timeout = 30 * time.Second
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, base)
	newHTTPClient := func(scopes ...string) (*guardian.HTTPClient, error) {
		config, err := google.JWTConfigFromJSON([]byte(cfg.ServiceAccountJSON), scopes...)
		if err != nil {
			return nil, errors.New("invalid Google service account configuration")
		}
		config.Subject = cfg.DelegatedUser
		transport := &oauth2.Transport{Source: config.TokenSource(ctx), Base: base.Transport}
		return &guardian.HTTPClient{Transport: transport, CheckRedirect: denyRedirect, Timeout: 2 * time.Minute}, nil
	}
	vaultHTTP, err := newHTTPClient(vaultScope, storageScope)
	if err != nil {
		return nil, err
	}
	directoryHTTP, err := newHTTPClient(orgUnitScope)
	if err != nil {
		return nil, err
	}
	return &Client{
		httpClient:       vaultHTTP,
		directoryClient:  directoryHTTP,
		matterID:         cfg.MatterID,
		orgUnitID:        cfg.OrgUnitID,
		baseURL:          "https://vault.googleapis.com/v1",
		storageURL:       "https://storage.googleapis.com/storage/v1",
		directoryURL:     "https://admin.googleapis.com/admin/directory/v1",
		maxAPIBytes:      maxAPIBytes,
		maxDownloadBytes: maxDownloadBytes,
	}, nil
}

func denyRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

type Export struct {
	ID                    string `json:"id"`
	Status                string `json:"status"`
	Files                 []File `json:"-"`
	ExportedArtifactCount int64  `json:"-"`
	TotalArtifactCount    int64  `json:"-"`
}

type File struct {
	BucketName string `json:"bucketName"`
	ObjectName string `json:"objectName"`
	Size       int64  `json:"size,string"`
	MD5Hash    string `json:"md5Hash"`
}

func (c *Client) matterPath() string { return c.baseURL + "/matters/" + url.PathEscape(c.matterID) }

// errNoMatter guards matter-scoped calls on a client built for setup only.
var errNoMatter = errors.New("vault matter ID is not configured")

// EnsureMatter returns the ID of an open matter with this name that the
// delegated admin can see, creating one when none exists. Reusing by name keeps
// reconnects and retried saves on one matter. Concurrent creates can leave
// duplicates, so the smallest ID wins.
func (c *Client) EnsureMatter(ctx context.Context, name, description string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("vault matter name is required")
	}
	var matches []string
	pageToken := ""
	seen := map[string]bool{}
	for {
		query := url.Values{"pageSize": {"100"}, "state": {"OPEN"}, "view": {"BASIC"}}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		data, err := c.request(ctx, http.MethodGet, c.baseURL+"/matters?"+query.Encode(), nil, c.maxAPIBytes)
		if err != nil {
			return "", err
		}
		var page struct {
			Matters []struct {
				ID    string `json:"matterId"`
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"matters"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return "", errors.New("invalid Vault matters response")
		}
		for _, matter := range page.Matters {
			if matter.Name != name || matter.State != "OPEN" {
				continue
			}
			if _, err := uuid.Parse(matter.ID); err != nil {
				return "", errors.New("invalid Vault matter response")
			}
			matches = append(matches, matter.ID)
		}
		if page.NextPageToken == "" {
			break
		}
		if seen[page.NextPageToken] || len(seen) >= 1000 {
			return "", errors.New("invalid or excessive Vault matter pagination")
		}
		pageToken = page.NextPageToken
		seen[pageToken] = true
	}
	if len(matches) > 0 {
		return slices.Min(matches), nil
	}
	data, err := c.request(ctx, http.MethodPost, c.baseURL+"/matters", map[string]string{"name": name, "description": description}, c.maxAPIBytes)
	if err != nil {
		return "", err
	}
	var created struct {
		ID    string `json:"matterId"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		return "", errors.New("invalid Vault matter response")
	}
	if _, err := uuid.Parse(created.ID); err != nil {
		return "", errors.New("invalid Vault matter response")
	}
	if created.State != "OPEN" {
		return "", ErrMatterNotOpen
	}
	return created.ID, nil
}

// OrgUnit is one Workspace organizational unit the delegated admin can read.
type OrgUnit struct {
	ID          string
	Path        string
	Name        string
	Description string
}

// ListOrgUnits returns every organizational unit including the root (path
// "/"), sorted by path, using only the directory read scope.
func (c *Client) ListOrgUnits(ctx context.Context) ([]OrgUnit, error) {
	data, err := c.requestWith(ctx, c.directoryClient, http.MethodGet, c.directoryURL+"/customer/my_customer/orgunits?type=allIncludingParent", nil, c.maxAPIBytes)
	if err != nil {
		return nil, err
	}
	var page struct {
		Units []struct {
			ID                string `json:"orgUnitId"`
			Path              string `json:"orgUnitPath"`
			Name              string `json:"name"`
			Description       string `json:"description"`
			ParentID          string `json:"parentOrgUnitId"`
			ParentOrgUnitPath string `json:"parentOrgUnitPath"`
		} `json:"organizationUnits"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, errors.New("invalid Workspace organizational units response")
	}
	if len(page.Units) > maxOrgUnits {
		return nil, errors.New("workspace has too many organizational units to list")
	}
	units := make([]OrgUnit, 0, len(page.Units)+1)
	hasRoot := false
	rootID := ""
	for _, unit := range page.Units {
		if unit.ID == "" || !strings.HasPrefix(unit.Path, "/") {
			return nil, errors.New("invalid Workspace organizational unit")
		}
		hasRoot = hasRoot || unit.Path == "/"
		if unit.ParentOrgUnitPath == "/" && unit.ParentID != "" {
			rootID = unit.ParentID
		}
		units = append(units, OrgUnit{ID: unit.ID, Path: unit.Path, Name: unit.Name, Description: unit.Description})
	}
	// Some tenants omit the root even with allIncludingParent; every top-level
	// unit still names it as its parent.
	if !hasRoot && rootID != "" {
		units = append(units, OrgUnit{ID: rootID, Path: "/", Name: "", Description: ""})
	}
	slices.SortFunc(units, func(a, b OrgUnit) int { return strings.Compare(a.Path, b.Path) })
	return units, nil
}

func (c *Client) GetMatter(ctx context.Context) error {
	if c.matterID == "" {
		return errNoMatter
	}
	data, err := c.request(ctx, http.MethodGet, c.matterPath()+"?view=BASIC", nil, c.maxAPIBytes)
	if err != nil {
		return err
	}
	var matter struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &matter); err != nil {
		return errors.New("invalid Vault matter response")
	}
	if matter.State != "OPEN" {
		return ErrMatterNotOpen
	}
	return nil
}

// CreateExport uses ORG_UNIT, not ENTIRE_ORG (which is Gmail-only).
// A root OU ID must be configured explicitly; an empty OU is not documented.
func (c *Client) CreateExport(ctx context.Context, name string, start, end time.Time) (*Export, error) {
	if c.matterID == "" {
		return nil, errNoMatter
	}
	if strings.TrimSpace(c.orgUnitID) == "" {
		return nil, errors.New("vault Gemini exports require org_unit_id; supply the root organizational unit ID for organization-wide coverage")
	}
	if strings.TrimSpace(name) == "" || start.IsZero() || !end.After(start) {
		return nil, errors.New("vault export requires a name and valid time window")
	}
	body := map[string]any{"name": name, "query": map[string]any{"corpus": "GEMINI", "dataScope": "ALL_DATA", "searchMethod": "ORG_UNIT", "orgUnitInfo": map[string]string{"orgUnitId": c.orgUnitID}, "startTime": start.UTC().Format(time.RFC3339Nano), "endTime": end.UTC().Format(time.RFC3339Nano)}, "exportOptions": map[string]any{"geminiOptions": map[string]string{"exportFormat": "XML"}}}
	data, err := c.request(ctx, http.MethodPost, c.matterPath()+"/exports", body, c.maxAPIBytes)
	if err != nil {
		return nil, err
	}
	return parseExport(data)
}

func (c *Client) GetExport(ctx context.Context, id string) (*Export, error) {
	if c.matterID == "" {
		return nil, errNoMatter
	}
	if id == "" || id == "." || id == ".." {
		return nil, errors.New("invalid Vault export ID")
	}
	data, err := c.request(ctx, http.MethodGet, c.matterPath()+"/exports/"+url.PathEscape(id), nil, c.maxAPIBytes)
	if err != nil {
		return nil, err
	}
	return parseExport(data)
}

// DeleteExport deletes a job in this client's fixed matter. Callers must not
// remove a successful job until their ingestion checkpoint is durable.
func (c *Client) DeleteExport(ctx context.Context, id string) error {
	if c.matterID == "" {
		return errNoMatter
	}
	if id == "" || id == "." || id == ".." {
		return errors.New("invalid Vault export ID")
	}
	_, err := c.request(ctx, http.MethodDelete, c.matterPath()+"/exports/"+url.PathEscape(id), nil, c.maxAPIBytes)
	return err
}

// FindExport locates a deterministic export name across all pages. Multiple
// matches are rejected rather than choosing an arbitrary job after a lost reply.
func (c *Client) FindExport(ctx context.Context, name string) (*Export, error) {
	if c.matterID == "" {
		return nil, errNoMatter
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("vault export name is required")
	}
	var found *Export
	pageToken := ""
	seen := map[string]bool{}
	for {
		query := url.Values{"pageSize": {"100"}}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		data, err := c.request(ctx, http.MethodGet, c.matterPath()+"/exports?"+query.Encode(), nil, c.maxAPIBytes)
		if err != nil {
			return nil, err
		}
		var page struct {
			Exports       []json.RawMessage `json:"exports"`
			NextPageToken string            `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, errors.New("invalid Vault exports response")
		}
		for _, raw := range page.Exports {
			var entry struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &entry); err != nil {
				return nil, errors.New("invalid Vault export response")
			}
			if entry.Name != name {
				continue
			}
			if found != nil {
				return nil, errors.New("multiple Vault exports match the requested name")
			}
			found, err = parseExport(raw)
			if err != nil {
				return nil, err
			}
		}
		if page.NextPageToken == "" {
			return found, nil
		}
		if seen[page.NextPageToken] || len(seen) >= 1000 {
			return nil, errors.New("invalid or excessive Vault export pagination")
		}
		pageToken = page.NextPageToken
		seen[pageToken] = true
	}
}

func parseExport(data []byte) (*Export, error) {
	var wire struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Stats  struct {
			Exported int64 `json:"exportedArtifactCount,string"`
			Total    int64 `json:"totalArtifactCount,string"`
		} `json:"stats"`
		Sink struct {
			Files []File `json:"files"`
		} `json:"cloudStorageSink"`
	}
	if err := json.Unmarshal(data, &wire); err != nil || wire.ID == "" || wire.Status == "" {
		return nil, errors.New("invalid Vault export response")
	}
	return &Export{ID: wire.ID, Status: wire.Status, Files: wire.Sink.Files, ExportedArtifactCount: wire.Stats.Exported, TotalArtifactCount: wire.Stats.Total}, nil
}

// Download reads a bounded GCS object on a fixed host. No upstream-supplied URL
// is followed, and slash-containing object names are escaped as one path segment.
func (c *Client) Download(ctx context.Context, file File) ([]byte, error) {
	if file.BucketName == "" || file.ObjectName == "" || file.BucketName == "." || file.BucketName == ".." || file.ObjectName == "." || file.ObjectName == ".." || file.Size < 0 || file.Size > c.maxDownloadBytes {
		return nil, errors.New("invalid or oversized Vault export file")
	}
	data, err := c.request(ctx, http.MethodGet, c.storageURL+"/b/"+url.PathEscape(file.BucketName)+"/o/"+url.PathEscape(file.ObjectName)+"?alt=media", nil, c.maxDownloadBytes)
	if err != nil {
		return nil, err
	}
	if file.Size > 0 && int64(len(data)) != file.Size {
		return nil, errors.New("vault export file size mismatch")
	}
	if file.MD5Hash != "" {
		// A transfer-integrity checksum, not a security primitive.
		digest := md5.Sum(data) //nolint:gosec // Export protocol checksum.
		want, ok := decodeMD5(file.MD5Hash)
		if !ok || !bytes.Equal(digest[:], want) {
			return nil, errors.New("vault export file checksum mismatch")
		}
	}
	return data, nil
}

// decodeMD5 reads Vault's CloudStorageFile.md5Hash, whose encoding Google does
// not document. Cloud Storage object metadata uses base64; accept hex as well.
// The forms cannot collide: an MD5 is 32 hex characters or 24 base64 ones.
func decodeMD5(encoded string) ([]byte, bool) {
	if raw, err := hex.DecodeString(encoded); err == nil && len(raw) == md5.Size {
		return raw, true
	}
	if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(raw) == md5.Size {
		return raw, true
	}
	return nil, false
}

// HTTPError exposes only the status code, never upstream response bodies.
// TokenExchange marks a status from Google's OAuth token endpoint rather than
// from Vault or Cloud Storage.
type HTTPError struct {
	StatusCode    int
	TokenExchange bool
}

func (e *HTTPError) Error() string {
	if e.TokenExchange {
		return fmt.Sprintf("vault token exchange returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("vault request returned HTTP %d", e.StatusCode)
}

// CredentialsRejected reports that Google refused to mint a token for the
// configured service account. The token endpoint answers 400 invalid_grant for
// a deleted key or unknown delegated user and 401 unauthorized_client when
// domain-wide delegation does not grant the scopes; no retry can fix either.
func (e *HTTPError) CredentialsRejected() bool {
	return e.TokenExchange && e.StatusCode >= 400 && e.StatusCode < 500 && e.StatusCode != http.StatusTooManyRequests
}

func (c *Client) request(ctx context.Context, method, endpoint string, body any, limit int64) ([]byte, error) {
	return c.requestWith(ctx, c.httpClient, method, endpoint, body, limit)
}

func (c *Client) requestWith(ctx context.Context, client *guardian.HTTPClient, method, endpoint string, body any, limit int64) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, errors.New("encode Vault request")
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, errors.New("create Vault request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("vault request cancelled: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("vault request cancelled: %w", ctx.Err())
		}
		// Keep only the token endpoint's status, never its upstream body,
		// matching the API error path below.
		if retrieveErr, ok := errors.AsType[*oauth2.RetrieveError](err); ok && retrieveErr.Response != nil {
			return nil, &HTTPError{StatusCode: retrieveErr.Response.StatusCode, TokenExchange: true}
		}
		return nil, errors.New("vault request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: resp.StatusCode, TokenExchange: false}
	}
	if resp.ContentLength > limit {
		return nil, errors.New("vault response exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, errors.New("read Vault response")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("vault response exceeds size limit")
	}
	return data, nil
}
