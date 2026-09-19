import { ArrowLeft } from "lucide-react";
import { Link, useNavigate, useSearchParams } from "react-router";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Icon } from "@/components/ui/Icon";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useIdentityProviderConnection } from "@gram/client/react-query/identityProviderConnection.js";

import {
  connectionStatusLabel,
  connectionStatusVariant,
} from "./connectionView";
import { IdentityProviderArea } from "./IdentityProviderArea";
import {
  enterpriseManagedAuthHref,
  SESSION_SECURITY,
  type ProviderTab,
} from "./identityProviderQueries";

/** Each integration owns its data and statuses; adding a provider does not replace Okta. */
export function OktaProviderCard(): JSX.Element {
  const flag = useFeatureFlag(FEATURE_FLAGS.oktaConnections);
  const query = useIdentityProviderConnection(undefined, SESSION_SECURITY, {
    throwOnError: false,
    retry: false,
  });
  const connection = query.data?.connection;
  const connected = connection != null && connection.status !== "revoked";
  const loading = query.isPending || flag.status === "loading";
  return (
    <article
      aria-label="Okta identity provider"
      className="flex min-w-0 flex-col gap-4 border p-6"
    >
      <div className="flex flex-wrap items-center gap-3">
        <Icon name="plug" className="h-5 w-5" />
        <Heading variant="h3">Okta</Heading>
        {!loading && query.data !== undefined && (
          <Badge
            variant={
              connected ? connectionStatusVariant(connection.status) : "neutral"
            }
          >
            {connected
              ? connectionStatusLabel(connection.status)
              : "Not connected"}
          </Badge>
        )}
      </div>
      <Text muted small>
        Sync applications from Okta and set up Cross App Access for your AI
        agents.
      </Text>
      {loading ? (
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
              {connected || flag.status === "enabled" ? (
                <Button
                  asChild
                  variant={connected ? "secondary" : "primary"}
                  size="sm"
                  className="self-start"
                >
                  <Link to={enterpriseManagedAuthHref("okta", "setup")}>
                    {connected ? "Manage Okta" : "Connect Okta"}
                  </Link>
                </Button>
              ) : (
                <Text muted small>
                  Okta setup is not enabled for this organization. Ask your
                  Speakeasy contact to enable it.
                </Text>
              )}
            </>
          )}
        </>
      )}
    </article>
  );
}

const OKTA_VIEWS: { value: string; label: string; tab: ProviderTab }[] = [
  { value: "setup", label: "Setup", tab: "provider" },
  { value: "applications", label: "Applications", tab: "applications" },
  {
    value: "cross-app-access",
    label: "Cross App Access",
    tab: "cross-app-access",
  },
];

/** Okta-specific navigation and setup; other providers supply their own workspace. */
export function OktaProviderWorkspace(): JSX.Element {
  const [search] = useSearchParams();
  const navigate = useNavigate();
  const view =
    OKTA_VIEWS.find((view) => view.value === search.get("view")) ??
    OKTA_VIEWS[0]!;
  return (
    <IdentityProviderArea
      tab={view.tab}
      renderWorkspace={(content, connected) => (
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
                Manage how your AI agents access company applications using
                Okta.
              </Text>
            )}
          </div>
          {connected ? (
            <Tabs
              value={view.value}
              onValueChange={(value) => {
                void navigate(enterpriseManagedAuthHref("okta", value));
              }}
              className="min-w-0 gap-6"
            >
              <div className="min-w-0 overflow-x-auto">
                <TabsList aria-label="Okta setup and access" className="h-10">
                  {OKTA_VIEWS.map((item) => (
                    <TabsTrigger key={item.value} value={item.value}>
                      {item.label}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </div>
              <TabsContent value={view.value}>{content}</TabsContent>
            </Tabs>
          ) : (
            content
          )}
        </section>
      )}
    />
  );
}
