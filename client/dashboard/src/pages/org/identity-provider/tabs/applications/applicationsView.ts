import type { BadgeProps } from "@/components/ui/Badge";
import { capitalize } from "@/lib/utils";
import type {
  ErrorT as ReconcileRunError,
  IdentityProviderConnectionReconcileRunStatus,
} from "@gram/client/models/components/identityproviderconnectionreconcilerun.js";

type BadgeVariant = NonNullable<BadgeProps["variant"]>;

const RECONCILE_ERRORS: Record<ReconcileRunError, string> = {
  rate_limited: "Okta received too many requests. Try syncing again later.",
  credential_rejected:
    "Okta rejected the connection. Review the setup on the Okta Setup tab.",
  okta_unreachable: "Okta could not be reached.",
  client_unavailable:
    "Speakeasy could not connect to Okta to sync applications.",
  superseded: "A newer sync finished first. Its results were kept.",
  discarded: "Sync stopped because the Okta connection was no longer verified.",
  interrupted: "Sync was interrupted before it finished. Try syncing again.",
};

export function reconcileErrorLabel(error: ReconcileRunError): string {
  return RECONCILE_ERRORS[error];
}

const RECONCILE_STATUS_VARIANTS: Record<
  IdentityProviderConnectionReconcileRunStatus,
  BadgeVariant
> = {
  running: "information",
  succeeded: "success",
  failed: "destructive",
};

const RECONCILE_STATUS_LABELS: Record<
  IdentityProviderConnectionReconcileRunStatus,
  string
> = {
  running: "Running",
  succeeded: "Succeeded",
  failed: "Failed",
};

export function reconcileStatusVariant(
  status: IdentityProviderConnectionReconcileRunStatus,
): BadgeVariant {
  return RECONCILE_STATUS_VARIANTS[status];
}

export function reconcileStatusLabel(
  status: IdentityProviderConnectionReconcileRunStatus,
): string {
  return RECONCILE_STATUS_LABELS[status];
}

const SIGN_ON_MODES: Record<string, string> = {
  OPENID_CONNECT: "OpenID Connect",
  SAML_2_0: "SAML 2.0",
  SAML_1_1: "SAML 1.1",
  WS_FEDERATION: "WS-Federation",
  BROWSER_PLUGIN: "Browser plugin",
  SECURE_PASSWORD_STORE: "Secure password store",
  AUTO_LOGIN: "Auto login",
  BASIC_AUTH: "Basic auth",
  BOOKMARK: "Bookmark",
};

/** Okta's SCREAMING_SNAKE tokens (sign-on modes, statuses) as readable labels. */
export function humanizeOktaToken(token: string): string {
  const known = SIGN_ON_MODES[token];
  if (known) return known;
  return capitalize(token.toLowerCase().replace(/_/g, " "));
}

export function applicationStatusVariant(status: string): BadgeVariant {
  return status === "ACTIVE" ? "success" : "neutral";
}
