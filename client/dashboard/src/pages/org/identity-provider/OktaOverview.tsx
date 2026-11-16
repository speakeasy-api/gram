import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useFetchOrganizationRemoteSessionIssuerMetadataMutation } from "@gram/client/react-query/fetchOrganizationRemoteSessionIssuerMetadata.js";
import { useEffect, useId, useState, type ReactNode } from "react";
import { Link } from "react-router";

import { emaReturnHref, oktaIssuerUrl, type EmaReturn } from "./oktaEma";
import { oktaViewHref } from "./tabs";

/** What the deployment and the organization's Okta connection allow. */
export type ConnectionState =
  | { kind: "unavailable" }
  | { kind: "not-connected" }
  | { kind: "connected"; connection: OktaIdentityProviderConnection };

/**
 * One Okta integration that turns features on as Okta allows: Enterprise
 * Managed Auth needs only the Okta domain, application sync needs an Okta
 * connection, and Cross App Access builds on synced applications.
 */
export function OktaOverview({
  connection,
  emaReturn,
  mcpAuthenticationHref,
}: {
  connection: ConnectionState;
  emaReturn: EmaReturn | undefined;
  /** Where EMA is set up when nothing sent the admin here; none hides it. */
  mcpAuthenticationHref: string | undefined;
}): JSX.Element {
  const inputId = useId();
  const connectedDomain =
    connection.kind === "connected" ? connection.connection.orgUrl : "";
  const [domain, setDomain] = useState(connectedDomain);
  const lookup = useFetchOrganizationRemoteSessionIssuerMetadataMutation();
  const [checkedIssuer, setCheckedIssuer] = useState<string | undefined>();
  const [unusable, setUnusable] = useState(false);

  const check = (value: string) => {
    const issuer = oktaIssuerUrl(value);
    setCheckedIssuer(issuer);
    setUnusable(!issuer);
    if (!issuer) return;
    lookup.mutate({
      request: { fetchIssuerMetadataRequestBody: { issuer } },
    });
  };

  // A connected organization already named its domain; check it on arrival.
  useEffect(() => {
    if (connectedDomain) {
      setDomain(connectedDomain);
      check(connectedDomain);
    }
    // Only when the connection's domain changes, not on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connectedDomain]);

  const signingKeys = lookup.data?.jwksUri;
  const emaReady = !!checkedIssuer && lookup.isSuccess && !!signingKeys;

  return (
    <div className="flex flex-col gap-8">
      {emaReturn && (
        <Alert variant="info" dismissible={false}>
          <div className="flex flex-wrap items-center gap-3">
            <span className="flex-1">
              Setting up Enterprise Managed Auth for {emaReturn.providerName}.
              Check your Okta domain below, then use it for{" "}
              {emaReturn.providerName}.
            </span>
            <Button variant="secondary" size="sm" asChild>
              <Link to={emaReturn.path}>
                <Button.Text>Back to {emaReturn.providerName}</Button.Text>
              </Link>
            </Button>
          </div>
        </Alert>
      )}

      <form
        className="flex max-w-2xl flex-col gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          check(domain);
        }}
      >
        <Label htmlFor={inputId}>Okta domain</Label>
        <div className="flex gap-2">
          <Input
            id={inputId}
            value={domain}
            placeholder="acme.okta.com"
            onChange={(value: string) => {
              setDomain(value);
              // A check describes the domain it ran on; once the field
              // changes, the overview must not offer the old issuer.
              setCheckedIssuer(undefined);
              setUnusable(false);
              lookup.reset();
            }}
          />
          <Button
            type="submit"
            variant="secondary"
            disabled={lookup.isPending || domain.trim() === ""}
          >
            <Button.Text>
              {lookup.isPending
                ? "Checking"
                : checkedIssuer
                  ? "Check again"
                  : "Check"}
            </Button.Text>
          </Button>
        </div>
        <Text small muted>
          The address your people use to sign in to Okta.
        </Text>
        {unusable ? (
          <Text small className="text-destructive">
            Enter an Okta address such as acme.okta.com.
          </Text>
        ) : null}
      </form>

      <section
        aria-label="What Okta can do here"
        className="flex flex-col gap-3"
      >
        <div>
          <Heading variant="h4" className="mb-1">
            What Okta can do here
          </Heading>
          <Text small muted>
            Each feature turns on as Okta grants what it needs.
          </Text>
        </div>
        <div className="divide-y border">
          <Feature
            title="Enterprise Managed Auth"
            status={emaStatus({
              checked: !!checkedIssuer && !lookup.isIdle,
              pending: lookup.isPending,
              ready: emaReady,
              failed: lookup.isError || (lookup.isSuccess && !signingKeys),
            })}
            description="People arrive at your MCP servers already signed in through Okta. Speakeasy checks the identity Okta vouches for with Okta's published signing keys, so no Okta app or API access is needed."
            action={
              emaReady && checkedIssuer ? (
                emaReturn ? (
                  <Button size="sm" asChild>
                    <Link to={emaReturnHref(emaReturn, checkedIssuer)}>
                      <Button.Text>
                        Use for {emaReturn.providerName}
                      </Button.Text>
                    </Link>
                  </Button>
                ) : mcpAuthenticationHref ? (
                  <Button variant="secondary" size="sm" asChild>
                    <Link to={mcpAuthenticationHref}>
                      <Button.Text>
                        Choose an MCP Authentication Provider
                      </Button.Text>
                    </Link>
                  </Button>
                ) : null
              ) : null
            }
          />
          <Feature
            title="Sync applications"
            status={syncStatus(connection)}
            description="Suggest MCP servers for the applications your people already use in Okta. Speakeasy needs read access to your Okta applications, people and groups."
            action={
              connection.kind === "unavailable" ? null : (
                <Button
                  variant={
                    connection.kind === "connected" ? "secondary" : "primary"
                  }
                  size="sm"
                  asChild
                >
                  <Link to={oktaViewHref("setup", undefined, emaReturn)}>
                    <Button.Text>
                      {connection.kind === "connected"
                        ? "Manage connection"
                        : "Connect Okta"}
                    </Button.Text>
                  </Link>
                </Button>
              )
            }
          />
          <Feature
            title="Cross App Access"
            status={
              connection.kind === "connected" ? null : (
                <Badge variant="neutral">After application sync</Badge>
              )
            }
            description="Let agents reach your Okta-connected applications as the person using them."
            action={
              connection.kind === "connected" ? (
                <Button variant="secondary" size="sm" asChild>
                  <Link
                    to={oktaViewHref("cross-app-access", undefined, emaReturn)}
                  >
                    <Button.Text>Review access</Button.Text>
                  </Link>
                </Button>
              ) : null
            }
          />
        </div>
      </section>
    </div>
  );
}

