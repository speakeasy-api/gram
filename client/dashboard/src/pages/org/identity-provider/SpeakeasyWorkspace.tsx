import { useEffect, useState, type ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import { Link } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import { invalidateAllInternalSessionIssuer } from "@gram/client/react-query/internalSessionIssuer.js";
import { useRotateInternalSessionIssuerMutation } from "@gram/client/react-query/rotateInternalSessionIssuer.js";
import { useSetInternalSessionIssuerEnabledMutation } from "@gram/client/react-query/setInternalSessionIssuerEnabled.js";
import {
  DangerSettingsSection,
  SettingsSection,
} from "@/components/detail/settings-section";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Heading } from "@/components/ui/Heading";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { IssuerUri } from "./SpeakeasyProviderCard";
import { ISSUER_STATE_BADGE, KEY_STATE_BADGE } from "./speakeasyIssuerView";
import { identityProvidersHref } from "./tabs";
import {
  enterpriseIssuerQueryKey,
  useEnterpriseIssuer,
} from "./useEnterpriseIssuer";

type Action = "disable" | "replace" | "rotate" | null;

/** Settings for Speakeasy's own issuer, one section per concern. */
export function SpeakeasyWorkspace(): JSX.Element {
  const organization = useOrganization();
  return <SpeakeasyWorkspaceContent key={organization.id} />;
}

