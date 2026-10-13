import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import { invalidateAllInternalSessionIssuer } from "@gram/client/react-query/internalSessionIssuer.js";
import { useSetInternalSessionIssuerEnabledMutation } from "@gram/client/react-query/setInternalSessionIssuerEnabled.js";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { CopyButton } from "@/components/ui/CopyButton";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  enterpriseIssuerQueryKey,
  useEnterpriseIssuer,
} from "./useEnterpriseIssuer";

type Action = "disable" | null;

export function EnterpriseIssuerSection(): JSX.Element {
  const organization = useOrganization();
  return <EnterpriseIssuerContent key={organization.id} />;
}

function EnterpriseIssuerContent(): JSX.Element {
  const organization = useOrganization();
  const session = useSession();
  const { hasScope } = useRBAC();
  const isDemo = organization.slug === DEMO_ORG_SLUG;
  const canManage =
    hasScope("org:admin", organization.id) &&
    !isDemo &&
    !session.organizationOverride &&
    !session.impersonatorEmail;
  const query = useEnterpriseIssuer();
  const queryClient = useQueryClient();
  const [action, setAction] = useState<Action>(null);
  const update = (data: InternalSessionIssuer) => {
    queryClient.setQueryData(enterpriseIssuerQueryKey(organization.id), data);
    void invalidateAllInternalSessionIssuer(queryClient);
    setAction(null);
  };
  const enable = useSetInternalSessionIssuerEnabledMutation({
    onSuccess: update,
  });
  const issuer = query.data;
  const pending = enable.isPending;
  const canEnable = canManage && issuer?.canEnable;
  const canDisable =
    canManage && !!issuer?.issuer && issuer.state !== "disabled";
  const chooseAction = (nextAction: Action) => {
    enable.reset();
    setAction(nextAction);
  };

  return (
    <section
      aria-label="Enterprise Managed Authorization"
      className="border-border border-t pt-6"
    >
      <Heading variant="h5">Enterprise Managed Authorization</Heading>
      <Text muted small className="mt-1 block">
        Let your team access supported MCP servers using their organization
        identity, without authorizing each service individually.
      </Text>
      <div className="mt-4 min-h-24 space-y-3">
        {query.isPending && (
          <Text small muted role="status">
            Loading issuer…
          </Text>
        )}
        {query.isError && (
          <Alert variant="error">
            <Text small>Couldn't load the issuer. Try again.</Text>
            <Button
              variant="secondary"
              size="sm"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              Retry
            </Button>
          </Alert>
        )}
        {issuer && <IssuerStatus issuer={issuer} />}
        {issuer?.issuer && (
          <>
            <Text small muted>
              Register the issuer URI in each service's enterprise authorization
              settings.
            </Text>
            <dl className="space-y-1">
              <dt className="text-eyebrow">Issuer URI</dt>
              <dd className="flex min-w-0 items-center gap-2">
                <code className="min-w-0 text-sm [overflow-wrap:anywhere]">
                  {issuer.issuer}
                </code>
                <span className="shrink-0">
                  <CopyButton
                    text={issuer.issuer}
                    tooltip="Copy issuer URI"
                    size="xs"
                  />
                </span>
              </dd>
            </dl>
            <Text small muted>
              Uses ID-JAG assertions with the JWT Bearer grant defined in{" "}
              <a
                href="https://www.rfc-editor.org/rfc/rfc7523.html"
                target="_blank"
                rel="noopener noreferrer"
                className="underline underline-offset-2"
              >
                RFC 7523
              </a>
              .
            </Text>
          </>
        )}
        {issuer && isDemo && (
          <Text small muted>
            The shared demo is read-only.
          </Text>
        )}
        {issuer && !canManage && !isDemo && (
          <Text small muted>
            Only organization admins signed in directly can change Enterprise
            Managed Authorization.
          </Text>
        )}
        <div className="flex flex-wrap gap-2">
          {canEnable && (
            <Button
              variant="secondary"
              size="sm"
              disabled={pending}
              onClick={() =>
                enable.mutate({
                  security: { sessionHeaderGramSession: "" },
                  request: { setEnabledRequestBody: { enabled: true } },
                })
              }
            >
              {enable.isPending
                ? "Enabling…"
                : "Enable Enterprise Managed Authorization"}
            </Button>
          )}
          {canDisable && (
            <SimpleTooltip tooltip="Turn off automatic authorization. Users will need to authorize affected services individually.">
              <Button
                variant="tertiary"
                size="sm"
                disabled={pending}
                onClick={() => chooseAction("disable")}
              >
                Disable
              </Button>
            </SimpleTooltip>
          )}
        </div>
        {enable.isError && action === null && (
          <Alert variant="error">
            Couldn't enable Enterprise Managed Authorization. Try again or
            contact your Speakeasy administrator.
          </Alert>
        )}
      </div>
      <ConfirmDialog
        open={action === "disable"}
        onOpenChange={(open) => {
          if (!open) chooseAction(null);
        }}
        title="Disable Enterprise Managed Authorization?"
        confirmLabel="Disable"
        pendingLabel="Disabling…"
        description={
          <>
            Users will need their own authorized connections to affected
            services. Existing individual connections will continue to work.
            Access already granted through Enterprise Managed Authorization may
            continue until it expires.
            <br />
            <br />
            You can re-enable this later using the same issuer URI.
          </>
        }
        isPending={enable.isPending}
        error={
          enable.isError
            ? "Couldn't disable Enterprise Managed Authorization. Try again."
            : undefined
        }
        onConfirm={() =>
          enable.mutate({
            security: { sessionHeaderGramSession: "" },
            request: { setEnabledRequestBody: { enabled: false } },
          })
        }
      />
    </section>
  );
}

function IssuerStatus({
  issuer,
}: {
  issuer: InternalSessionIssuer;
}): JSX.Element | null {
  let message: string;
  switch (issuer.state) {
    case "enabled":
      return null;
    case "setup_required":
      message = issuer.canEnable
        ? "Enable Enterprise Managed Authorization to create your organization's issuer."
        : "Enterprise Managed Authorization cannot be enabled right now. Contact your Speakeasy administrator to check availability and SSO access.";
      break;
    case "disabled":
      message = issuer.canEnable
        ? "Enterprise Managed Authorization is disabled. Re-enable it to restore automatic authorization."
        : "Enterprise Managed Authorization is disabled and cannot be re-enabled right now. Contact your Speakeasy administrator to check availability and SSO access.";
      break;
    case "entitlement_required":
      message =
        "Enterprise Managed Authorization requires SSO access. Contact your Speakeasy administrator to enable it.";
      break;
    case "signing_paused":
      message =
        "Enterprise Managed Authorization is temporarily paused. Contact your Speakeasy administrator for help.";
      break;
    case "keyless":
      message =
        "Enterprise Managed Authorization is unavailable. Ask an organization administrator to contact Speakeasy support to restore it.";
      break;
    case "unavailable":
      message =
        "Enterprise Managed Authorization is not available for this organization. Contact your Speakeasy administrator.";
      break;
  }
  return (
    <Text small muted role="status">
      {message}
    </Text>
  );
}
