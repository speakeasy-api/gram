import { useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useReplaceIdentityProviderConnectionClientSecretMutation } from "@gram/client/react-query/replaceIdentityProviderConnectionClientSecret.js";

import {
  dpopObservation,
  isConnectionChecked,
  LISTING_MODE_LABELS,
} from "../../connectionView";
import {
  inlineError,
  invalidateIdentityProviderQueries,
  SESSION_SECURITY,
} from "../../identityProviderQueries";
import { ClientSecretField } from "./OktaConnectionForms";

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

export function CopyableValue({
  value,
  tooltip = "Copy",
}: {
  value: string;
  tooltip?: string;
}): JSX.Element {
  return (
    <span className="inline-flex min-w-0 max-w-full items-center gap-2">
      <span className="min-w-0 [overflow-wrap:anywhere]">{value}</span>
      <span className="shrink-0">
        <CopyButton text={value} size="xs" tooltip={tooltip} />
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
  switch (dpopObservation(connection)) {
    case "unknown":
      return (
        <Text muted small>
          Not checked yet
        </Text>
      );
    case "protected":
      return (
        <Badge variant="success" size="sm">
          Protected
        </Badge>
      );
    case "unprotected":
      return (
        <Badge variant="neutral" size="sm">
          Not protected
        </Badge>
      );
  }
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
      {connection.jwksUrl && (
        <Fact label="Public key URL (JWKS)" mono>
          <CopyableValue value={connection.jwksUrl} />
        </Fact>
      )}
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
        {LISTING_MODE_LABELS[connection.listingMode]}
      </Fact>
      <Fact label="Token protection (DPoP)">
        <DpopBadge connection={connection} />
      </Fact>
      <Fact label="Last verified">
        <LastVerified connection={connection} />
      </Fact>
      {connection.jwksUrl && (
        <Fact label="Active signing key ID" mono>
          {connection.activeKey ? (
            <>
              <CopyableValue value={connection.activeKey.kid} />
              <Text muted small className="font-sans">
                since{" "}
                <HumanizeDateTime date={connection.activeKey.activatedAt} />
              </Text>
            </>
          ) : (
            <Text muted small>
              Withdrawn
            </Text>
          )}
        </Fact>
      )}
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

/** OIN connections authenticate with a client secret; rotating it re-verifies and keeps the old one on rejection. */
export function ReplaceClientSecretButton({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState("");
  const replace = useReplaceIdentityProviderConnectionClientSecretMutation({
    // The request variables carry the plaintext secret; reset clears them.
    gcTime: 0,
    onSuccess: (updated) => {
      replace.reset();
      if (updated.status === "verified") {
        toast.success("Client secret replaced");
      } else {
        toast.warning(
          "Client secret replaced. Review the verification results.",
        );
      }
      setOpen(false);
      setSecret("");
      void invalidateIdentityProviderQueries(queryClient);
    },
    onError: inlineError,
  });
  const trimmed = secret.trim();
  const close = () => {
    if (replace.isPending) return;
    setOpen(false);
    setSecret("");
    replace.reset();
  };
  const submit = () => {
    if (trimmed === "" || replace.isPending) return;
    replace.mutate({
      security: SESSION_SECURITY,
      request: {
        replaceIdentityProviderConnectionClientSecretRequestBody: {
          id: connection.id,
          clientSecret: trimmed,
        },
      },
    });
  };

  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        Replace client secret
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (next) setOpen(true);
          else close();
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Replace the client secret</Dialog.Title>
            <Dialog.Description>
              Generate a new client secret for the Speakeasy app in Okta and
              paste it here. Speakeasy checks it with Okta before saving. If
              Okta rejects it, the current secret stays in use.
            </Dialog.Description>
          </Dialog.Header>
          <div className="space-y-3 py-2">
            <ClientSecretField
              replacement
              id="okta-replace-client-secret"
              value={secret}
              onChange={setSecret}
              onEnter={submit}
              disabled={replace.isPending}
            />
            {replace.error && (
              <p role="alert" className="text-destructive text-sm">
                Unable to replace the client secret. Check the Okta app settings
                and try again.
              </p>
            )}
          </div>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={close}
              disabled={replace.isPending}
            >
              Cancel
            </Button>
            <Button
              disabled={trimmed === "" || replace.isPending}
              onClick={submit}
            >
              {replace.isPending ? "Verifying..." : "Replace and verify"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
