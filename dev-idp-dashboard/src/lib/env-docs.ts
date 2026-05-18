/**
 * Documented Gram-server-relevant env vars surfaced by the
 * `/api/gram-mode` endpoint and rendered by `EnvReadout`. The list is
 * deliberately small — focused on the auth/IDP knobs that change which mode
 * Gram talks to. Add new entries here when more configuration becomes
 * relevant for the dashboard's reader.
 */
export interface EnvDoc {
  name: string;
  description: string;
  /** When true, the API masks the actual value and only reports is_set. */
  sensitive?: boolean;
}

export const ENV_DOCS: readonly EnvDoc[] = [
  {
    name: "GRAM_IDP_BASE_URL",
    description:
      "Base URL for OIDC auth (token exchange, userinfo). Typically points at the dev-idp's /oauth2 endpoint.",
  },
  {
    name: "WORKOS_API_URL",
    description:
      "Base URL the Gram server uses to call WorkOS REST API. Drives mode detection: when this starts with $GRAM_DEVIDP_EXTERNAL_URL/mock-workos, the dashboard reports mock-workos mode.",
  },
  {
    name: "GRAM_DEVIDP_EXTERNAL_URL",
    description:
      "Externally reachable URL of the dev-idp listener. The mode detector compares WORKOS_API_URL against this base.",
  },
  {
    name: "WORKOS_API_KEY",
    description:
      "API key the dev-idp uses for its live /workos passthrough. The /workos mount is only active when this is set.",
    sensitive: true,
  },
] as const;
