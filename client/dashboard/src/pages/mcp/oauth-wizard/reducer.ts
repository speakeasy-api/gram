import type {
  DiscoveredOAuth,
  ProxyFormData,
  WizardAction,
  WizardState,
} from "./types";

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

export const INITIAL_STATE: WizardState = {
  step: "path_selection",
  title: "Connect OAuth",
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

export function applyExternalDiscovered(d: DiscoveredOAuth) {
  return {
    slug: d.slug,
    metadataJson: JSON.stringify(d.metadata, null, 2),
    jsonError: null,
    prefilled: true,
  };
}

export function applyProxyDiscovered(
  d: DiscoveredOAuth,
): Partial<ProxyFormData> {
  const m = d.metadata;
  const partial: Partial<ProxyFormData> = { slug: d.slug };
  if (typeof m.authorization_endpoint === "string")
    partial.authorizationEndpoint = m.authorization_endpoint;
  if (typeof m.token_endpoint === "string")
    partial.tokenEndpoint = m.token_endpoint;
  if (Array.isArray(m.scopes_supported))
    partial.scopes = m.scopes_supported.join(", ");
  return partial;
}

export function makeProxyState(
  overrides?: Partial<ProxyFormData> & { prefilled?: boolean; title?: string },
): Extract<WizardState, { step: "oauth_proxy_server_metadata_form" }> {
  return {
    step: "oauth_proxy_server_metadata_form",
    title: overrides?.title ?? "Configure OAuth Proxy",
    slug: overrides?.slug ?? "",
    authorizationEndpoint: overrides?.authorizationEndpoint ?? "",
    tokenEndpoint: overrides?.tokenEndpoint ?? "",
    scopes: overrides?.scopes ?? "",
    audience: overrides?.audience ?? "",
    tokenAuthMethod: overrides?.tokenAuthMethod ?? "client_secret_post",
    environmentSlug: overrides?.environmentSlug ?? "",
    error: null,
    prefilled: overrides?.prefilled ?? false,
  };
}

export function extractProxyFormData(
  s: Extract<WizardState, { step: "oauth_proxy_server_metadata_form" }>,
): ProxyFormData {
  return {
    slug: s.slug,
    authorizationEndpoint: s.authorizationEndpoint,
    tokenEndpoint: s.tokenEndpoint,
    scopes: s.scopes,
    audience: s.audience,
    tokenAuthMethod: s.tokenAuthMethod,
    environmentSlug: s.environmentSlug,
  };
}

// ---------------------------------------------------------------------------
// Reducer
// ---------------------------------------------------------------------------

export function wizardReducer(
  state: WizardState,
  action: WizardAction,
): WizardState {
  switch (action.type) {
    case "SELECT_EXTERNAL": {
      const discovered = action.discoveredOAuth
        ? applyExternalDiscovered(action.discoveredOAuth)
        : {};
      return {
        step: "external_oauth_server_metadata_form",
        title: "Configure External OAuth",
        slug: "",
        metadataJson: "",
        jsonError: null,
        prefilled: false,
        ...discovered,
      };
    }

    case "SELECT_PROXY": {
      const discovered = action.discoveredOAuth
        ? { ...applyProxyDiscovered(action.discoveredOAuth), prefilled: true }
        : {};
      return makeProxyState({
        ...action.defaults,
        ...discovered,
        title: action.title,
      });
    }

    case "BACK": {
      if (state.step === "oauth_proxy_client_credentials_form") {
        return makeProxyState(state.proxyFormData);
      }
      return INITIAL_STATE;
    }

    case "PROXY_NEXT": {
      if (state.step !== "oauth_proxy_server_metadata_form") return state;
      return {
        step: "oauth_proxy_client_credentials_form",
        title: "OAuth Client Credentials",
        proxyFormData: extractProxyFormData(state),
        clientId: "",
        clientSecret: "",
        error: null,
      };
    }

    case "UPDATE_FIELD": {
      if (state.step === "path_selection") return state;
      return { ...state, [action.field]: action.value } as WizardState;
    }

    case "SET_ERROR": {
      if (state.step === "path_selection") return state;
      return { ...state, error: action.error } as WizardState;
    }

    case "APPLY_DISCOVERED": {
      if (state.step === "external_oauth_server_metadata_form") {
        return { ...state, ...applyExternalDiscovered(action.discoveredOAuth) };
      }
      if (state.step === "oauth_proxy_server_metadata_form") {
        return {
          ...state,
          ...applyProxyDiscovered(action.discoveredOAuth),
          prefilled: true,
        };
      }
      return state;
    }

    case "SET_RESULT":
      return {
        step: "result",
        title: action.success ? "OAuth Configured" : "Configuration Failed",
        success: action.success,
        message: action.message,
      };

    case "RESET":
      return INITIAL_STATE;

    default:
      return state;
  }
}
