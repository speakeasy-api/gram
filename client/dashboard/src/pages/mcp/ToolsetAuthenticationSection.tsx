import { useExternalMcpOAuthConfigStatus } from "@/components/sources/sources-hooks";
import type { Toolset } from "@/lib/toolTypes";
import { useRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { Button } from "@/components/ui/Button";
import { useState } from "react";
import { PageSection } from "./MCPDetails";
import { ConnectOAuthModal } from "./oauth-wizard";
import { AttachRemoteIdentityProviderSheet } from "./x/tabs/settings/sections/authentication/AttachRemoteIdentityProviderSheet";
import { AuthenticationSectionBody } from "./x/tabs/settings/sections/authentication/AuthenticationSection";
import { useToolsetAuthTarget } from "./x/tabs/settings/sections/authentication/authTarget";
import {
  canConfigureExternalOAuth,
  externalOauthIssuerUrl,
} from "./toolsetAuthSurface";

/**
 * User-sessions auth surface for toolset-backed MCP servers: the remote
 * server tab's identity-provider config wrapped in this page's section
 * chrome. Covers both the attach state (no issuer yet) and the manage state.
 */
export function ToolsetAuthenticationSection({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element {
  const target = useToolsetAuthTarget(toolset);
  const [externalOAuthOpen, setExternalOAuthOpen] = useState(false);
  const externalMcpOAuthStatus = useExternalMcpOAuthConfigStatus(toolset.slug);
  const externalOAuthAvailable = canConfigureExternalOAuth(
    toolset,
    externalMcpOAuthStatus === "required-unconfigured",
  );

  return (
    <>
      <PageSection
        heading="Authentication"
        description="Configure the upstream identity provider and user session settings for clients connecting to this server."
      >
        <AuthenticationSectionBody
          target={target}
          additionalSetupAction={
            externalOAuthAvailable ? (
              <Button
                variant="secondary"
                onClick={() => setExternalOAuthOpen(true)}
              >
                <Button.Text>Configure External OAuth</Button.Text>
              </Button>
            ) : undefined
          }
        />
      </PageSection>
      <ConnectOAuthModal
        isOpen={externalOAuthOpen}
        onClose={() => setExternalOAuthOpen(false)}
        toolsetSlug={toolset.slug}
        toolset={toolset}
        initialPath="external"
      />
    </>
  );
}

/**
 * Convert path for external-OAuth toolsets: opens the attach sheet seeded
 * with the external server's issuer URL. On success the sheet links the new
 * issuer to the toolset, flipping the page to the manage surface; the
 * external OAuth config stays in place but goes inert.
 */
export function ConvertToUserSessionsButton({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element {
  const [sheetOpen, setSheetOpen] = useState(false);
  const target = useToolsetAuthTarget(toolset);
  // No issuer wired yet, so every issuer this project can see is selectable.
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
