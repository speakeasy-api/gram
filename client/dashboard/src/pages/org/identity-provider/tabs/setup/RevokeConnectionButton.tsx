import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Field, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useRevokeIdentityProviderConnectionMutation } from "@gram/client/react-query/revokeIdentityProviderConnection.js";

import {
  inlineError,
  invalidateIdentityProviderQueries,
  SESSION_SECURITY,
} from "../../identityProviderQueries";

function orgHostOf(orgUrl: string): string {
  try {
    return new URL(orgUrl).host.toLowerCase();
  } catch {
    return orgUrl.replace(/^https?:\/\//i, "").toLowerCase();
  }
}

export function RevokeConnectionButton({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const revoke = useRevokeIdentityProviderConnectionMutation({
    onSuccess: () => {
      toast.success("Okta connection revoked");
      setOpen(false);
      void invalidateIdentityProviderQueries(queryClient);
    },
    onError: inlineError,
  });
  const orgHost = orgHostOf(connection.orgUrl);
  const confirmed = typed.trim().toLowerCase() === orgHost;
  const close = () => {
    if (revoke.isPending) return;
    setOpen(false);
    setTyped("");
    revoke.reset();
  };

  return (
    <>
      <Button variant="destructive-secondary" onClick={() => setOpen(true)}>
        Revoke
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (next) setOpen(true);
          else close();
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke the Okta connection</Dialog.Title>
            <Dialog.Description>
              Speakeasy stops connecting to Okta and removes this
              connection&apos;s public keys. Application sync stops, and
              Speakeasy no longer tracks Cross App Access setup. You can
              reconnect afterwards with a new client ID.
            </Dialog.Description>
          </Dialog.Header>
          <div className="space-y-3 py-2">
            <Field>
              <FieldLabel htmlFor="okta-revoke-confirm">
                Type <span className="font-mono">{orgHost}</span> to confirm
              </FieldLabel>
              <Input
                id="okta-revoke-confirm"
                value={typed}
                onChange={setTyped}
                className="font-mono"
                autoComplete="off"
                spellCheck={false}
              />
            </Field>
            <ApiErrorAlert error={revoke.error} />
          </div>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={close}
              disabled={revoke.isPending}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={!confirmed || revoke.isPending}
              onClick={() =>
                revoke.mutate({
                  security: SESSION_SECURITY,
                  request: {
                    revokeIdentityProviderConnectionRequestBody: {
                      id: connection.id,
                    },
                  },
                })
              }
            >
              {revoke.isPending ? "Revoking..." : "Revoke connection"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
