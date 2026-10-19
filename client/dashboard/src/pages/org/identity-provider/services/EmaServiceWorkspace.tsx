import { useEffect, useMemo } from "react";
import { ArrowLeft } from "lucide-react";
import { Link, useLocation } from "react-router";

import { SettingsSection } from "@/components/detail/settings-section";
import { CopyableValue } from "@/components/setup-steps/StepBlocks";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Skeleton, SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";
import { useOrganizationUserSessionIssuersInfinite } from "@gram/client/react-query/organizationUserSessionIssuers.js";

import { SESSION_SECURITY } from "../identityProviderQueries";
import { identityProvidersHref, oktaViewHref } from "../tabs";
import { useEnterpriseIssuer } from "../useEnterpriseIssuer";
import {
  EmaAllowedProvidersEditor,
  type ProviderCandidate,
} from "./EmaAllowedProvidersEditor";
import { ProviderTrust } from "./ProviderTrust";
import {
  issuerDisplayName,
  PLACEHOLDER_SCOPE,
  providerFixLink,
  addIssuerStep,
  crossAppAccessStep,
  trustCaveat,
  type ProviderView,
} from "./emaServiceView";
import { EmaServiceServers } from "./EmaServiceServers";
import { useEmaServices, useOktaIssuerId } from "./useEmaServices";

/** Who may change which identity providers are enabled for a service, and why not when they can't. */
function useAcceptedProviderPermissions(): {
  canEdit: boolean;
  canRestore: boolean;
  readOnlyReason?: string;
} {
  const organization = useOrganization();
  const session = useSession();
  const { hasScope } = useRBAC();
  const providersFlag = useFeatureFlag(FEATURE_FLAGS.emaProviders);
  if (organization.slug === DEMO_ORG_SLUG) {
    return {
      canEdit: false,
      canRestore: false,
      readOnlyReason: "The shared demo is read-only.",
    };
  }
  if (!hasScope("org:admin", organization.id)) {
    return {
      canEdit: false,
      canRestore: false,
      readOnlyReason:
        "Only organization admins can change which identity providers are enabled for a service.",
    };
  }
  if (session.organizationOverride || session.impersonatorEmail) {
    return {
      canEdit: false,
      canRestore: false,
      readOnlyReason:
        "You can’t change this from a support or impersonated session.",
    };
  }
  if (providersFlag.status !== "enabled") {
    return {
      canEdit: false,
      canRestore: true,
      readOnlyReason:
        "Choosing enabled identity providers isn’t available for your organization yet. You can still go back to the default.",
    };
  }
  return { canEdit: true, canRestore: true };
}

/** One service: what it has to trust, which providers are enabled for it, and which servers connect to it. */
export function EmaServiceWorkspace({
  vendorIssuer,
}: {
  vendorIssuer: string;
}): JSX.Element {
  const organization = useOrganization();
  const services = useEmaServices();
  const row = services.rows.find((r) => r.vendorIssuer === vendorIssuer);
  useScrollToSection(Boolean(row));
  const permissions = useAcceptedProviderPermissions();
  const candidates = useProviderCandidates(permissions.canEdit);
  const oktaFlag = useFeatureFlag(FEATURE_FLAGS.oktaConnections);
  const connectedOktaId = useOktaIssuerId(oktaFlag.status === "enabled");
  const oktaIssuerId =
    oktaFlag.status === "enabled" ? connectedOktaId : undefined;
  const name = row?.name ?? "Service";

  return (
    <section className="flex min-w-0 flex-col gap-6" aria-label={name}>
      <div className="flex flex-col gap-3">
        <Link
          to={identityProvidersHref()}
          className="inline-flex w-fit items-center gap-2 text-sm underline underline-offset-4"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          All identity providers
        </Link>
        {services.isPending ? (
          <Skeleton className="h-8 w-48" />
        ) : (
          <Heading variant="h2">{name}</Heading>
        )}
      </div>
      {services.isPending && (
        <div className="flex flex-col gap-8">
          <SkeletonParagraph />
          <SkeletonParagraph />
          <span role="status" className="sr-only">
            Loading service…
          </span>
        </div>
      )}
      {services.isError && (
        <Alert variant="error">
          <Text small>Couldn’t load this service. Try again.</Text>
          <Button
            variant="secondary"
            size="sm"
            disabled={services.isFetching}
            onClick={services.refetch}
          >
            Retry
          </Button>
        </Alert>
      )}
      {!services.isPending && !services.isError && !row && (
        <Text muted>
          No servers connect to this service anymore.{" "}
          <Link
            to={identityProvidersHref()}
            className="underline underline-offset-4"
          >
            Back to identity providers
          </Link>
        </Text>
      )}
      {row && (
        <div className="flex flex-col gap-10">
          <SettingsSection id="trust">
            <SettingsSection.Header>
              <SettingsSection.Title>What {name} trusts</SettingsSection.Title>
              <SettingsSection.Description>
                {trustCaveat(name)}
              </SettingsSection.Description>
            </SettingsSection.Header>
            <SettingsSection.Panel>
              <SettingsSection.Body>
                <ul aria-label={`${name} trusts`} className="space-y-5">
                  {row.providers.map((provider) => (
                    <TrustRow
                      key={provider.key}
                      provider={provider}
                      isDefault={row.isDefault}
                      service={name}
                      oktaIssuerId={oktaIssuerId}
                    />
                  ))}
                </ul>
              </SettingsSection.Body>
            </SettingsSection.Panel>
          </SettingsSection>
          <EmaAllowedProvidersEditor
            key={`${organization.id}:${row.vendorIssuer}`}
            serviceName={name}
            remoteSessionIssuerId={row.remoteSessionIssuerId}
            current={row.providers}
            isDefault={row.isDefault}
            candidates={candidates.items}
            candidatesReady={candidates.ready}
            candidatesError={candidates.error}
            refreshCandidates={candidates.refresh}
            canEdit={permissions.canEdit}
            canRestore={permissions.canRestore}
            readOnlyReason={permissions.readOnlyReason}
            servers={row.servers}
          />
          <EmaServiceServers row={row} />
        </div>
      )}
    </section>
  );
}

