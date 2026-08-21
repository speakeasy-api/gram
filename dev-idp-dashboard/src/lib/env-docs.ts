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
      "Base URL for OIDC auth. Points at the dev-idp /oauth2-1 authorization server.",
  },
  {
    name: "WORKOS_API_URL",
    description:
      "Base URL the Gram server uses to call the WorkOS REST API. Points at the dev-idp /workos surface in both backends; GRAM_DEVIDP_BACKEND decides what serves it.",
  },
  {
    name: "GRAM_DEVIDP_EXTERNAL_URL",
    description:
      "Externally reachable URL of the dev-idp listener. The mode detector compares WORKOS_API_URL against this base.",
  },
  {
    name: "GRAM_IDP_CLIENT_SECRET",
    description:
      "WorkOS API key. The local backend accepts any value; the workos backend needs a real sk_test_... key for the passthrough.",
    sensitive: true,
  },
] as const;
