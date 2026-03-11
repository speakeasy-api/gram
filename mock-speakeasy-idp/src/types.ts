export interface SpeakeasyUser {
  id: string;
  email: string;
  display_name: string;
  photo_url: string | null;
  github_handle: string | null;
  admin: boolean;
  created_at: string;
  updated_at: string;
  whitelisted: boolean;
}

export interface SpeakeasyOrganization {
  id: string;
  name: string;
  slug: string;
  created_at: string;
  updated_at: string;
  account_type: string;
  sso_connection_id: string | null;
  user_workspaces_slugs: string[];
}

export interface ValidateTokenResponse {
  user: SpeakeasyUser;
  organizations: SpeakeasyOrganization[];
}

export interface TokenExchangeRequest {
  code: string;
}

export interface TokenExchangeResponse {
  id_token: string;
}

export interface CreateOrgRequest {
  organization_name: string;
  account_type: string;
}
