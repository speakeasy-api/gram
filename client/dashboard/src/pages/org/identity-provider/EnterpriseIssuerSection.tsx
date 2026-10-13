import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import { invalidateAllInternalSessionIssuer } from "@gram/client/react-query/internalSessionIssuer.js";
import { useSetInternalSessionIssuerEnabledMutation } from "@gram/client/react-query/setInternalSessionIssuerEnabled.js";
import { useRotateInternalSessionIssuerMutation } from "@gram/client/react-query/rotateInternalSessionIssuer.js";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { CopyButton } from "@/components/ui/CopyButton";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import {
  enterpriseIssuerQueryKey,
  useEnterpriseIssuer,
} from "./useEnterpriseIssuer";

type Props = { ssoFeatureEnabled: boolean };
type Action = "disable" | "rotate" | null;

export function EnterpriseIssuerSection(props: Props): JSX.Element {
  const organization = useOrganization();
  return <EnterpriseIssuerContent key={organization.id} {...props} />;
}

function EnterpriseIssuerContent({ ssoFeatureEnabled }: Props): JSX.Element {
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
  const rotate = useRotateInternalSessionIssuerMutation({ onSuccess: update });
  const issuer = query.data;
  const pending = enable.isPending || rotate.isPending;
  const activeKey = issuer?.keys.find((key) => key.state === "active");
  const canEnable =
    canManage &&
    ssoFeatureEnabled &&
    (issuer?.state === "setup_required" || issuer?.state === "disabled");
  const canDisable =
    canManage && !!issuer?.issuer && issuer.state !== "disabled";
  const canRotate =
    canManage &&
    !!activeKey &&
    !issuer?.rotationReadyAt &&
    (issuer?.state === "enabled" || issuer?.state === "signing_paused");
  const chooseAction = (nextAction: Action) => {
    enable.reset();
    rotate.reset();
    setAction(nextAction);
  };

  return (
    <section
      aria-label="Enterprise authorization"
      className="border-border border-t pt-6"
    >
      <Heading variant="h5">Enterprise authorization</Heading>
      <Text muted small className="mt-1 block">
        Register your organization's issuer with supported services to let your
        team use them through Speakeasy without separate OAuth consent.
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
        {issuer && (
          <IssuerStatus issuer={issuer} ssoFeatureEnabled={ssoFeatureEnabled} />
        )}
        {issuer &&
          ["enabled", "setup_required", "disabled"].includes(issuer.state) && (
            <Text small muted>
              To use enterprise authorization, each user must sign in to
              Speakeasy through your organization's enterprise SSO. Google or
              other social sign-in alone is not sufficient. Enabling the issuer
              does not establish an enterprise SSO session.
            </Text>
          )}
        {issuer?.issuer && (
          <div className="bg-muted flex min-w-0 items-start gap-3 p-4">
            <div className="min-w-0 flex-1">
              <Text small className="font-medium">
                Issuer URI
              </Text>
              <code className="mt-1 block break-all text-sm">
                {issuer.issuer}
              </code>
            </div>
            <CopyButton
              text={issuer.issuer}
              tooltip="Copy issuer URI"
              size="sm"
            />
          </div>
        )}
        {issuer && isDemo && (
          <Text small muted>
            The shared demo is read-only.
          </Text>
        )}
        {issuer && !canManage && !isDemo && (
          <Text small muted>
            Only organization admins signed in directly can change enterprise
            authorization.
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
                  request: { setEnabledRequestBody: { enabled: true } },
                })
              }
            >
              {enable.isPending
                ? "Enabling…"
                : "Enable enterprise authorization"}
            </Button>
          )}
          {canRotate && (
            <Button
              variant="secondary"
              size="sm"
              disabled={pending}
              onClick={() => chooseAction("rotate")}
            >
              Rotate key
            </Button>
          )}
          {canDisable && (
            <Button
              variant="tertiary"
              size="sm"
              disabled={pending}
              onClick={() => chooseAction("disable")}
            >
              Disable enterprise authorization
            </Button>
          )}
        </div>
        {enable.isError && action === null && (
          <Alert variant="error">
            Couldn't enable enterprise authorization. Try again or contact your
            Speakeasy administrator.
          </Alert>
        )}
      </div>
      <ConfirmDialog
        open={action === "disable"}
        onOpenChange={(open) => {
          if (!open) chooseAction(null);
        }}
        title="Disable enterprise authorization?"
        confirmLabel="Disable"
        pendingLabel="Disabling…"
        description="Speakeasy will stop issuing new authorization tokens for this organization. Existing access tokens may remain valid until they expire. The issuer URI is kept for re-enabling later."
        isPending={enable.isPending}
        error={
          enable.isError
            ? "Couldn't disable enterprise authorization. Try again."
            : undefined
        }
        onConfirm={() =>
          enable.mutate({
            request: { setEnabledRequestBody: { enabled: false } },
          })
        }
      />
      <ConfirmDialog
        open={action === "rotate"}
        onOpenChange={(open) => {
          if (!open) chooseAction(null);
        }}
        title="Rotate the signing key?"
        confirmLabel="Rotate key"
        pendingLabel="Preparing key…"
        confirmVariant="primary"
        description="Speakeasy will publish a new key before activating it. The current key continues signing during publication, and the issuer URI stays the same."
        isPending={rotate.isPending}
        error={
          rotate.isError ? (
            <>
              <Text small>
                Couldn't rotate the key. Refresh the issuer and try again.
              </Text>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  chooseAction(null);
                  void query.refetch();
                }}
              >
                Refresh issuer
              </Button>
            </>
          ) : undefined
        }
        onConfirm={() =>
          rotate.mutate({
            request: {
              rotateRequestBody: {
                activeKid: activeKey?.kid,
                compromised: false,
              },
            },
          })
        }
      />
    </section>
  );
}

function IssuerStatus({
  issuer,
  ssoFeatureEnabled,
}: {
  issuer: InternalSessionIssuer;
  ssoFeatureEnabled: boolean;
}): JSX.Element | null {
  let message: string;
  switch (issuer.state) {
    case "enabled":
      if (!issuer.rotationReadyAt) return null;
      message =
        "The new signing key is being published. Speakeasy will activate it automatically when it is ready.";
      break;
    case "setup_required":
      message =
        "Enable enterprise authorization to create your organization's issuer.";
      break;
    case "disabled":
      message = ssoFeatureEnabled
        ? "Enterprise authorization is disabled. Re-enable it to resume issuing authorization tokens."
        : "Enterprise authorization is disabled. Contact your Speakeasy administrator to restore SSO access before re-enabling it.";
      break;
    case "entitlement_required":
      message =
        "Enterprise authorization requires SSO access. Contact your Speakeasy administrator to enable it.";
      break;
    case "signing_paused":
      message =
        "Speakeasy has paused authorization token signing. Contact your Speakeasy administrator to resume it.";
      break;
    case "keyless":
      message =
        "Authorization token signing is stopped because no active signing key is available. Ask an organization administrator to contact Speakeasy support for the signing-key recovery procedure. Recovery is not available on this page.";
      break;
    case "unavailable":
      message =
        "Enterprise authorization is not available for this organization. Contact your Speakeasy administrator.";
      break;
  }
  return (
    <Text small muted role="status">
      {message}
    </Text>
  );
}