function emaStatus({
  checked,
  pending,
  ready,
  failed,
}: {
  checked: boolean;
  pending: boolean;
  ready: boolean;
  failed: boolean;
}): ReactNode {
  if (!checked) return <Badge variant="neutral">Needs your Okta domain</Badge>;
  if (pending) return <Badge variant="neutral">Checking</Badge>;
  if (ready) return <Badge variant="success">Ready</Badge>;
  if (failed) {
    return <Badge variant="warning">Couldn&apos;t use this domain</Badge>;
  }
  return null;
}

function syncStatus(connection: ConnectionState): ReactNode {
  switch (connection.kind) {
    case "unavailable":
      return <Badge variant="neutral">Not available on this deployment</Badge>;
    case "not-connected":
      return <Badge variant="warning">Needs Okta access</Badge>;
    case "connected":
      return <Badge variant="success">Connected</Badge>;
  }
}

function Feature({
  title,
  status,
  description,
  action,
}: {
  title: string;
  status: ReactNode;
  description: string;
  action: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-wrap items-center gap-4 px-6 py-5">
      <div className="flex min-w-0 flex-1 basis-96 flex-col gap-1">
        <div className="flex flex-wrap items-center gap-3">
          <Text>{title}</Text>
          {status}
        </div>
        <Text small muted className="max-w-3xl">
          {description}
        </Text>
      </div>
      {action}
    </div>
  );
}
