import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { UpdateUserSessionIssuerFormClientIdMetadataAdmissionMode as WritableMode } from "@gram/client/models/components/updateusersessionissuerform.js";
import { useEffect, useState } from "react";
import { CimdAdmissionModeField } from "./CimdAdmissionModeField";
import { CimdCustomClientsField } from "./CimdCustomClientsField";
import { UserSessionDurationField } from "./UserSessionDurationField";

export function UserIdentitySessionControls({
  userSessionIssuer,
}: {
  userSessionIssuer: UserSessionIssuer;
}): JSX.Element {
  const [cimdDraftMode, setCimdDraftMode] = useState<WritableMode | null>(null);

  useEffect(() => {
    setCimdDraftMode(null);
  }, [userSessionIssuer.clientIdMetadataAdmissionMode, userSessionIssuer.id]);

  // The custom-URL list follows the unsaved selection. An operator moving to
  // Verified clients can stage its URLs before the policy takes effect.
  const shownMode =
    cimdDraftMode ?? userSessionIssuer.clientIdMetadataAdmissionMode;
  // An organization-owned issuer is managed at the org level; a project
  // surface may read it but must not rewrite it.
  const organizationOwned = userSessionIssuer.projectId === "";
  const admitsCustomUrls =
    shownMode === WritableMode.Presets || shownMode === "reporting";

  return (
    <>
      <UserSessionDurationField
        userSessionIssuer={userSessionIssuer}
        readOnly={organizationOwned}
      />
      <CimdAdmissionModeField
        readOnly={organizationOwned}
        userSessionIssuer={userSessionIssuer}
        onDraftModeChange={setCimdDraftMode}
      >
        {admitsCustomUrls && !organizationOwned ? (
          <CimdCustomClientsField userSessionIssuer={userSessionIssuer} />
        ) : null}
      </CimdAdmissionModeField>
    </>
  );
}