function SpeakeasyWorkspaceContent(): JSX.Element {
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
  const [replacementError, setReplacementError] = useState<string>();
  const update = (data: InternalSessionIssuer) => {
    queryClient.setQueryData(enterpriseIssuerQueryKey(organization.id), data);
    void invalidateAllInternalSessionIssuer(queryClient);
    setAction(null);
  };
  const enable = useSetInternalSessionIssuerEnabledMutation({
    onSuccess: update,
  });
  const rotate = useRotateInternalSessionIssuerMutation({
    onSuccess: (data) => {
      update(data);
      setReplacementError(data.replacementError);
    },
  });
  const issuer = query.data;
  const pending = enable.isPending || rotate.isPending;
  const canEnable = canManage && issuer?.canEnable;
  const canDisable =
    canManage && !!issuer?.issuer && issuer.state !== "disabled";
  const activeKid = issuer?.keys.find((key) => key.state === "active")?.kid;
  const canReplace = canManage && !!activeKid;
  const canFinish = canManage && issuer?.state === "keyless";
  const canRotate = canManage && issuer?.state === "enabled" && !!activeKid;
  const hasPendingKey = !!issuer?.keys.some((key) => key.state === "pending");
  const readyAt = issuer?.rotationReadyAt;
  const rotationWaiting = useIsFuture(readyAt);
  const rotationReady = hasPendingKey && !rotationWaiting;
  const chooseAction = (nextAction: Action) => {
    enable.reset();
    rotate.reset();
    setReplacementError(undefined);
    setAction(nextAction);
  };
  // A compromised rotation withdraws every current key, then sets up a new
  // one. With no active key left it only finishes the setup.
  const replaceKey = (kid: string | undefined) =>
    rotate.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { rotateRequestBody: { activeKid: kid, compromised: true } },
    });
  // A routine rotation publishes a pending key; once verifiers have had time
  // to fetch it, the same call activates it. An early repeat changes nothing.
  const rotateKey = () =>
    rotate.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { rotateRequestBody: { activeKid, compromised: false } },
    });
  const setEnabled = (enabled: boolean) =>
    enable.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { setEnabledRequestBody: { enabled } },
    });

  return (
    <section className="flex min-w-0 flex-col gap-6" aria-label="Speakeasy">
      <div className="flex flex-col gap-3">
        <Link
          to={identityProvidersHref()}
          className="inline-flex w-fit items-center gap-2 text-sm underline underline-offset-4"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          All identity providers
        </Link>
        <Heading variant="h2">Speakeasy</Heading>
        <Text muted>
          Enterprise Managed Authorization lets your team reach supported MCP
          servers with their organization sign-in, without authorizing each
          service individually.
        </Text>
      </div>
      {query.isLoading && (
        <div className="flex flex-col gap-8">
          <SkeletonParagraph />
          <SkeletonParagraph />
          <span role="status" className="sr-only">
            Loading issuer…
          </span>
        </div>
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
        <div className="flex flex-col gap-10">
          <SettingsSection id="status">
            <SettingsSection.Header>
              <div className="flex flex-wrap items-center gap-3">
                <SettingsSection.Title>Status</SettingsSection.Title>
                <Badge
                  variant={ISSUER_STATE_BADGE[issuer.state].variant}
                  size="sm"
                >
                  {ISSUER_STATE_BADGE[issuer.state].label}
                </Badge>
              </div>
            </SettingsSection.Header>
            <SettingsSection.Panel>
              <SettingsSection.Body>
                <IssuerStatus issuer={issuer} />
                {isDemo && (
                  <Text small muted>
                    The shared demo is read-only.
                  </Text>
                )}
                {!canManage && !isDemo && (
                  <Text small muted>
                    Only organization admins signed in directly can change
                    Enterprise Managed Authorization.
                  </Text>
                )}
                {enable.isError && action === null && (
                  <Alert variant="error">
                    Couldn't enable Enterprise Managed Authorization. Try again
                    or contact your Speakeasy administrator.
                  </Alert>
                )}
              </SettingsSection.Body>
              {canEnable && (
                <SettingsSection.Footer>
                  <SettingsSection.FooterActions>
                    <Button
                      variant="primary"
                      size="sm"
                      disabled={pending}
                      onClick={() => setEnabled(true)}
                    >
                      {enable.isPending
                        ? "Enabling…"
                        : "Enable Enterprise Managed Authorization"}
                    </Button>
                  </SettingsSection.FooterActions>
                </SettingsSection.Footer>
              )}
            </SettingsSection.Panel>
          </SettingsSection>

          {issuer.issuer && (
            <SettingsSection id="issuer">
              <SettingsSection.Header>
                <SettingsSection.Title>Issuer</SettingsSection.Title>
                <SettingsSection.Description>
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
                  . It never changes, even when signing keys are replaced.
                  Registering it alone changes nothing: add Speakeasy to the
                  service's EMA provider order too.
                </SettingsSection.Description>
              </SettingsSection.Header>
              <SettingsSection.Panel>
                <SettingsSection.Body>
                  <IssuerUri uri={issuer.issuer} />
                </SettingsSection.Body>
              </SettingsSection.Panel>
            </SettingsSection>
          )}

          {issuer.issuer && (
            <SettingsSection id="signing-keys">
              <SettingsSection.Header>
                <SettingsSection.Title>Signing keys</SettingsSection.Title>
                <SettingsSection.Description>
                  Speakeasy signs with the active key. Services fetch the
                  published keys to verify each assertion.
                </SettingsSection.Description>
              </SettingsSection.Header>
              <SettingsSection.Panel>
                <SettingsSection.Body>
                  {issuer.keys.length === 0 ? (
                    <Text small muted>
                      No published keys.
                    </Text>
                  ) : (
                    <ul aria-label="Signing keys" className="space-y-2">
                      {issuer.keys.map((key) => (
                        <li
                          key={key.kid}
                          className="flex min-w-0 items-center gap-3"
                        >
                          <code className="min-w-0 text-sm [overflow-wrap:anywhere]">
                            {key.kid}
                          </code>
                          <Badge
                            variant={KEY_STATE_BADGE[key.state].variant}
                            size="sm"
                          >
                            {KEY_STATE_BADGE[key.state].label}
                          </Badge>
                        </li>
                      ))}
                    </ul>
                  )}
                  {issuer.kmsCleanupPending > 0 && (
                    <Text small muted>
                      Speakeasy is still disabling the signing material of{" "}
                      {issuer.kmsCleanupPending} withdrawn{" "}
                      {issuer.kmsCleanupPending === 1 ? "key" : "keys"} and
                      retries until it's done.
                    </Text>
                  )}
                  {replacementError && issuer.state === "keyless" && (
                    <Alert variant="error">{replacementError}</Alert>
                  )}
                  {issuer.state === "enabled" && rotationWaiting && (
                    <Text small muted role="status">
                      New key published. From {formatReadyAt(readyAt!)} it
                      activates automatically within the hour, or sooner with
                      Activate new key. Speakeasy keeps signing with the current
                      key until then.
                    </Text>
                  )}
                  {issuer.state === "enabled" && rotationReady && (
                    <Text small muted role="status">
                      The new key is published and activates automatically
                      within the hour, or now with Activate new key. The old key
                      stays published for a short overlap.
                    </Text>
                  )}
                  {rotate.isError && action === null && (
                    <Alert variant="error">
                      {issuer.state === "keyless"
                        ? "Couldn't finish setting up a new key. Try again."
                        : "Couldn't rotate the key. Try again."}
                    </Alert>
                  )}
                </SettingsSection.Body>
                {(canFinish || canReplace || canRotate) && (
                  <SettingsSection.Footer className="flex-wrap">
                    <SettingsSection.FooterHint>
                      {canFinish
                        ? "The issuer URI stays the same."
                        : "If a signing key may be exposed, stop using it now and set up a new one."}
                    </SettingsSection.FooterHint>
                    <SettingsSection.FooterActions className="shrink flex-wrap justify-end">
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
                          {rotate.isPending
                            ? "Setting up…"
                            : "Finish setting up a new key"}
                        </Button>
                      )}
                      {canRotate && (
                        <Button
                          variant="secondary"
                          size="sm"
                          disabled={pending || rotationWaiting}
                          onClick={() => {
                            if (rotationReady) {
                              chooseAction(null);
                              rotateKey();
                            } else {
                              chooseAction("rotate");
                            }
                          }}
                        >
                          {rotationReady
                            ? rotate.isPending
                              ? "Activating…"
                              : "Activate new key"
                            : "Rotate key"}
                        </Button>
                      )}
                      {canReplace && (
                        <Button
                          variant="destructive-secondary"
                          size="sm"
                          disabled={pending}
                          onClick={() => chooseAction("replace")}
                        >
                          Replace compromised key
                        </Button>
                      )}
                    </SettingsSection.FooterActions>
                  </SettingsSection.Footer>
                )}
              </SettingsSection.Panel>
            </SettingsSection>
          )}

          {/* Sign-in methods, tenant allow-lists and vendor bindings go here. */}

          {canDisable && (
            <DangerSettingsSection id="danger-zone">
              <DangerSettingsSection.Header>
                <DangerSettingsSection.Title>
                  Danger zone
                </DangerSettingsSection.Title>
                <DangerSettingsSection.Description>
                  Turn off automatic authorization. Users will need to authorize
                  affected services individually.
                </DangerSettingsSection.Description>
              </DangerSettingsSection.Header>
              <DangerSettingsSection.Panel>
                <DangerSettingsSection.Body>
                  <div>
                    <Button
                      variant="destructive-primary"
                      size="sm"
                      disabled={pending}
                      onClick={() => chooseAction("disable")}
                    >
                      Disable
                    </Button>
                  </div>
                </DangerSettingsSection.Body>
              </DangerSettingsSection.Panel>
            </DangerSettingsSection>
          )}
        </div>
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
        open={action === "rotate"}
        onOpenChange={(open) => {
          if (!open) chooseAction(null);
        }}
        title="Rotate the signing key?"
        confirmLabel="Rotate key"
        pendingLabel="Rotating…"
        confirmVariant="primary"
        description={
          <>
            Speakeasy publishes a new key now and keeps signing with the current
            one. The new key activates automatically within about an hour, or
            you can activate it here after a couple of minutes. The old key
            stays published for a short overlap, so services keep working
            throughout.
          </>
        }
        isPending={rotate.isPending}
        error={
          rotate.isError ? "Couldn't rotate the key. Try again." : undefined
        }
        onConfirm={rotateKey}
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
        onConfirm={() => setEnabled(false)}
      />
    </section>
  );
}

