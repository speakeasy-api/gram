import { RequireScope } from "@/components/require-scope";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { toastError } from "@/lib/toast-error";
import { useOrgRoutes } from "@/routes";
import type { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { invalidateAllOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { useUpdateOrganizationRemoteSessionClientMutation } from "@gram/client/react-query/updateOrganizationRemoteSessionClient.js";
import { Button, Input } from "@/components/ui/moonshine";
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

  const update = useUpdateOrganizationRemoteSessionClientMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionClient(queryClient, {
        refetchType: "all",
      });
      setClientSecret("");
      toast.success("Client updated");
    },
    onError: (error) => {
      toastError(error, "Failed to update client");
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
        <div className="flex flex-col gap-1.5">
          <Label>Scopes (comma-separated)</Label>
          <Input value={scope} onChange={(e) => setScope(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label>Audience</Label>
          <Input
            value={audience}
            onChange={(e) => setAudience(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label>Rotate client secret</Label>
          <Input
            type="password"
            value={clientSecret}
            onChange={(e) => setClientSecret(e.target.value)}
            placeholder="Enter a new secret to rotate; leave blank to keep current"
          />
          <Type small muted>
            The secret is encrypted at rest and never displayed. Leave blank to
            keep the existing secret.
          </Type>
        </div>
        <div>
          <RequireScope scope="org:admin" level="component">
            <Button onClick={handleSave} disabled={update.isPending}>
              <Button.Text>
                {update.isPending ? "Saving…" : "Save changes"}
              </Button.Text>
            </Button>
          </RequireScope>
        </div>
      </div>

      <div className="border-destructive/30 flex flex-col gap-2 rounded-md border p-4">
        <Type className="font-medium">Danger Zone</Type>
        <Type small muted>
          Deleting this client is permanent and revokes all of its sessions.
        </Type>
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
