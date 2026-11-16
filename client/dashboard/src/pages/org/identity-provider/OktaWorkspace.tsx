import { enumPresentation } from "@/lib/enum";
import { ArrowLeft } from "lucide-react";
import { parseAsString, parseAsStringLiteral, useQueryState } from "nuqs";
import { Link, useNavigate } from "react-router";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Icon } from "@/components/ui/Icon";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";

import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useOrgRoutes } from "@/routes";
import { CONNECTION_STATUS, isConnected } from "./connectionView";
import { OktaOverview } from "./OktaOverview";
import { connectionsUnavailable, parseEmaReturn } from "./oktaEma";
import { CrossAppAccessTab } from "./tabs/cross-app-access/CrossAppAccessTab";
import { useOktaConnection } from "./identityProviderQueries";
import { ChecklistOverridesProvider } from "./tabs/setup/ConnectionChecklist";
import { OktaConnectionTab } from "./tabs/setup/OktaConnectionTab";
import {
  identityProvidersHref,
  OKTA_VIEWS,
  oktaViewHref,
  type OktaView,
} from "./tabs";

export function OktaProviderCard(): JSX.Element {
  const { query, connection } = useOktaConnection();
  const unknownProvider = connection != null && connection.provider !== "okta";
  const connected = !unknownProvider && isConnected(connection);
  const unknownStatus =
    connection != null && !connected && connection.status !== "revoked";
  const status = enumPresentation(CONNECTION_STATUS, connection?.status ?? "", {
    variant: "neutral",
    label: "Unknown",
  });
  const statusLabel =
    connection == null || connection.status === "revoked"
      ? "Not connected"
      : status.label;
  return (
    <article
      aria-label="Okta identity provider"
      className="flex min-w-0 flex-col gap-4 border p-6"
    >
      <div className="flex flex-wrap items-center gap-3">
        <Icon name="plug" className="h-5 w-5" />
        <Heading variant="h3">Okta</Heading>
        {!unknownProvider && !query.isPending && query.data !== undefined && (
          <Badge variant={status.variant}>{statusLabel}</Badge>
        )}
      </div>
      <Text muted small>
        Let people arrive already signed in through Okta, and sync the
        applications they use into MCP servers.
      </Text>
      {unknownProvider ? (
        <UnknownConnectionProvider />
      ) : query.isPending ? (
        <SkeletonParagraph />
      ) : (
        <>
          <ApiErrorAlert error={query.error} />
          {query.data === undefined ? (
            <Button
              variant="secondary"
              size="sm"
              className="self-start"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              Retry loading Okta
            </Button>
          ) : (
            <>
              {connected && (
                <Text small className="break-all">
                  {connection.orgUrl}
                </Text>
              )}
              {unknownStatus && <UnknownConnectionStatus />}
              {!unknownStatus && (
                <Button
                  asChild
                  variant={connected ? "secondary" : "primary"}
                  size="sm"
                  className="self-start"
                >
                  <Link to={oktaViewHref("overview")}>
                    {connected ? "Manage Okta" : "Set up Okta"}
                  </Link>
                </Button>
              )}
            </>
          )}
        </>
      )}
    </article>
  );
}

const VIEW_LABELS: Record<OktaView, string> = {
  overview: "Overview",
  setup: "Connection",
  "cross-app-access": "Cross App Access",
};

export function OktaWorkspace(): JSX.Element {
  const [view] = useQueryState(
    "view",
    parseAsStringLiteral(OKTA_VIEWS).withDefault("overview"),
  );
  const [emaFor] = useQueryState("emaFor", parseAsString);
  const [emaPath] = useQueryState("emaReturn", parseAsString);
  const emaReturn = parseEmaReturn(emaFor, emaPath);
  const orgRoutes = useOrgRoutes();
  const mcpAuthenticationHref =
    useFeatureFlag(FEATURE_FLAGS.userSessionIssuerDashboard).status ===
    "enabled"
      ? orgRoutes.userSessionIssuers.href()
      : undefined;
  const navigate = useNavigate();
  const { query, connection } = useOktaConnection();
  const unavailable = connectionsUnavailable(query.error);
  const unknownProvider = connection != null && connection.provider !== "okta";
  const connected = !unknownProvider && isConnected(connection);
  const unknownStatus =
    connection != null && !connected && connection.status !== "revoked";
  // Cross App Access reviews synced applications, so it waits for a connection.
  const views = OKTA_VIEWS.filter(
    (item) => item !== "cross-app-access" || connected,
  );
  const shownView = views.includes(view) ? view : "overview";
  return (
    <section className="flex min-w-0 flex-col gap-6" aria-label="Okta">
      <div className="flex flex-col gap-3">
        <Link
          to={identityProvidersHref()}
          className="inline-flex w-fit items-center gap-2 text-sm underline underline-offset-4"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          All identity providers
        </Link>
        <Heading variant="h3">Okta</Heading>
      </div>
      {unknownProvider ? (
        <UnknownConnectionProvider />
      ) : query.isPending ? (
        <div className="flex flex-col gap-8">
          <SkeletonParagraph />
          <SkeletonParagraph />
        </div>
      ) : unavailable ? (
        // Nothing here can hold a connection; what needs only the domain
        // still works.
        <OktaOverview
          connection={{ kind: "unavailable" }}
          emaReturn={emaReturn}
          mcpAuthenticationHref={mcpAuthenticationHref}
        />
      ) : (
        <div className="flex flex-col gap-6">
          <ApiErrorAlert error={query.error} />
          {query.data !== undefined && unknownStatus && (
            <UnknownConnectionStatus />
          )}
          {query.data !== undefined && !unknownStatus && (
            <ChecklistOverridesProvider key={connection?.id ?? "none"}>
              <Tabs
                value={shownView}
                onValueChange={(value) => {
                  const next = views.find((item) => item === value);
                  if (next)
                    void navigate(oktaViewHref(next, undefined, emaReturn));
                }}
                className="min-w-0 gap-6"
              >
                <div className="min-w-0 overflow-x-auto">
                  <TabsList aria-label="Okta" className="h-10">
                    {views.map((item) => (
                      <TabsTrigger key={item} value={item}>
                        {VIEW_LABELS[item]}
                      </TabsTrigger>
                    ))}
                  </TabsList>
                </div>
                <TabsContent value={shownView}>
                  {shownView === "overview" ? (
                    <OktaOverview
                      connection={
                        connected
                          ? { kind: "connected", connection }
                          : { kind: "not-connected" }
                      }
                      emaReturn={emaReturn}
                      mcpAuthenticationHref={mcpAuthenticationHref}
                    />
                  ) : (
                    <ViewContent view={shownView} connection={connection} />
                  )}
                </TabsContent>
              </Tabs>
            </ChecklistOverridesProvider>
          )}
        </div>
      )}
    </section>
  );
}

function ViewContent({
  view,
  connection,
}: {
  view: Exclude<OktaView, "overview">;
  connection: OktaIdentityProviderConnection | undefined;
}): JSX.Element {
  switch (view) {
    case "setup":
      return <OktaConnectionTab connection={connection} />;
    case "cross-app-access":
      return connection ? (
        <CrossAppAccessTab connection={connection} />
      ) : (
        <OktaConnectionTab connection={connection} />
      );
  }
}

function UnknownConnectionStatus(): JSX.Element {
  return (
    <Text small muted>
      Connection status is unknown. Refresh the page before managing Okta.
    </Text>
  );
}

function UnknownConnectionProvider(): JSX.Element {
  return (
    <Text small muted>
      Refresh the page before managing this unsupported identity provider.
    </Text>
  );
}
