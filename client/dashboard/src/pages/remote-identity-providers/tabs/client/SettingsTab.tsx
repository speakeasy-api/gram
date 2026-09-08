import { RequireScope } from "@/components/require-scope";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import type { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { invalidateAllOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { useUpdateOrganizationRemoteSessionClientMutation } from "@gram/client/react-query/updateOrganizationRemoteSessionClient.js";
import { Button } from "@/components/ui/Button";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { remoteSessionClientDisplayName } from "../../clientDisplay";
import { TokenEndpointAuthMethodField } from "../../../mcp/x/tabs/settings/sections/authentication/IssuerFormFields";
import {
  narrowTokenEndpointAuthMethod,
  parseScopes,
} from "../../../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import { DeleteClientDialog } from "../../clientDialogs";
import { KeySetField } from "./KeySetField";

export function SettingsTab({
  client,
  issuerId,
}: {
  client: RemoteSessionClient;
  issuerId: string;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [authMethod, setAuthMethod] = useState<
    CreateRemoteSessionClientFormTokenEndpointAuthMethod | ""
  >(narrowTokenEndpointAuthMethod(client.tokenEndpointAuthMethod) ?? "");
  const [scope, setScope] = useState((client.scope ?? []).join(", "));
  const [audience, setAudience] = useState(client.audience ?? "");
  const [clientSecret, setClientSecret] = useState("");
  const [showDelete, setShowDelete] = useState(false);
  // The key set link saves on its own endpoint the moment it changes, while
  // these fields wait for Save. Selecting a set and immediately saving
  // private_key_jwt would otherwise race: the update can reach the server first
  // and be refused for having no set attached. AIM-156 makes that method
  // selectable; the sequencing is here so it is already right when it does.
  const [keySetPending, setKeySetPending] = useState(false);

  const update = useUpdateOrganizationRemoteSessionClientMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionClient(queryClient, {
        refetchType: "all",
      });
      setClientSecret("");
      toast.success("Client updated");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to update client",
      );
    },
  });

  const handleSave = () => {
    update.mutate({
      request: {
        updateRemoteSessionClientForm: {
          id: client.id,
          tokenEndpointAuthMethod: authMethod || undefined,
          scope: parseScopes(scope),
          audience: audience.trim() || undefined,
          clientSecret: clientSecret.trim() || undefined,
        },
      },
    });
  };

  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <div className="flex flex-col gap-4">
        <TokenEndpointAuthMethodField
          value={authMethod}
          onChange={setAuthMethod}
        />
        {/* org:admin like the Save button below: attach and detach are
            org:admin on the server, so a reader must not get a live control
            whose every change 403s. */}
        <RequireScope scope="org:admin" level="component">
          <KeySetField
            client={client}
            issuerId={issuerId}
            onPendingChange={setKeySetPending}
          />
        </RequireScope>
        <div className="flex flex-col gap-1.5">
          <Label>Scopes (comma-separated)</Label>
          <Input value={scope} onChange={setScope} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label>Audience</Label>
          <Input value={audience} onChange={setAudience} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label>Rotate client secret</Label>
          <Input
            type="password"
            value={clientSecret}
            onChange={setClientSecret}
            placeholder="Enter a new secret to rotate; leave blank to keep current"
          />
          <Text small muted>
            The secret is encrypted at rest and never displayed. Leave blank to
            keep the existing secret.
          </Text>
        </div>
        <div>
          <RequireScope scope="org:admin" level="component">
            <Button
              onClick={handleSave}
              disabled={update.isPending || keySetPending}
            >
              <Button.Text>
                {update.isPending ? "Saving…" : "Save changes"}
              </Button.Text>
            </Button>
          </RequireScope>
        </div>
      </div>

      <div className="border-destructive/30 flex flex-col gap-2 border p-4">
        <Text className="font-medium">Danger Zone</Text>
        <Text small muted>
          Deleting this client is permanent and revokes all of its sessions.
        </Text>
        <div>
          <RequireScope scope="org:admin" level="component">
            <Button
              variant="destructive-primary"
              onClick={() => setShowDelete(true)}
            >
              <Button.Text>Delete client</Button.Text>
            </Button>
          </RequireScope>
        </div>
      </div>

      {showDelete && (
        <DeleteClientDialog
          clientId={client.id}
          clientLabel={remoteSessionClientDisplayName(client)}
          onClose={() => setShowDelete(false)}
          onDeleted={() =>
            orgRoutes.remoteIdentityProviders.issuerDetail.goTo(issuerId)
          }
        />
      )}
    </div>
  );
}
