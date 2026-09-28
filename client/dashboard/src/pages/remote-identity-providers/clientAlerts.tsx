import { Alert } from "@/components/ui/Alert";
import { useRBAC } from "@/hooks/useRBAC";
import { remoteSessionScopeTier } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Link } from "react-router";

// IssuerScopeOverrideAlert warns beside a client's scope field that the parent
// remote identity provider pins the requested scopes. The override replaces
// the client's scopes on the authorize redirect, so editing them here changes
// nothing. Renders nothing when the provider has no override.
export function IssuerScopeOverrideAlert({
  issuer,
}: {
  issuer:
    | {
        id: string;
        projectId?: string | null;
        organizationId?: string | null;
        scopeOverride?: string[] | null;
      }
    | undefined;
}): JSX.Element | null {
  if (!issuer?.scopeOverride?.length) return null;

  return (
    <Alert variant="warning" dismissible={false} alignTop>
      This client's remote identity provider has a scope override, so every
      sign-in requests{" "}
      <span className="font-mono">{issuer.scopeOverride.join(" ")}</span> and
      changing the client's scopes has no effect.{" "}
      {/* Platform providers have no settings page, and the organization
          cannot edit their override. */}
      {remoteSessionScopeTier(issuer) === "platform" ? (
        "The platform remote identity provider sets this override, and it can't be changed from this organization."
      ) : (
        <EditScopeOverrideHint issuerId={issuer.id} />
      )}
    </Alert>
  );
}

// Split out so the routing and RBAC hooks only run when there is an editable
// override to warn about.
function EditScopeOverrideHint({
  issuerId,
}: {
  issuerId: string;
}): JSX.Element {
  const routes = useRoutes();
  const { hasAnyScope } = useRBAC();

  // The provider's detail page requires org:read/org:admin; without them the
  // destination is named but not linked, matching IssuerLink.
  const settings = hasAnyScope(["org:read", "org:admin"]) ? (
    <Link
      to={routes.remoteIdentityProviders.issuerDetail.settings.href(issuerId)}
      className="font-medium underline"
    >
      remote identity provider's settings
    </Link>
  ) : (
    "remote identity provider's settings"
  );

  return <>Edit the scope override in the {settings} instead.</>;
}

// LegacyCallbackAlert flags a client registered upstream with the legacy
// callback URL. Its sign-ins send that URL and a JSON state rather than the
// current callback, so it can behave differently from its neighbors.
export function LegacyCallbackAlert({
  legacyCallbackUrl,
}: {
  legacyCallbackUrl: boolean;
}): JSX.Element | null {
  if (!legacyCallbackUrl) return null;

  return (
    <Alert variant="warning" dismissible={false} alignTop>
      This client uses the legacy callback URLs. It was registered with the
      identity provider under the legacy callback URL, so its sign-ins send that
      URL and a JSON state instead of the current callback.
    </Alert>
  );
}
