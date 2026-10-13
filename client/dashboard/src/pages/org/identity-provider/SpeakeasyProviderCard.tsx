import { useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import { invalidateAllInternalSessionIssuer } from "@gram/client/react-query/internalSessionIssuer.js";
import { useRotateInternalSessionIssuerMutation } from "@gram/client/react-query/rotateInternalSessionIssuer.js";
import { useSetInternalSessionIssuerEnabledMutation } from "@gram/client/react-query/setInternalSessionIssuerEnabled.js";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { Alert } from "@/components/ui/Alert";
import { Badge, type BadgeProps } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { CopyButton } from "@/components/ui/CopyButton";
import { Heading } from "@/components/ui/Heading";
import { Icon } from "@/components/ui/Icon";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  enterpriseIssuerQueryKey,
  useEnterpriseIssuer,
} from "./useEnterpriseIssuer";

type Action = "disable" | "replace" | null;

type BadgeVariant = NonNullable<BadgeProps["variant"]>;

const STATE_BADGE: Record<
  InternalSessionIssuer["state"],
  { label: string; variant: BadgeVariant }
> = {
  enabled: { label: "Enabled", variant: "success" },
  disabled: { label: "Off", variant: "neutral" },
  setup_required: { label: "Off", variant: "neutral" },
  verified_domain_required: {
    label: "Needs a verified domain",
    variant: "neutral",
  },
  signing_paused: { label: "Paused", variant: "warning" },
  keyless: { label: "Needs attention", variant: "destructive" },
  unavailable: { label: "Unavailable", variant: "neutral" },
};

/** Speakeasy as a built-in identity provider for Enterprise Managed Authorization. */
export function SpeakeasyProviderCard(): JSX.Element {
  const organization = useOrganization();
  return <SpeakeasyProviderContent key={organization.id} />;
}

function SpeakeasyProviderContent(): JSX.Element {
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
  const rotate = useRotateInternalSessionIssuerMutation({
    onSuccess: update,
  });
  const issuer = query.data;
  const pending = enable.isPending || rotate.isPending;
  const canEnable = canManage && issuer?.canEnable;
  const canDisable =
    canManage && !!issuer?.issuer && issuer.state !== "disabled";
  const activeKid = issuer?.keys.find((key) => key.state === "active")?.kid;
  const canReplace = canManage && !!activeKid;
  const canFinish = canManage && issuer?.state === "keyless";
  const chooseAction = (nextAction: Action) => {
    enable.reset();
    rotate.reset();
    setAction(nextAction);
  };
  // A compromised rotation withdraws every current key, then sets up a new
  // one. With no active key left it only finishes the setup.
  const replaceKey = (kid: string | undefined) =>
    rotate.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { rotateRequestBody: { activeKid: kid, compromised: true } },
    });
  const badge = issuer ? STATE_BADGE[issuer.state] : undefined;

  return (
    <article
      aria-label="Speakeasy identity provider"
      className="flex min-w-0 flex-col gap-4 border p-6"
    >
      <div className="flex flex-wrap items-center gap-3">
        <Icon name="key-round" className="h-5 w-5" />
        <Heading variant="h3">Speakeasy</Heading>
        {badge && <Badge variant={badge.variant}>{badge.label}</Badge>}
      </div>
      <Text muted small>
        Enterprise Managed Authorization lets your team reach supported MCP
        servers with their organization sign-in, without authorizing each
        service individually.
      </Text>
      {query.isLoading && (
        <>
          <SkeletonParagraph />
          <span role="status" className="sr-only">
            Loading issuer…
          </span>
        </>
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
            Register this URI in each service's enterprise authorization
            settings. It uses ID-JAG assertions with the JWT Bearer grant
            defined in{" "}
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
      {(canEnable || canDisable || canReplace || canFinish) && (
        <div className="flex flex-wrap gap-2">
          {canFinish && (
            <Button
              variant="primary"
              size="sm"
              disabled={pending}
              onClick={() => {
                chooseAction(null);
                replaceKey(undefined);
              }}
            >
              {rotate.isPending ? "Setting up…" : "Finish setting up a new key"}
            </Button>
          )}
          {canEnable && (
            <Button
              variant="primary"
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
          {canReplace && (
            <SimpleTooltip tooltip="If a signing key may be exposed, stop using it now and set up a new one. The issuer URI stays the same.">
              <Button
                variant="tertiary"
                size="sm"
                disabled={pending}
                onClick={() => chooseAction("replace")}
              >
                Replace compromised key
              </Button>
            </SimpleTooltip>
          )}
        </div>
      )}
      {rotate.isError && action === null && (
        <Alert variant="error">
          Couldn't finish setting up a new key. Try again.
        </Alert>
      )}
      {enable.isError && action === null && (
        <Alert variant="error">
          Couldn't enable Enterprise Managed Authorization. Try again or contact
          your Speakeasy administrator.
        </Alert>
      )}
      <ConfirmDialog
        open={action === "replace"}
        onOpenChange={(open) => {
          if (!open) chooseAction(null);
        }}
        title="Replace the signing key?"
        confirmLabel="Replace key"
        pendingLabel="Replacing…"
        confirmVariant="destructive-primary"
        description={
          <>
            Speakeasy stops signing with every current key right away and
            withdraws them, then sets up a new key. The issuer URI doesn't
            change, so services keep trusting it.
            <br />
            <br />
            Automatic authorization pauses until the new key is ready, and
            services can take a few minutes to see it.
          </>
        }
        isPending={rotate.isPending}
        error={
          rotate.isError ? "Couldn't replace the key. Try again." : undefined
        }
        onConfirm={() => replaceKey(activeKid)}
      />
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
    </article>
  );
}

function IssuerStatus({
  issuer,
}: {
  issuer: InternalSessionIssuer;
}): JSX.Element | null {
  let message: ReactNode;
  switch (issuer.state) {
    case "enabled":
      return null;
    case "setup_required":
      message = issuer.canEnable
        ? "Enable Enterprise Managed Authorization to create your organization's issuer."
        : "Enterprise Managed Authorization cannot be enabled right now. Contact your Speakeasy administrator to check availability and your plan.";
      break;
    case "disabled":
      message = issuer.canEnable
        ? "Enterprise Managed Authorization is disabled. Re-enable it to restore automatic authorization."
        : "Enterprise Managed Authorization is disabled and cannot be re-enabled right now. Contact your Speakeasy administrator to check availability and your plan.";
      break;
    case "verified_domain_required":
      message = (
        <>
          Speakeasy only vouches for emails on your organization's verified
          domains. Verify a domain on the{" "}
          <a href="?tab=sso" className="underline">
            Single sign-on
          </a>{" "}
          tab first.
        </>
      );
      break;
    case "signing_paused":
      message =
        "Enterprise Managed Authorization is temporarily paused. Contact your Speakeasy administrator for help.";
      break;
    case "keyless":
      message =
        "Enterprise Managed Authorization is paused because its signing key was withdrawn. An organization administrator can finish setting up a new key here; the issuer URI stays the same.";
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