function TrustRow({
  provider,
  isDefault,
  service,
  oktaIssuerId,
}: {
  provider: ProviderView;
  isDefault: boolean;
  service: string;
  /** The Okta connection's issuer, when its Cross App Access view is available. */
  oktaIssuerId: string | undefined;
}): JSX.Element {
  // The Okta fix needs the Okta view, which the rollout can hide.
  const fixTarget =
    provider.fix === "okta" && !oktaIssuerId
      ? "identity-providers"
      : provider.fix;
  const fix = fixTarget ? providerFixLink(fixTarget) : undefined;
  return (
    <li className="space-y-2">
      {/* The placeholder's full scope follows below, so its short one would repeat it. */}
      <ProviderTrust
        provider={provider}
        isDefault={isDefault && !provider.placeholder}
      />
      {provider.placeholder && (
        <Text muted small className="block">
          {PLACEHOLDER_SCOPE}
        </Text>
      )}
      {!provider.placeholder &&
        provider.kind !== "speakeasy" &&
        provider.usable &&
        (oktaIssuerId && provider.remoteSessionIssuerId === oktaIssuerId ? (
          <Link
            className="text-muted-foreground block text-sm underline underline-offset-4"
            to={oktaViewHref("cross-app-access")}
          >
            {crossAppAccessStep(service, provider.name)}
          </Link>
        ) : (
          <Text muted small className="block">
            {crossAppAccessStep(service, provider.name)}
          </Text>
        ))}
      {provider.kind === "speakeasy" &&
        provider.usable &&
        provider.issuerUrl &&
        !provider.lastAcceptedAt && (
          <Text muted small className="block">
            {addIssuerStep(service)}
          </Text>
        )}
      {provider.kind === "speakeasy" &&
        provider.usable &&
        provider.issuerUrl && (
          <div className="max-w-2xl">
            <CopyableValue
              label={`${provider.name}’s issuer URL`}
              value={provider.issuerUrl}
            />
          </div>
        )}
      {fix && (
        <Link
          className="text-muted-foreground text-sm underline underline-offset-4"
          to={fix.href}
        >
          {fix.label}
        </Link>
      )}
    </li>
  );
}

/**
 * Identity providers an admin can choose for a service to accept: each
 * organization-level identity provider a sign-in trusts, and Speakeasy once it
 * exists.
 */
function useProviderCandidates(enabled: boolean): {
  items: ProviderCandidate[];
  ready: boolean;
  error?: { retry: () => void };
  /** Reload the candidates, after another admin's change. */
  refresh: () => Promise<unknown>;
} {
  const speakeasy = useEnterpriseIssuer();
  const signIns = useOrganizationUserSessionIssuersInfinite(
    { tier: "organization" },
    SESSION_SECURITY,
    { enabled, throwOnError: false, retry: false },
  );
  useDrainInfiniteQuery(signIns, enabled);
  const externalIds = useMemo(
    () => [
      ...new Set(
        (signIns.data?.pages ?? []).flatMap((page) =>
          page.result.items.flatMap((issuer) =>
            issuer.projectId === "" && issuer.trustedRemoteSessionIssuerId
              ? [issuer.trustedRemoteSessionIssuerId]
              : [],
          ),
        ),
      ),
    ],
    [signIns.data],
  );
  const issuers = useRemoteSessionIssuersByIds(externalIds, { enabled });
  const items: ProviderCandidate[] = externalIds.map((id) => ({
    key: `external-${id}`,
    kind: "external",
    name: issuerDisplayName(issuers.items.find((issuer) => issuer.id === id)),
    remoteSessionIssuerId: id,
  }));
  if (speakeasy.data?.issuer) {
    items.push({ key: "speakeasy", kind: "speakeasy", name: "Speakeasy" });
  }
  const failed = signIns.isError || speakeasy.isError || issuers.isError;
  const ready =
    enabled &&
    !failed &&
    signIns.isSuccess &&
    !signIns.hasNextPage &&
    !speakeasy.isPending &&
    !issuers.isLoading;
  const error =
    enabled && failed
      ? {
          retry: () => {
            if (signIns.isError) void signIns.refetch();
            if (speakeasy.isError) void speakeasy.refetch();
            if (issuers.isError) issuers.refetch();
          },
        }
      : undefined;
  const refresh = () =>
    enabled
      ? Promise.all([signIns.refetch(), speakeasy.refetch()])
      : Promise.resolve();
  return { items, ready, error, refresh };
}

// Sections mount once the service loads, so a deep link waits for it.
function useScrollToSection(ready: boolean): void {
  const { hash } = useLocation();
  useEffect(() => {
    const id = hash.replace("#", "");
    if (!ready || !id) return;
    const frame = window.requestAnimationFrame(() => {
      document
        .getElementById(id)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [hash, ready]);
}