/** Whether a time is still ahead, re-rendering once when it passes. */
function useIsFuture(at: Date | undefined): boolean {
  const target = at?.getTime();
  const [, setPassed] = useState<number>();
  useEffect(() => {
    if (target === undefined) return;
    const wait = target - Date.now();
    if (wait <= 0) return;
    const timer = setTimeout(() => setPassed(target), wait + 250);
    return () => clearTimeout(timer);
  }, [target]);
  return target !== undefined && target > Date.now();
}

function formatReadyAt(readyAt: Date): string {
  return readyAt.toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
  });
}

function IssuerStatus({
  issuer,
}: {
  issuer: InternalSessionIssuer;
}): JSX.Element {
  let message: ReactNode;
  switch (issuer.state) {
    case "enabled":
      message =
        "The issuer is ready. Speakeasy vouches for your team only to services whose EMA provider order includes it; other services keep their current identity provider.";
      break;
    case "setup_required":
      message = issuer.canEnable
        ? "Enable Enterprise Managed Authorization to create your organization's issuer."
        : "Enterprise Managed Authorization cannot be enabled on this deployment right now. Contact your Speakeasy administrator.";
      break;
    case "disabled":
      message = issuer.canEnable
        ? "Enterprise Managed Authorization is disabled. Re-enable it to restore automatic authorization."
        : "Enterprise Managed Authorization is disabled and cannot be re-enabled right now. Check that your organization still has a verified domain on the Single sign-on tab; if it does, contact your Speakeasy administrator.";
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
        "Enterprise Managed Authorization is paused because its signing key was withdrawn. An organization administrator can finish setting up a new key below; the issuer URI stays the same.";
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
