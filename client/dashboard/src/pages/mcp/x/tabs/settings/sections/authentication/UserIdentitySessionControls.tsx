import { useReportUnsavedChanges } from "@/components/detail/settings-layout";
import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { UpdateUserSessionIssuerFormClientIdMetadataAdmissionMode as WritableMode } from "@gram/client/models/components/updateusersessionissuerform.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { useUpdateUserSessionIssuerMutation } from "@gram/client/react-query/updateUserSessionIssuer.js";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { CimdAdmissionModeField } from "./CimdAdmissionModeField";
import { CimdCustomClientsField } from "./CimdCustomClientsField";
import { DURATION_UNIT_HOURS, splitIntoUnit } from "./sessionDuration";
import { UserSessionDurationField } from "./UserSessionDurationField";

/**
 * Session length and client access, with one footer Save for both, like every
 * other settings panel. Render it last in its panel: the footer closes it.
 */
export function UserIdentitySessionControls({
  userSessionIssuer,
}: {
  userSessionIssuer: UserSessionIssuer;
}): JSX.Element {
  const queryClient = useQueryClient();
  const savedHours = userSessionIssuer.sessionDurationHours;
  const savedMode = userSessionIssuer.clientIdMetadataAdmissionMode;

  const [duration, setDuration] = useState(() => splitIntoUnit(savedHours));
  useEffect(() => {
    setDuration(splitIntoUnit(savedHours));
  }, [userSessionIssuer.id, savedHours]);

  // Resync on the saved mode, NOT the issuer object: other saves in this
  // panel invalidate the issuer query, and keying on the object would discard
  // an unsaved selection. The id keeps a draft from crossing issuers.
  const [draftMode, setDraftMode] = useState<WritableMode | null>(null);
  useEffect(() => {
    setDraftMode(null);
  }, [savedMode, userSessionIssuer.id]);

  // An organization-owned issuer is managed at the org level; a project
  // surface may read it but must not rewrite it.
  const organizationOwned = userSessionIssuer.projectId === "";

  const draftHours = duration.number * DURATION_UNIT_HOURS[duration.unit];
  const durationDirty = !organizationOwned && draftHours !== savedHours;
  const durationValid = draftHours > 0;
  const modeDirty =
    !organizationOwned && draftMode !== null && draftMode !== savedMode;
  const dirty = durationDirty || modeDirty;
  useReportUnsavedChanges(dirty);

  const update = useUpdateUserSessionIssuerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllUserSessionIssuer(queryClient, { refetchType: "all" }),
        // The sidebar readiness bar reads these (AGE-3279).
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Session settings updated");
    },
    // No error toast: the failure is shown in the footer, and reporting it
    // twice reads as two failures.
  });

  const save = () => {
    if (!dirty || (durationDirty && !durationValid) || update.isPending) {
      return;
    }
    update.mutate({
      request: {
        updateUserSessionIssuerForm: {
          id: userSessionIssuer.id,
          ...(durationDirty ? { sessionDurationHours: draftHours } : {}),
          ...(modeDirty && draftMode
            ? { clientIdMetadataAdmissionMode: draftMode }
            : {}),
        },
      },
    });
  };

  // The custom-URL list follows the unsaved selection. An operator moving to
  // Verified clients can stage its URLs before the policy takes effect.
  const shownMode = draftMode ?? savedMode;
  const admitsCustomUrls =
    shownMode === WritableMode.Presets || shownMode === "reporting";

  return (
    <>
      <UserSessionDurationField
        value={duration}
        onChange={setDuration}
        invalid={durationDirty && !durationValid}
        readOnly={organizationOwned}
      />
      <CimdAdmissionModeField
        readOnly={organizationOwned}
        userSessionIssuer={userSessionIssuer}
        draftMode={draftMode}
        onDraftModeChange={setDraftMode}
      >
        {admitsCustomUrls && !organizationOwned ? (
          <CimdCustomClientsField userSessionIssuer={userSessionIssuer} />
        ) : null}
      </CimdAdmissionModeField>
      <SettingsSection.Footer>
        <SettingsSection.FooterHint>
          {update.isError ? (
            <span role="alert" className="text-destructive">
              {update.error.message}
            </span>
          ) : organizationOwned ? (
            "Managed in your organization's settings."
          ) : null}
        </SettingsSection.FooterHint>
        {organizationOwned ? null : (
          <SettingsSection.FooterActions>
            {/* Render-function form: RequireScope's loading branch applies
                only pointer-events-none, so a keyboard user could still save
                while grants are in flight. */}
            <RequireScope
              scope="project:write"
              resourceId={userSessionIssuer.projectId}
              level="component"
            >
              {({ disabled }) => (
                <FooterSaveButton
                  pending={update.isPending}
                  disabled={
                    disabled ||
                    !dirty ||
                    (durationDirty && !durationValid) ||
                    update.isPending
                  }
                  onClick={save}
                />
              )}
            </RequireScope>
          </SettingsSection.FooterActions>
        )}
      </SettingsSection.Footer>
    </>
  );
}
