import { ArrowLeft } from "lucide-react";
import { parseAsStringLiteral, useQueryState } from "nuqs";
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

import { ApplicationsTab } from "./tabs/applications/ApplicationsTab";
import { CONNECTION_STATUS, isConnected } from "./connectionView";
import { CrossAppAccessTab } from "./tabs/cross-app-access/CrossAppAccessTab";
import { useOktaConnection } from "./identityProviderQueries";
import { OktaConnectionTab } from "./tabs/setup/OktaConnectionTab";
import {
  enterpriseManagedAuthHref,
  OKTA_VIEWS,
  oktaViewHref,
  type OktaView,
} from "./tabs";

export function OktaProviderCard(): JSX.Element {
  const { query, connection } = useOktaConnection();
  const connected = isConnected(connection);
  return (
    <article
      aria-label="Okta identity provider"
      className="flex min-w-0 flex-col gap-4 border p-6"
    >
      <div className="flex flex-wrap items-center gap-3">
        <Icon name="plug" className="h-5 w-5" />
        <Heading variant="h3">Okta</Heading>
        {!query.isPending && query.data !== undefined && (
          <Badge
            variant={
              connected
                ? CONNECTION_STATUS[connection.status].variant
                : "neutral"
            }
          >
            {connected
              ? CONNECTION_STATUS[connection.status].label
              : "Not connected"}
          </Badge>
        )}
      </div>
      <Text muted small>
        Sync applications from Okta and set up Cross App Access for your AI
        agents.
      </Text>
      {query.isPending ? (
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
              <Button
                asChild
                variant={connected ? "secondary" : "primary"}
                size="sm"
                className="self-start"
              >
                <Link to={oktaViewHref("setup")}>
                  {connected ? "Manage Okta" : "Connect Okta"}
                </Link>
              </Button>
            </>
          )}
        </>
      )}
    </article>
  );
}

const VIEW_LABELS: Record<OktaView, string> = {
  setup: "Setup",
  applications: "Applications",
  "cross-app-access": "Cross App Access",
};

export function OktaWorkspace(): JSX.Element {
  const [view] = useQueryState(
    "view",
    parseAsStringLiteral(OKTA_VIEWS).withDefault("setup"),
  );
  const navigate = useNavigate();
  const { query, connection } = useOktaConnection();
  const connected = isConnected(connection);
  return (
    <section
      className="flex min-w-0 flex-col gap-6"
      aria-label="Okta Enterprise Managed Auth"
    >
      <div className="flex flex-col gap-3">
        <Link
          to={enterpriseManagedAuthHref()}
          className="inline-flex w-fit items-center gap-2 text-sm underline underline-offset-4"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          All identity providers
        </Link>
        <Heading variant="h2">Okta</Heading>
        {connected && (
          <Text muted>
            Manage how your AI agents access company applications using Okta.
          </Text>
        )}
      </div>
      {query.isPending ? (
        <div className="flex flex-col gap-8">
          <SkeletonParagraph />
          <SkeletonParagraph />
        </div>
      ) : (
        <div className="flex flex-col gap-6">
          <ApiErrorAlert error={query.error} />
          {query.data !== undefined &&
            (connected ? (
              <Tabs
                value={view}
                onValueChange={(value) => {
                  const next = OKTA_VIEWS.find((item) => item === value);
                  if (next) void navigate(oktaViewHref(next));
                }}
                className="min-w-0 gap-6"
              >
                <div className="min-w-0 overflow-x-auto">
                  <TabsList aria-label="Okta setup and access" className="h-10">
                    {OKTA_VIEWS.map((item) => (
                      <TabsTrigger key={item} value={item}>
                        {VIEW_LABELS[item]}
                      </TabsTrigger>
                    ))}
                  </TabsList>
                </div>
                <TabsContent value={view}>
                  <ViewContent view={view} connection={connection} />
                </TabsContent>
              </Tabs>
            ) : (
              <OktaConnectionTab connection={connection} />
            ))}
        </div>
      )}
    </section>
  );
}

function ViewContent({
  view,
  connection,
}: {
  view: OktaView;
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  switch (view) {
    case "setup":
      return <OktaConnectionTab connection={connection} />;
    case "applications":
      return <ApplicationsTab connection={connection} />;
    case "cross-app-access":
      return <CrossAppAccessTab connection={connection} />;
  }
}
