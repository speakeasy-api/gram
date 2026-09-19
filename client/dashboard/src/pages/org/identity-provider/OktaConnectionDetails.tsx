import type { ReactNode } from "react";

import { Badge } from "@/components/ui/Badge";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";

import { isConnectionChecked, listingModeLabel } from "./connectionView";

function FactList({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <dl
      className={cn(
        "grid grid-cols-1 gap-x-8 gap-y-4 sm:grid-cols-[max-content_minmax(0,1fr)]",
        className,
      )}
    >
      {children}
    </dl>
  );
}

function Fact({
  label,
  children,
  mono,
}: {
  label: string;
  children: ReactNode;
  mono?: boolean;
}): JSX.Element {
  return (
    <>
      <dt className="text-eyebrow self-center">{label}</dt>
      <dd
        className={cn(
          "flex min-w-0 flex-wrap items-center gap-2 text-sm",
          mono && "font-mono text-xs",
        )}
      >
        {children}
      </dd>
    </>
  );
}

function CopyableValue({ value }: { value: string }): JSX.Element {
  return (
    <span className="inline-flex min-w-0 max-w-full items-center gap-2">
      <span className="min-w-0 [overflow-wrap:anywhere]">{value}</span>
      <span className="shrink-0">
        <CopyButton text={value} size="xs" tooltip="Copy" />
      </span>
    </span>
  );
}

function ExternalValueLink({ href }: { href: string }): JSX.Element {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="hover:text-foreground min-w-0 underline underline-offset-2 [overflow-wrap:anywhere]"
    >
      {href}
    </a>
  );
}

function DpopBadge({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const observation = connection.checklist?.find((item) => item.key === "dpop");
  if (
    !isConnectionChecked(connection) ||
    connection.lastError ||
    (observation != null && observation.completed == null)
  ) {
    return (
      <Text muted small>
        Not checked yet
      </Text>
    );
  }
  return (observation?.completed ?? connection.dpopRequired) ? (
    <Badge variant="success" size="sm">
      Protected
    </Badge>
  ) : (
    <Badge variant="neutral" size="sm">
      Not protected
    </Badge>
  );
}

/** Degraded checks have no dedicated timestamp; updatedAt also includes unrelated edits. */
function LastVerified({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  if (connection.lastVerifiedAt) {
    return <HumanizeDateTime date={connection.lastVerifiedAt} />;
  }
  if (connection.status === "degraded") {
    return (
      <>
        <Text muted small>
          Never;
        </Text>
        <Text small>needs attention</Text>
      </>
    );
  }
  return (
    <Text muted small>
      Never
    </Text>
  );
}

export function ConnectionFacts({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  return (
    <FactList>
      <Fact label="Okta organization URL" mono>
        <ExternalValueLink href={connection.orgUrl} />
      </Fact>
      <Fact label="Public key URL (JWKS)" mono>
        <CopyableValue value={connection.jwksUrl} />
      </Fact>
      <Fact label="Client ID" mono>
        {connection.clientId ? (
          <CopyableValue value={connection.clientId} />
        ) : (
          <Text muted small>
            Not submitted yet
          </Text>
        )}
      </Fact>
      <Fact label="App setup method">
        {listingModeLabel(connection.listingMode)}
      </Fact>
      <Fact label="Token protection (DPoP)">
        <DpopBadge connection={connection} />
      </Fact>
      <Fact label="Last verified">
        <LastVerified connection={connection} />
      </Fact>
      <Fact label="Active signing key ID" mono>
        {connection.activeKey ? (
          <>
            <CopyableValue value={connection.activeKey.kid} />
            <Text muted small className="font-sans">
              since <HumanizeDateTime date={connection.activeKey.activatedAt} />
            </Text>
          </>
        ) : (
          <Text muted small>
            Withdrawn
          </Text>
        )}
      </Fact>
    </FactList>
  );
}

function ScopeGroup({
  label,
  scopes,
  variant,
  emptyLabel,
  unknown = false,
}: {
  label: string;
  scopes: string[];
  variant: "neutral" | "success" | "destructive";
  emptyLabel: string;
  unknown?: boolean;
}): JSX.Element {
  const placeholder = unknown ? "Not checked yet" : emptyLabel;
  const body =
    unknown || scopes.length === 0 ? (
      <Text muted small>
        {placeholder}
      </Text>
    ) : (
      <div className="flex flex-wrap gap-1.5">
        {scopes.map((scope) => (
          <Badge key={scope} variant={variant} size="sm">
            {scope}
          </Badge>
        ))}
      </div>
    );
  return (
    <div className="flex flex-col gap-2">
      <span className="text-eyebrow">
        {label}
        {unknown ? "" : ` · ${scopes.length}`}
      </span>
      {body}
    </div>
  );
}

export function ConnectionScopes({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const unknown = !isConnectionChecked(connection);
  return (
    <div className="grid grid-cols-1 gap-6 md:grid-cols-3">
      <ScopeGroup
        label="Required permissions (scopes)"
        scopes={connection.requiredScopes}
        variant="neutral"
        emptyLabel="None"
      />
      <ScopeGroup
        label="Granted by Okta"
        scopes={connection.grantedScopes}
        variant="success"
        emptyLabel="Nothing granted"
        unknown={unknown}
      />
      <ScopeGroup
        label="Still missing"
        scopes={connection.missingScopes}
        variant="destructive"
        emptyLabel="Nothing missing"
        unknown={unknown}
      />
    </div>
  );
}
