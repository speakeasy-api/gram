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
  | { step: "path_selection"; title: string }
  | {
      step: "external_oauth_server_metadata_form";
      title: string;
      slug: string;
      metadataJson: string;
      jsonError: string | null;
      prefilled: boolean;
    }
  | {
      step: "oauth_proxy_server_metadata_form";
      title: string;
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
      title: string;
      proxyFormData: ProxyFormData;
      clientId: string;
      clientSecret: string;
      error: string | null;
    }
  | {
      step: "result";
      title: string;
      success: boolean;
      message: string;
    };

export type WizardAction =
  | { type: "SELECT_EXTERNAL"; discoveredOAuth?: DiscoveredOAuth | null }
  | {
      type: "SELECT_PROXY";
      discoveredOAuth?: DiscoveredOAuth | null;
      defaults?: Partial<ProxyFormData>;
      title?: string;
    }
  | { type: "BACK" }
  | {
      type: "PROXY_NEXT";
      prefilledClientId?: string;
      prefilledClientSecret?: string;
      tokenAuthMethod?: string;
    }
  | { type: "UPDATE_FIELD"; field: string; value: string }
  | { type: "SET_ERROR"; error: string | null }
  | { type: "APPLY_DISCOVERED"; discoveredOAuth: DiscoveredOAuth }
  | { type: "SET_RESULT"; success: boolean; message: string }
  | { type: "RESET" };

export type WizardDispatch = React.Dispatch<WizardAction>;
