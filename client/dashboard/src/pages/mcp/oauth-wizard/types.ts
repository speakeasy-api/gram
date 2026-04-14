import React from "react";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface DiscoveredOAuth {
  slug: string;
  name: string;
  version: string;
  metadata: Record<string, unknown>;
}

export interface ProxyFormData {
  slug: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  scopes: string;
  audience: string;
  tokenAuthMethod: string;
  environmentSlug: string;
}

export type WizardState =
  | { step: "path_selection" }
  | {
      step: "external_oauth_server_metadata_form";
      slug: string;
      metadataJson: string;
      jsonError: string | null;
      prefilled: boolean;
    }
  | {
      step: "oauth_proxy_server_metadata_form";
      slug: string;
      authorizationEndpoint: string;
      tokenEndpoint: string;
      scopes: string;
      audience: string;
      tokenAuthMethod: string;
      environmentSlug: string;
      error: string | null;
      prefilled: boolean;
    }
  | {
      step: "oauth_proxy_client_credentials_form";
      proxyFormData: ProxyFormData;
      clientId: string;
      clientSecret: string;
      error: string | null;
    };

export type WizardAction =
  | { type: "SELECT_EXTERNAL"; discoveredOAuth?: DiscoveredOAuth | null }
  | {
      type: "SELECT_PROXY";
      discoveredOAuth?: DiscoveredOAuth | null;
      defaults?: Partial<ProxyFormData>;
    }
  | { type: "BACK" }
  | { type: "PROXY_NEXT" }
  | { type: "UPDATE_FIELD"; field: string; value: string }
  | { type: "SET_ERROR"; error: string | null }
  | { type: "APPLY_DISCOVERED"; discoveredOAuth: DiscoveredOAuth }
  | { type: "RESET" };

export type WizardDispatch = React.Dispatch<WizardAction>;
