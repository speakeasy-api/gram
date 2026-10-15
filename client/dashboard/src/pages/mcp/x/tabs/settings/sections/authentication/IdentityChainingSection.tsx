import { Link } from "react-router";

import { SettingsSection } from "@/components/detail/settings-section";
import { Text } from "@/components/ui/Text";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useAllRemoteSessionClients } from "@/lib/remote-identity";
import { identityProvidersHref } from "@/pages/org/identity-provider/tabs";
import { useOrgRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";

import { MCP_SESSIONS_SECTION_ID } from "../RemoteMcpSessionsSection";
import { useMcpServerAuthTarget } from "./authTarget";
import { IdentityChainingField } from "./IdentityChainingField";
import { identityChainingProviders } from "./identityChainingGate";
import { useSpeakeasyIssuerStatus } from "./useSpeakeasyIssuerStatus";

export const IDENTITY_CHAINING_SECTION_ID = "identity-chaining";

// Speakeasy could vouch once an admin sets its issuer up.
const SPEAKEASY_SETUP = new Set(["setup_required", "verified_domain_required"]);

/**
 * Its own section because it acts on its own (each action applies at once)
 * rather than through the Identity panel's Save.
 */
export function IdentityChainingSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element | null {
  const target = useMcpServerAuthTarget(mcpServer);
  const remoteMcpServerId = target.remoteMcpServerId ?? "";
  const userSessionIssuerId = target.userSessionIssuerId ?? undefined;
  const { data: userSessionIssuer } = useUserSessionIssuer(
    { id: userSessionIssuerId },
    undefined,
    { enabled: !!userSessionIssuerId, throwOnError: false },
  );
  const { items: clients, isLoading: clientsLoading } =
    useAllRemoteSessionClients(
      { userSessionIssuerId },
      { enabled: !!userSessionIssuerId },
    );
  const sourceQuery = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "", throwOnError: false },
  );
  const externalFlag = useFeatureFlag(FEATURE_FLAGS.oktaConnections);
  const emaProvidersFlag = useFeatureFlag(FEATURE_FLAGS.emaProviders);
  const speakeasyIssuer = useSpeakeasyIssuerStatus();
  const orgRoutes = useOrgRoutes();
  const speakeasyRollout = emaProvidersFlag.status === "enabled";
  const providers = identityChainingProviders(userSessionIssuer, {
    externalRollout: externalFlag.status === "enabled",
    speakeasyRollout,
    speakeasyIssuer: speakeasyIssuer.data,
  });
  const resource = sourceQuery.data?.url;
  const speakeasySetup =
    userSessionIssuer?.projectId === "" &&
    speakeasyRollout &&
    !!speakeasyIssuer.data &&
    SPEAKEASY_SETUP.has(speakeasyIssuer.data.state);

  // A project sign-in can't chain; say so where the admin would look for it,
  // but only once the organization has an identity provider that could vouch.
  const projectSignIn =
    userSessionIssuer?.projectId !== "" &&
    ((speakeasyRollout &&
      !!speakeasyIssuer.data &&
      speakeasyIssuer.data.state !== "unavailable") ||
      (externalFlag.status === "enabled" &&
        !!userSessionIssuer?.trustedRemoteSessionIssuerId));

  if (!userSessionIssuer || !resource || clientsLoading) return null;
  if (!providers && !speakeasySetup && !projectSignIn) return null;

  return (
    <SettingsSection id={IDENTITY_CHAINING_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>
          Enterprise Managed Authorization
        </SettingsSection.Title>
        <SettingsSection.Description>
          Gets people into the service behind this server with a sign-in they
          already have, from your identity provider or Speakeasy, so they
          don&apos;t connect it separately. Only works with services that
          support Enterprise Managed Authorization for MCP.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        {providers ? (
          <IdentityChainingField
            userSessionIssuer={userSessionIssuer}
            providers={providers}
            speakeasyIssuerUrl={speakeasyIssuer.data?.issuer}
            linkedClients={clients}
            resource={resource}
            remoteMcpServerId={remoteMcpServerId}
          />
        ) : projectSignIn ? (
          <SettingsSection.Body>
            <Text muted small>
              Enterprise Managed Authorization needs an organization-level
              sign-in. This server signs people in with {userSessionIssuer.slug}
              , which belongs to this project.{" "}
              <Link
                className="underline underline-offset-4"
                to={`#${MCP_SESSIONS_SECTION_ID}`}
              >
                Change in Sessions
              </Link>
            </Text>
          </SettingsSection.Body>
        ) : (
          <SettingsSection.Body>
            <Text muted small>
              No identity provider can vouch for people on this server yet.{" "}
              <Link
                className="underline underline-offset-4"
                to={`${orgRoutes.identity.href()}${identityProvidersHref()}`}
              >
                Set up Speakeasy as an identity provider
              </Link>{" "}
              to turn this on.
            </Text>
          </SettingsSection.Body>
        )}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
