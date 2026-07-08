import type { Toolset } from "@/lib/toolTypes";
import { useRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { Button } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { PageSection } from "./MCPDetails";
import { AttachRemoteIdentityProviderSheet } from "./x/tabs/settings/sections/authentication/AttachRemoteIdentityProviderSheet";
import { AuthenticationSectionBody } from "./x/tabs/settings/sections/authentication/AuthenticationSection";
import { useToolsetAuthTarget } from "./x/tabs/settings/sections/authentication/authTarget";
import { UserSessionsList } from "./x/tabs/settings/sections/authentication/McpServerSessionsPanel";
import { externalOauthIssuerUrl } from "./toolsetAuthSurface";

// The user-sessions authentication surface for toolset-backed MCP servers:
// the same identity-provider configuration the remote server settings tab
// renders, wrapped in this page's section chrome. Covers both the attach
// state (no user_session_issuer yet) and the manage state.
export function ToolsetAuthenticationSection({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element {
  const target = useToolsetAuthTarget(toolset);

  return (
    <>
      <PageSection
        heading="Authentication"
        description="Configure the upstream identity provider and user session settings for clients connecting to this server."
      >
        <AuthenticationSectionBody target={target} />
      </PageSection>
      {target.userSessionIssuerId && (
        <PageSection
          heading="User sessions"
          description="Active sessions clients hold into this server, established via OAuth."
        >
          <UserSessionsList issuerId={target.userSessionIssuerId} />
        </PageSection>
      )}
    </>
  );
}

// Convert path for external-OAuth toolsets: opens the attach sheet seeded
// with the external server's issuer URL. On success the sheet links the
// created user_session_issuer to the toolset, which flips the page to the
// manage surface; the external OAuth config stays in place but goes inert.
export function ConvertToUserSessionsButton({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element {
  const [sheetOpen, setSheetOpen] = useState(false);
  const target = useToolsetAuthTarget(toolset);
  // No user_session_issuer exists yet on this surface, so every issuer this
  // project can see is selectable.
  const { data: issuersResult } = useRemoteSessionIssuers();
  const selectableIssuers = issuersResult?.result.items ?? [];

  return (
    <>
      <Button variant="tertiary" onClick={() => setSheetOpen(true)}>
        <Button.Text>Convert to User Sessions</Button.Text>
      </Button>
      <AttachRemoteIdentityProviderSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        target={target}
        userSessionIssuer={null}
        selectableIssuers={selectableIssuers}
        initialIssuerUrl={externalOauthIssuerUrl(toolset)}
      />
    </>
  );
}
