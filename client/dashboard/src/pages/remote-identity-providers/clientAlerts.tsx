import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { remoteSessionScopeTier } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { useState } from "react";
import { Link } from "react-router";
import { legacyCallbackURL } from "../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import { ConfirmDialog } from "./ConfirmDialog";

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

// LegacyCallbackAlert flags, to platform admins only, a client registered
// upstream with the legacy callback URL. Migrating clears compatibility mode,
// so sign-ins send the current callback URL, which must already be registered
// with the identity provider.
export function LegacyCallbackAlert({
  legacyCallbackUrl,
  callbackUrl,
  onMigrate,
  isMigrating = false,
  canMigrate,
  className,
}: {
  legacyCallbackUrl: boolean;
  // callbackUrl is the client's current redirect URI, as the server reports
  // it. The legacy URL shares its origin.
  callbackUrl: string | undefined;
  onMigrate: () => void;
  isMigrating?: boolean;
  // False when the caller lacks the permission the save needs, so the button
  // is hidden rather than offered only to fail.
  canMigrate: boolean;
  className?: string;
}): JSX.Element | null {
  const isPlatformAdmin = useIsPlatformAdmin();
  const [confirming, setConfirming] = useState(false);

  if (!legacyCallbackUrl || !isPlatformAdmin || !callbackUrl) return null;

  const current = callbackUrl;

  return (
    <Alert variant="warning" dismissible={false} alignTop className={className}>
      <div className="flex flex-col items-start gap-3">
        <span>
          This app was registered with the{" "}
          <span className="font-mono">{legacyCallbackURL(current)}</span> URL.
          Its replacement callback is{" "}
          <span className="font-mono">{current}</span>. This app runs in
          compatibility mode with the old URL. Register the replacement in order
          to migrate it.
        </span>
        {canMigrate && (
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setConfirming(true)}
          >
            <Button.Text>Migrate</Button.Text>
          </Button>
        )}
      </div>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Migrate to the new callback URL?"
        description={
          <>
            Sign-ins will send <span className="font-mono">{current}</span>. If
            that URL is not registered with the identity provider yet, sign-ins
            will fail until it is.
          </>
        }
        confirmLabel="Migrate"
        confirmVariant="primary"
        onConfirm={onMigrate}
        isPending={isMigrating}
      />
    </Alert>
  );
}
