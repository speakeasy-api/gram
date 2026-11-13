import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { getServerURL } from "@/lib/utils";
import type { AgentCredentialFormat } from "./credential";

/** How a service account client proves itself at the token endpoint. */
export type ServiceAccountOAuthFormat = "client-secret" | "signing-key";

/** Every way a Service Account can authenticate upstream. */
export type ServiceAccountFormat =
  | Exclude<AgentCredentialFormat, "client-credentials">
  | ServiceAccountOAuthFormat
  | "github-app";

export type SecretMethod = "client_secret_basic" | "client_secret_post";
export type AssertionAudience = "issuer" | "token_endpoint";

export function isOAuthFormat(
  format: ServiceAccountFormat,
): format is ServiceAccountOAuthFormat {
  return format === "client-secret" || format === "signing-key";
}

export function oauthFormatOf(
  client: RemoteSessionClient,
): ServiceAccountOAuthFormat {
  return client.tokenEndpointAuthMethod === "private_key_jwt"
    ? "signing-key"
    : "client-secret";
}

/**
 * Which key set a client signs with: one the organization already has, or a
 * new one to create from a Google Cloud KMS key when the client is created.
 */
export type KeySetChoice =
  | { kind: "existing"; id: string }
  | { kind: "new"; name: string; externalKeyId: string };

export function keySetChoiceReady(choice: KeySetChoice | null): boolean {
  if (!choice) return false;
  if (choice.kind === "existing") return choice.id !== "";
  return choice.name.trim() !== "" && choice.externalKeyId !== "";
}

/** Where the private key that signs the client's token requests lives. */
export type SigningKeySource = "speakeasy" | "kms";

export type SigningKeySourceOption = {
  value: SigningKeySource;
  title: string;
  description: string;
  /** Shown but not choosable: the backend can't do it yet. */
  comingSoon: boolean;
};

export function signingKeySourceOptions(): SigningKeySourceOption[] {
  return [
    {
      value: "speakeasy",
      title: "Speakeasy-Managed",
      description: "A new key Speakeasy holds and rotates.",
      comingSoon: true,
    },
    {
      value: "kms",
      title: "Self-Managed",
      description: "Sign with a key set backed by a Google Cloud KMS key.",
      comingSoon: false,
    },
  ];
}

/**
 * Where a provider reads a client's public keys. Served for any client with a
 * key set attached, at the platform origin only, so it is built from the
 * server URL rather than the page's.
 */
export function clientKeySetUrl(clientRowId: string): string {
  return `${getServerURL().replace(/\/+$/, "")}/.well-known/oauth-client/${clientRowId}/jwks.json`;
}
