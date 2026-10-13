import { ApiErrorAlert } from "@/components/api-error-alert";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import type { OrganizationUserSessionIssuerReference } from "@gram/client/models/components/organizationusersessionissuerreference.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";

import type { TrustSignInConfirmation } from "./oktaSignIn";

function AffectedUsers({
  servers,
  toolsets,
  lookupFailed,
}: {
  servers: OrganizationUserSessionIssuerReference[];
  toolsets: OrganizationUserSessionIssuerReference[];
  lookupFailed: boolean;
}): JSX.Element | null {
  if (lookupFailed) {
    return (
      <Alert variant="warning" dismissible={false}>
        Could not check which servers and toolsets use this sign-in issuer.
        People signing in to them may start signing in through Okta.
      </Alert>
    );
  }
  const affected = [
    ...servers.map((ref) => ({ ref, kind: "Server" })),
    ...toolsets.map((ref) => ({ ref, kind: "Toolset" })),
  ];
  if (affected.length === 0) return null;
  return (
    <Alert variant="warning" dismissible={false} alignTop>
      <Text small className="block">
        People signing in to these will sign in through Okta:
      </Text>
      <ul className="my-2 list-disc pl-5">
        {affected.map(({ ref, kind }) => (
          <li key={`${kind}:${ref.id}`}>
            <Text small>
              {kind}: {ref.name} ({ref.projectName})
            </Text>
          </li>
        ))}
      </ul>
    </Alert>
  );
}

export function TrustSignInConfirmDialog({
  confirmation,
  pending,
  error,
  onCancel,
  onConfirm,
}: {
  confirmation: TrustSignInConfirmation | null;
  pending: boolean;
  /** Shown in place so a retry stays on the issuer the admin confirmed. */
  error: Error | null;
  onCancel: () => void;
  onConfirm: (issuer: UserSessionIssuer) => void;
}): JSX.Element {
  return (
    <Dialog
      open={confirmation != null}
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
    >
      <Dialog.Content className="max-w-md">
        <Dialog.Header>
          <Dialog.Title>
            Trust Okta sign-in on {confirmation?.issuer.slug}?
          </Dialog.Title>
          <Dialog.Description>
            {confirmation?.trustsOther &&
              "This sign-in issuer trusts another sign-in client, which Okta replaces. "}
            Okta sign-in only admits people who already exist in this
            organization, so provision everyone through SCIM first.
          </Dialog.Description>
        </Dialog.Header>
        {confirmation && (
          <AffectedUsers
            servers={confirmation.servers}
            toolsets={confirmation.toolsets}
            lookupFailed={confirmation.lookupFailed}
          />
        )}
        <ApiErrorAlert error={error} />
        <Dialog.Footer>
          <Button variant="secondary" disabled={pending} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="destructive-primary"
            disabled={pending || !confirmation}
            onClick={() => {
              if (confirmation) onConfirm(confirmation.issuer);
            }}
          >
            Trust Okta sign-in
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
