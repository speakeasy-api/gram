import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import type { GcpIamCredential } from "@gram/client/models/components/gcpiamcredential.js";
import { invalidateAllGetGcpIamCredential } from "@gram/client/react-query/getGcpIamCredential";
import { invalidateAllListExternalCredentials } from "@gram/client/react-query/listExternalCredentials";
import { useUpdateGcpIamCredentialMutation } from "@gram/client/react-query/updateGcpIamCredential";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { DeleteCredentialDialog } from "../ExternalServices";

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

function Field({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      <Input value={value} onChange={onChange} placeholder={placeholder} />
    </div>
  );
}

export function SettingsTab({
  credential,
}: {
  credential: GcpIamCredential;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [name, setName] = useState(credential.name);
  const [impersonateServiceAccount, setImpersonateServiceAccount] = useState(
    credential.impersonateServiceAccount ?? "",
  );
  const [showDelete, setShowDelete] = useState(false);

  const update = useUpdateGcpIamCredentialMutation({
    onSuccess: async () => {
      // Refresh the list and this credential's detail (Overview + page title) so
      // the saved changes appear without a reload.
      await Promise.all([
        invalidateAllListExternalCredentials(queryClient),
        invalidateAllGetGcpIamCredential(queryClient),
      ]);
      toast.success("External credential updated");
    },
    onError: (error) => {
      console.error("Update external credential failed", error);
    },
  });

  const saveError = update.error
    ? update.error instanceof Error && update.error.message
      ? update.error.message
      : "An unexpected error occurred. Please try again."
    : null;

  // Every field the update replaces is rendered above, so saving can never
  // silently drop a value the form did not show.
  const savable =
    name.trim().length > 0 && impersonateServiceAccount.trim().length > 0;

  const handleSave = () => {
    if (!savable) return;
    update.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        updateGcpIamCredentialRequestBody: {
          id: credential.id,
          name: name.trim(),
          impersonateServiceAccount: impersonateServiceAccount.trim(),
        },
      },
    });
  };

  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <SettingsSection
        title="Credential"
        description="How this credential is labelled in the dashboard."
      >
        <Field label="Name" value={name} onChange={setName} />
      </SettingsSection>

      <SettingsSection
        title="GCP identity"
        description="The service account in your project that Gram impersonates. Gram's own service account needs the roles/iam.serviceAccountTokenCreator role on it."
      >
        <Field
          label="Impersonate service account"
          value={impersonateServiceAccount}
          onChange={setImpersonateServiceAccount}
          placeholder="name@project.iam.gserviceaccount.com"
        />
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
          Deleting this credential is permanent.
        </Text>
        <div>
          <Button
            variant="destructive-primary"
            onClick={() => setShowDelete(true)}
          >
            <Button.Text>Delete credential</Button.Text>
          </Button>
        </div>
      </div>

      {showDelete && (
        <DeleteCredentialDialog
          credentialId={credential.id}
          credentialName={credential.name}
          onClose={() => setShowDelete(false)}
          onDeleted={() => orgRoutes.externalServices.goTo()}
        />
      )}
    </div>
  );
}
