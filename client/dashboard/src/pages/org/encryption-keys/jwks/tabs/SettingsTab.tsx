import {
  DangerSettingsSection,
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { useOrgRoutes } from "@/routes";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildGetJsonWebKeySetQuery } from "@gram/client/react-query/getJsonWebKeySet";
import { useUpdateJsonWebKeySetMutation } from "@gram/client/react-query/updateJsonWebKeySet";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { DeleteSetDialog } from "../dialogs";
import { invalidateSet } from "../invalidate";

export function SettingsTab({ set }: { set: JSONWebKeySet }): JSX.Element {
  const client = useGramContext();
  const queryClient = useQueryClient();
  const orgRoutes = useOrgRoutes();
  const [name, setName] = useState(set.name);
  const [showDelete, setShowDelete] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const update = useUpdateJsonWebKeySetMutation();

  const trimmed = name.trim();
  const savable = trimmed.length > 0 && trimmed !== set.name;

  const handleSave = async () => {
    if (!savable || saving) return;
    setSaving(true);
    setSaveError(null);
    try {
      // The update replaces both fields, and the backing key is changed by
      // publishing from the Keys tab rather than here. Read the set again
      // rather than trusting the props so a rename never reverts a re-point
      // made elsewhere since this page loaded.
      const fresh = await queryClient.fetchQuery(
        buildGetJsonWebKeySetQuery(client, { id: set.id }),
      );
      await update.mutateAsync({
        security: { sessionHeaderGramSession: "" },
        request: {
          updateJSONWebKeySetRequestBody: {
            id: set.id,
            name: trimmed,
            externalKeyId: fresh.externalKeyId,
          },
        },
      });
      // The list, this set's detail (Overview + page title) and its history
      // all show the name.
      await invalidateSet(queryClient);
      toast.success("Signing key set updated");
    } catch (caught: unknown) {
      console.error("Update signing key set failed", caught);
      setSaveError(
        caught instanceof Error && caught.message
          ? caught.message
          : "An unexpected error occurred. Please try again.",
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex max-w-2xl flex-col gap-8">
      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>Key set</SettingsSection.Title>
          <SettingsSection.Description>
            How this set is labeled in the dashboard. Names may repeat within an
            organization. The KMS key it publishes from is chosen when
            publishing a new key.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <div className="flex flex-col gap-1.5">
              <Label>Name</Label>
              <Input value={name} onChange={setName} />
            </div>
            {saveError && (
              <Alert variant="error" dismissible={false}>
                {saveError}
              </Alert>
            )}
          </SettingsSection.Body>
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              {savable ? "Unsaved changes" : "No changes"}
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              <FooterSaveButton
                pending={saving}
                disabled={!savable || saving}
                onClick={() => void handleSave()}
              />
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        </SettingsSection.Panel>
      </SettingsSection>

      <DangerSettingsSection>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Danger Zone</DangerSettingsSection.Title>
          <DangerSettingsSection.Description>
            Deleting this set withdraws every key in it. Anything that verifies
            tokens against it stops accepting them.
          </DangerSettingsSection.Description>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body>
            <div>
              <Button
                variant="destructive-primary"
                onClick={() => setShowDelete(true)}
              >
                <Button.Text>Delete key set</Button.Text>
              </Button>
            </div>
          </DangerSettingsSection.Body>
        </DangerSettingsSection.Panel>
      </DangerSettingsSection>

      {showDelete && (
        <DeleteSetDialog
          set={set}
          onClose={() => setShowDelete(false)}
          onDeleted={() => orgRoutes.encryptionKeys.goTo()}
        />
      )}
    </div>
  );
}
