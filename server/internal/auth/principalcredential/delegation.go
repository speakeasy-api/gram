package principalcredential

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// DelegationTokenType is the typ header of a delegation. It differs from
	// TokenType so a delegation never authenticates a request by itself.
	DelegationTokenType = "gram-principal-delegation+jwt"

	// DelegationAudience is the only audience a delegation is valid for: the
	// token endpoint that exchanges it.
	DelegationAudience = "urn:gram:principal-delegation"
)

// A delegation lets exactly one workload actor exchange an agent credential
// that acts for a user (RFC 8693 §4.4 may_act). It carries the same claims as
// the credential it exchanges for, so admission is unchanged.
type delegationClaims struct {
	claims
	MayAct mayAct `json:"may_act"`
}

type mayAct struct {
	Subject string `json:"sub"`
}

// MintDelegation signs a delegation of c to actor. c must be an agent acting
// for a user, and actor a workload principal.
func (i *Issuer) MintDelegation(c Credential, actor urn.Principal) (string, string, error) {
	if err := checkShape(c); err != nil {
		return "", "", err
	}
	if c.Principal.Type != urn.PrincipalTypeAgent {
		return "", "", ErrInvalid
	}
	if _, _, err := actor.Workload(); err != nil {
		return "", "", ErrInvalid
	}
	now := time.Now()
	id := uuid.NewString()
	var profileVersion *uuid.UUID
	if c.SelectionProfileVersionID.Valid {
		profileVersion = &c.SelectionProfileVersionID.UUID
	}
	raw, err := i.signer.SignRS256(delegationClaims{
		claims: claims{
			OrganizationID: c.OrganizationID, ProjectID: c.ProjectID, AuthorizerUserID: c.AuthorizerUserID,
			SelectionProfileVersionID: profileVersion,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer: i.signer.URL(), Subject: c.Principal.String(), Audience: jwt.ClaimStrings{DelegationAudience},
				ExpiresAt: jwt.NewNumericDate(now.Add(Lifetime)), NotBefore: nil, IssuedAt: jwt.NewNumericDate(now), ID: id,
			},
		},
		MayAct: mayAct{Subject: actor.String()},
	}, DelegationTokenType)
	if err != nil {
		return "", "", fmt.Errorf("sign principal delegation: %w", err)
	}
	return raw, id, nil
}

// ValidateDelegation verifies raw and returns the credential it delegates and
// the only actor that may exchange it.
func (i *Issuer) ValidateDelegation(raw string) (Authenticated, urn.Principal, error) {
	var c delegationClaims
	if err := i.signer.ParseRS256(raw, &c, DelegationTokenType, DelegationAudience); err != nil {
		return Authenticated{}, urn.Principal{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if len(c.Audience) != 1 || c.ID == "" {
		return Authenticated{}, urn.Principal{}, ErrInvalid
	}
	principal, err := urn.ParsePrincipal(c.Subject)
	if err != nil || principal.Type != urn.PrincipalTypeAgent {
		return Authenticated{}, urn.Principal{}, ErrInvalid
	}
	actor, err := urn.ParsePrincipal(c.MayAct.Subject)
	if err != nil {
		return Authenticated{}, urn.Principal{}, ErrInvalid
	}
	if _, _, err := actor.Workload(); err != nil {
		return Authenticated{}, urn.Principal{}, ErrInvalid
	}
	profileVersion := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if c.SelectionProfileVersionID != nil {
		profileVersion = uuid.NullUUID{UUID: *c.SelectionProfileVersionID, Valid: true}
	}
	credential := Credential{OrganizationID: c.OrganizationID, ProjectID: c.ProjectID, Principal: principal, AuthorizerUserID: c.AuthorizerUserID, SelectionProfileVersionID: profileVersion}
	if err := checkShape(credential); err != nil {
		return Authenticated{}, urn.Principal{}, err
	}
	return Authenticated{ID: c.ID, Credential: credential}, actor, nil
}
