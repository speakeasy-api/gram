import { useMemo } from "react";
import { ArrowLeft } from "lucide-react";
import { Link } from "react-router";

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
import { identityProvidersHref } from "../tabs";
import { useEnterpriseIssuer } from "../useEnterpriseIssuer";
import {
  EmaProviderOrderEditor,
  type OrderCandidate,
} from "./EmaProviderOrderEditor";
import { ProviderTrust } from "./ProviderTrust";
import {
  issuerDisplayName,
  PLACEHOLDER_SCOPE,
  providerFixLink,
  trustInstruction,
  type ProviderView,
} from "./emaServiceView";
import { EmaServiceServers } from "./EmaServiceServers";
import { serverCountLabel, useEmaServices } from "./useEmaServices";

/** Who may change a service's order, and why not when they can't. */
function useOrderPermissions(): {
  canEdit: boolean;
  canRestore: boolean;
  readOnlyReason?: string;
} {
  const organization = useOrganization();
  const session = useSession();
  const { hasScope } = useRBAC();
  const orderFlag = useFeatureFlag(FEATURE_FLAGS.emaProviders);
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
      readOnlyReason: "Only organization admins can change the order.",
    };
  }
  if (session.organizationOverride || session.impersonatorEmail) {
    return {
      canEdit: false,
      canRestore: false,
      readOnlyReason:
        "The order can’t be changed from a support or impersonated session.",
    };
  }
  if (orderFlag.status !== "enabled") {
    return {
      canEdit: false,
      canRestore: true,
      readOnlyReason:
        "Custom orders aren’t available for your organization yet. You can still go back to the default.",
    };
  }
  return { canEdit: true, canRestore: true };
}

/** One service: what it has to trust, in which order, and who reaches it. */
export function EmaServiceWorkspace({
  vendorIssuer,
}: {
  vendorIssuer: string;
}): JSX.Element {
  const services = useEmaServices();
  const row = services.rows.find((r) => r.vendorIssuer === vendorIssuer);
  const permissions = useOrderPermissions();
  const candidates = useOrderCandidates(permissions.canEdit);
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
          <>
            <Skeleton className="h-8 w-48" />
            <Skeleton className="h-5 w-96 max-w-full" />
          </>
        ) : (
          <>
            <Heading variant="h2">{name}</Heading>
            <Text muted>
              {!row
                ? "A service Enterprise Managed Authorization reaches."
                : row.serverCount === 0
                  ? `No servers reach ${name} through Enterprise Managed Authorization yet.`
                  : `${serverCountLabel(row.serverCount)} ${row.serverCount === 1 ? "reaches" : "reach"} ${name} through Enterprise Managed Authorization.`}
            </Text>
          </>
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
          No server reaches this service any more.{" "}
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
                {trustInstruction(name)}
              </SettingsSection.Description>
            </SettingsSection.Header>
            <SettingsSection.Panel>
              <SettingsSection.Body>
                <ul aria-label={`${name} trusts`} className="space-y-5">
                  {row.providers.map((provider) => (
                    <TrustRow
                      key={provider.key}
                      provider={provider}
                      isDefaultOrder={row.isDefaultOrder}
                    />
                  ))}
                </ul>
              </SettingsSection.Body>
            </SettingsSection.Panel>
          </SettingsSection>
          <EmaProviderOrderEditor
            key={row.vendorIssuer}
            serviceName={name}
            remoteSessionIssuerId={row.remoteSessionIssuerId}
            current={row.providers}
            isDefault={row.isDefaultOrder}
            candidates={candidates.items}
            candidatesReady={candidates.ready}
            candidatesError={candidates.error}
            canEdit={permissions.canEdit}
            canRestore={permissions.canRestore}
            readOnlyReason={permissions.readOnlyReason}
          />
          <EmaServiceServers row={row} />
        </div>
      )}
    </section>
  );
}

function TrustRow({
  provider,
  isDefaultOrder,
}: {
  provider: ProviderView;
  isDefaultOrder: boolean;
}): JSX.Element {
  const fix = provider.fix ? providerFixLink(provider.fix) : undefined;
  return (
    <li className="space-y-2">
      {/* The placeholder's full scope follows below, so its short one would repeat it. */}
      <ProviderTrust
        provider={provider}
        isDefaultOrder={isDefaultOrder && !provider.placeholder}
      />
      {provider.placeholder && (
        <Text muted small className="block">
          {PLACEHOLDER_SCOPE}
        </Text>
      )}
      {!provider.placeholder && provider.usable && provider.issuerUrl && (
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
 * Identity providers an admin can put in an order: each organization-level
 * identity provider a sign-in trusts, and Speakeasy once it exists.
 */
function useOrderCandidates(enabled: boolean): {
  items: OrderCandidate[];
  ready: boolean;
  error?: { retry: () => void };
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
  const items: OrderCandidate[] = externalIds.map((id) => ({
    key: `external-${id}`,
    kind: "external",
    name: issuerDisplayName(issuers.items.find((issuer) => issuer.id === id)),
    remoteSessionIssuerId: id,
  }));
  if (speakeasy.data?.issuer) {
    items.push({ key: "speakeasy", kind: "speakeasy", name: "Speakeasy" });
  }
  const ready =
    enabled &&
    signIns.isSuccess &&
    !signIns.hasNextPage &&
    !speakeasy.isPending &&
    !issuers.isLoading;
  const error =
    enabled && signIns.isError
      ? { retry: () => void signIns.refetch() }
      : undefined;
  return { items, ready, error };
}
