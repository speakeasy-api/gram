package localspeakeasy

// WorkOS-shaped wire types served by the local-speakeasy mode's
// /user_management/*, /organizations/*, and /authorization/* endpoints.
// Field names + JSON tags mirror workos-go/v6 SDK types so Gram-side's
// `*workos.Client` can decode our responses with zero changes when its
// base URL is pointed at us instead of api.workos.com.

// listMetadata is the cursor pagination envelope every WorkOS list
// endpoint wraps results in. We only support forward pagination, so
// `Before` is always empty.
type listMetadata struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

type workosUser struct {
	ID                string            `json:"id"`
	FirstName         string            `json:"first_name"`
	LastName          string            `json:"last_name"`
	Email             string            `json:"email"`
	EmailVerified     bool              `json:"email_verified"`
	ProfilePictureURL string            `json:"profile_picture_url"`
	CreatedAt         string            `json:"created_at"`
	UpdatedAt         string            `json:"updated_at"`
	LastSignInAt      string            `json:"last_sign_in_at"`
	ExternalID        string            `json:"external_id"`
	Metadata          map[string]string `json:"metadata"`
}

type workosUserList struct {
	Data         []workosUser `json:"data"`
	ListMetadata listMetadata `json:"list_metadata"`
}

type workosOrganization struct {
	ID                               string                     `json:"id"`
	Name                             string                     `json:"name"`
	AllowProfilesOutsideOrganization bool                       `json:"allow_profiles_outside_organization"`
	Domains                          []workosOrganizationDomain `json:"domains"`
	StripeCustomerID                 string                     `json:"stripe_customer_id,omitempty"`
	CreatedAt                        string                     `json:"created_at"`
	UpdatedAt                        string                     `json:"updated_at"`
	ExternalID                       string                     `json:"external_id"`
}

type workosOrganizationDomain struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Domain         string `json:"domain"`
	State          string `json:"state"`
}

type workosRoleSlug struct {
	Slug string `json:"slug"`
}

type workosOrganizationMembership struct {
	ID               string         `json:"id"`
	UserID           string         `json:"user_id"`
	OrganizationID   string         `json:"organization_id"`
	OrganizationName string         `json:"organization_name"`
	Role             workosRoleSlug `json:"role"`
	Status           string         `json:"status"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

type workosOrganizationMembershipList struct {
	Data         []workosOrganizationMembership `json:"data"`
	ListMetadata listMetadata                   `json:"list_metadata"`
}

type workosInvitation struct {
	ID                  string `json:"id"`
	Email               string `json:"email"`
	State               string `json:"state"`
	AcceptedAt          string `json:"accepted_at"`
	RevokedAt           string `json:"revoked_at"`
	Token               string `json:"token"`
	AcceptInvitationURL string `json:"accept_invitation_url"`
	OrganizationID      string `json:"organization_id"`
	InviterUserID       string `json:"inviter_user_id"`
	ExpiresAt           string `json:"expires_at"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

type workosInvitationList struct {
	Data         []workosInvitation `json:"data"`
	ListMetadata listMetadata       `json:"list_metadata"`
}

// workosRole mirrors organizations.Role in workos-go. The dev-idp doesn't
// distinguish system from custom roles — every emulated role is `Type =
// "EnvironmentRole"` (the WorkOS default).
type workosRole struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Type        string `json:"type"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type workosRoleList struct {
	Data         []workosRole `json:"data"`
	ListMetadata listMetadata `json:"list_metadata"`
}
