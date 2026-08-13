import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import type { GcpKmsKey } from "@gram/client/models/components/gcpkmskey.js";
import { invalidateAllGetGcpKmsKey } from "@gram/client/react-query/getGcpKmsKey";
import { invalidateAllListExternalKeys } from "@gram/client/react-query/listExternalKeys";
import { useUpdateGcpKmsKeyMutation } from "@gram/client/react-query/updateGcpKmsKey";
import { useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { CredentialSelect } from "../CredentialSelect";
import { DeleteKeyDialog } from "../EncryptionKeys";

function SettingsSection({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-4 border-b pb-6 last:border-b-0 last:pb-0">
      <div className="flex flex-col gap-1">
        <Text className="font-medium">{title}</Text>
        {description && (
          <Text small muted>
            {description}
          </Text>
        )}
      </div>
      {children}
    </div>
  );
}

export function SettingsTab({
  externalKey,
}: {
  externalKey: GcpKmsKey;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [name, setName] = useState(externalKey.name);
  const [credentialId, setCredentialId] = useState(
    externalKey.externalCredentialId,
  );
  const [grantReference, setGrantReference] = useState(
    externalKey.customerGrantReference ?? "",
  );
  const [showDelete, setShowDelete] = useState(false);

  const update = useUpdateGcpKmsKeyMutation({
    onSuccess: async () => {
      // Refresh the list and this key's detail (Overview + page title) so the
      // saved changes appear without a reload.
      await Promise.all([
        invalidateAllListExternalKeys(queryClient),
        invalidateAllGetGcpKmsKey(queryClient),
      ]);
      toast.success("Encryption key updated");
    },
    onError: (error) => {
      console.error("Update external key failed", error);
    },
  });

  const saveError = update.error
    ? update.error instanceof Error && update.error.message
      ? update.error.message
      : "An unexpected error occurred. Please try again."
    : null;

  // Every field the update replaces is rendered above, so saving can never
  // silently drop a value the form did not show.
  const savable = name.trim().length > 0 && credentialId.length > 0;

  const handleSave = () => {
    if (!savable) return;
    update.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        updateGcpKmsKeyRequestBody: {
          id: externalKey.id,
          name: name.trim(),
          externalCredentialId: credentialId,
          // The update replaces rather than patches, so an emptied field has to
          // be sent as absent for the server to clear it.
          customerGrantReference: grantReference.trim() || undefined,
        },
      },
    });
  };

  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <SettingsSection
        title="Key"
        description="How this key is labeled in the dashboard."
      >
        <div className="flex flex-col gap-1.5">
          <Label>Name</Label>
          <Input value={name} onChange={setName} />
        </div>
      </SettingsSection>

      <SettingsSection
        title="Access"
        description="The external credential Gram authenticates with to reach this key. Repointing it changes how Gram gets to the key, not which key it is."
      >
        <CredentialSelect value={credentialId} onChange={setCredentialId} />
        <div className="flex flex-col gap-1.5">
          <Label>Granted identity (optional)</Label>
          <Input
            value={grantReference}
            onChange={setGrantReference}
            placeholder="name@project.iam.gserviceaccount.com"
          />
          <Text small muted>
            The Gram identity you granted on the key. Recorded for your own
            reference so a rotated identity can be spotted before signing starts
            failing.
          </Text>
        </div>
      </SettingsSection>

      <SettingsSection
        title="Identity"
        description="The key this record names is fixed. Signing with a different key means deleting this one and creating another, because anything Gram has already published pins its identity to this record."
      >
        <div className="flex flex-col gap-1.5">
          <Label>Resource name</Label>
          <Input value={externalKey.resourceName} disabled readOnly />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label>Algorithm</Label>
          <Input value={externalKey.algorithm} disabled readOnly />
        </div>
      </SettingsSection>

      {saveError && (
        <Alert variant="error" dismissible={false}>
          {saveError}
        </Alert>
      )}

      <div>
        <Button onClick={handleSave} disabled={!savable || update.isPending}>
          <Button.Text>
            {update.isPending ? "Saving…" : "Save changes"}
          </Button.Text>
        </Button>
      </div>

      <div className="border-destructive/30 flex flex-col gap-2 border p-4">
        <Text className="font-medium">Danger Zone</Text>
        <Text small muted>
          Deleting this key is permanent. The key itself stays in your KMS.
        </Text>
        <div>
          <Button
            variant="destructive-primary"
            onClick={() => setShowDelete(true)}
          >
            <Button.Text>Delete key</Button.Text>
          </Button>
        </div>
      </div>

      {showDelete && (
        <DeleteKeyDialog
          keyId={externalKey.id}
          keyName={externalKey.name}
          onClose={() => setShowDelete(false)}
          onDeleted={() => orgRoutes.encryptionKeys.goTo()}
        />
      )}
    </div>
  );
}
