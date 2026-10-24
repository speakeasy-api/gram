import { useEffect, useRef, type ReactNode } from "react";
import { Link, useLocation } from "react-router";

import { SettingsSection } from "@/components/detail/settings-section";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
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
import {
  chainingVouchers,
  identityChainingProviders,
} from "./identityChainingGate";
import { useSpeakeasyIssuerStatus } from "./useSpeakeasyIssuerStatus";

export const IDENTITY_CHAINING_SECTION_ID = "identity-chaining";

// Speakeasy could vouch once an admin sets its issuer up.
const SPEAKEASY_SETUP = new Set(["setup_required", "verified_domain_required"]);

/** The section mounts after its own queries, so a cold-load deep link scrolls once it is there. */
function useScrollWhenReady(ready: boolean) {
  const location = useLocation();
  const done = useRef(false);
  useEffect(() => {
    if (!ready || done.current) return;
    if (location.hash !== `#${IDENTITY_CHAINING_SECTION_ID}`) return;
    done.current = true;
    const frame = window.requestAnimationFrame(() => {
      document
        .getElementById(IDENTITY_CHAINING_SECTION_ID)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [ready, location.hash]);
}

function Shell({ children }: { children: ReactNode }): JSX.Element {
  return (
    <SettingsSection id={IDENTITY_CHAINING_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>
          Enterprise Managed Authorization
        </SettingsSection.Title>
        <SettingsSection.Description>
          Signs people in to the service behind this server automatically, using
          their existing login. The service must support Enterprise Managed
          Authorization for MCP.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>{children}</SettingsSection.Panel>
    </SettingsSection>
  );
}

/** Holds the loaded section's height: the status line and four chain rows. */
function SectionSkeleton(): JSX.Element {
  return (
    <>
      <SettingsSection.Body>
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-4 w-full max-w-md" />
      </SettingsSection.Body>
      <div className="border-border divide-border divide-y border-t">
        {[0, 1, 2, 3].map((row) => (
          <div key={row} className="px-6 py-4">
            <Skeleton className="h-5 w-full max-w-lg" />
          </div>
        ))}
      </div>
      <span role="status" className="sr-only">
        Loading Enterprise Managed Authorization…
      </span>
    </>
  );
}

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
  const issuerQuery = useUserSessionIssuer(
    { id: userSessionIssuerId },
    undefined,
    { enabled: !!userSessionIssuerId, throwOnError: false },
  );
  const userSessionIssuer = issuerQuery.data;
  const clientsQuery = useAllRemoteSessionClients(
    { userSessionIssuerId },
    { enabled: !!userSessionIssuerId },
  );
  const clients = clientsQuery.items;
  const sourceQuery = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "", throwOnError: false },
  );
  const emaProvidersFlag = useFeatureFlag(FEATURE_FLAGS.emaProviders);
  const speakeasyIssuer = useSpeakeasyIssuerStatus();
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const orgAdmin = hasScope("org:admin", organization.id);
  // Speakeasy vouches once its issuer is offered; ema-providers only gates the provider list.
  const speakeasyOffered =
    !!speakeasyIssuer.data && speakeasyIssuer.data.state !== "unavailable";
  // The flag only stands in when the issuer state can't be read.
  const speakeasyRollout =
    speakeasyOffered ||
    (emaProvidersFlag.status === "enabled" && !speakeasyIssuer.data);
  // Without org:read the issuer query never runs; a binding may still be on.
  const speakeasyUnreadable =
    speakeasyIssuer.isPending && speakeasyIssuer.fetchStatus === "idle";
  const rollouts = {
    speakeasyRollout,
    speakeasyIssuer: speakeasyIssuer.data,
  };
  const known = identityChainingProviders(userSessionIssuer, rollouts);
  const providers =
    known ??
    (speakeasyUnreadable
      ? identityChainingProviders(userSessionIssuer, {
          ...rollouts,
          speakeasyUnknown: true,
        })
      : undefined);
  // Who can vouch is unknown, so show only a binding that's already on.
  const onlyWhenOn = !known && !!providers;
  const resource = sourceQuery.data?.url;
  const speakeasySetup =
    userSessionIssuer?.projectId === "" &&
    speakeasyRollout &&
    !!speakeasyIssuer.data &&
    SPEAKEASY_SETUP.has(speakeasyIssuer.data.state);

  // A project sign-in can't chain; say so where the admin would look for it,
  // but only once the organization has an identity provider that could vouch.
  const projectSignIn =
    !!userSessionIssuer &&
    userSessionIssuer.projectId !== "" &&
    !!chainingVouchers(userSessionIssuer, rollouts);

  const applies = !!userSessionIssuerId && remoteMcpServerId !== "";
  const failed =
    issuerQuery.isError ||
    sourceQuery.isError ||
    clientsQuery.isError ||
    // Only when Speakeasy's issuer decides whether anyone can vouch.
    (speakeasyRollout && speakeasyIssuer.isError && !known);
  const loading =
    !failed &&
    (issuerQuery.isLoading ||
      sourceQuery.isLoading ||
      clientsQuery.isLoading ||
      emaProvidersFlag.status === "loading" ||
      speakeasyIssuer.isLoading);
  const shown =
    applies &&
    (loading ||
      failed ||
      (!!userSessionIssuer &&
        !!resource &&
        (!!providers || speakeasySetup || projectSignIn)));
  useScrollWhenReady(shown && !loading);

  if (!shown) return null;
  if (onlyWhenOn && !loading && !failed && userSessionIssuer && resource) {
    return (
      <IdentityChainingField
        userSessionIssuer={userSessionIssuer}
        providers={providers}
        linkedClients={clients}
        resource={resource}
        remoteMcpServerId={remoteMcpServerId}
        onlyWhenOn
        frame={(content) => <Shell>{content}</Shell>}
      />
    );
  }
  if (loading) {
    return (
      <Shell>
        <SectionSkeleton />
      </Shell>
    );
  }
  if (failed || !userSessionIssuer || !resource) {
    return (
      <Shell>
        <SettingsSection.Body>
          <Alert variant="error">
            <Text small>
              Couldn’t load Enterprise Managed Authorization for this server.
            </Text>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                void issuerQuery.refetch();
                void sourceQuery.refetch();
                clientsQuery.refetch();
                if (speakeasyIssuer.isError) void speakeasyIssuer.refetch();
              }}
            >
              Retry
            </Button>
          </Alert>
        </SettingsSection.Body>
      </Shell>
    );
  }

  return (
    <Shell>
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
            sign-in. This server’s sign-in belongs to this project.{" "}
            <Link
              className="underline underline-offset-4"
              to={`#${MCP_SESSIONS_SECTION_ID}`}
            >
              Change in Sessions
            </Link>
          </Text>
        </SettingsSection.Body>
      ) : speakeasyIssuer.data?.state === "verified_domain_required" ? (
        <SettingsSection.Body>
          <Text muted small>
            Speakeasy vouches only for emails on your organization&apos;s
            verified domains.{" "}
            {orgAdmin ? (
              <>
                <Link
                  className="underline underline-offset-4"
                  to={`${orgRoutes.identity.href()}?tab=sso`}
                >
                  Verify a domain on the Single sign-on tab
                </Link>{" "}
                to turn this on.
              </>
            ) : (
              "An organization admin can verify a domain to turn this on."
            )}
          </Text>
        </SettingsSection.Body>
      ) : (
        <SettingsSection.Body>
          <Text muted small>
            No identity provider can vouch for people on this server yet.{" "}
            {orgAdmin ? (
              <>
                <Link
                  className="underline underline-offset-4"
                  to={`${orgRoutes.identity.href()}${identityProvidersHref()}`}
                >
                  Set up Speakeasy as an identity provider
                </Link>{" "}
                to turn this on.
              </>
            ) : (
              "An organization admin can set up Speakeasy as an identity provider to turn this on."
            )}
          </Text>
        </SettingsSection.Body>
      )}
    </Shell>
  );
}
