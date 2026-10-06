import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useState } from "react";
import { legacyCallbackURL } from "../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import { ConfirmDialog } from "./ConfirmDialog";

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
