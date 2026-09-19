import { ApiErrorAlert } from "@/components/api-error-alert";
import { RequireScope } from "@/components/require-scope";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useIdentityProviderConnection } from "@gram/client/react-query/identityProviderConnection.js";

import { ApplicationsTab } from "./ApplicationsTab";
import { CrossAppAccessTab } from "./CrossAppAccessTab";
import { IdentityProviderTab } from "./IdentityProviderTab";
import { SESSION_SECURITY, type ProviderTab } from "./identityProviderQueries";

export type ConnectionTabProps = {
  connection: OktaIdentityProviderConnection | undefined;
  /** The okta-connections rollout: gates creating a connection and confirming readiness. */
  rolloutEnabled: boolean;
};

type IdentityProviderAreaProps = {
  tab: ProviderTab;
  renderWorkspace?: (content: JSX.Element, connected: boolean) => JSX.Element;
};

/** Shared shell for the provider-backed identity tabs: admin gate, connection load, then the tab. */
export function IdentityProviderArea({
  tab,
  renderWorkspace,
}: IdentityProviderAreaProps): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <IdentityProviderAreaInner tab={tab} renderWorkspace={renderWorkspace} />
    </RequireScope>
  );
}

function IdentityProviderAreaInner({
  tab,
  renderWorkspace = (content) => content,
}: IdentityProviderAreaProps): JSX.Element {
  const flag = useFeatureFlag(FEATURE_FLAGS.oktaConnections);
  const connectionQuery = useIdentityProviderConnection(
    undefined,
    SESSION_SECURITY,
    { throwOnError: false, retry: false },
  );

  if (connectionQuery.isPending || flag.status === "loading") {
    return renderWorkspace(
      <div className="flex flex-col gap-8">
        <SkeletonParagraph />
        <SkeletonParagraph />
      </div>,
      false,
    );
  }
  if (connectionQuery.data === undefined) {
    return renderWorkspace(
      <ApiErrorAlert error={connectionQuery.error} />,
      false,
    );
  }

  const connection = connectionQuery.data.connection;
  const connected = connection != null && connection.status !== "revoked";
  return renderWorkspace(
    <div className="flex flex-col gap-6">
      {connectionQuery.error != null && (
        <ApiErrorAlert error={connectionQuery.error} />
      )}
      <TabContent
        tab={connected ? tab : "provider"}
        connection={connection}
        rolloutEnabled={flag.status === "enabled"}
      />
    </div>,
    connected,
  );
}

function TabContent({
  tab,
  connection,
  rolloutEnabled,
}: ConnectionTabProps & { tab: ProviderTab }): JSX.Element {
  switch (tab) {
    case "applications":
      return (
        <ApplicationsTab
          connection={connection}
          rolloutEnabled={rolloutEnabled}
        />
      );
    case "cross-app-access":
      return (
        <CrossAppAccessTab
          connection={connection}
          rolloutEnabled={rolloutEnabled}
        />
      );
    case "provider":
      return (
        <IdentityProviderTab
          connection={connection}
          rolloutEnabled={rolloutEnabled}
        />
      );
  }
}
