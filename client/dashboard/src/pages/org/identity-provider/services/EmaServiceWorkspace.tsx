import { ArrowLeft } from "lucide-react";
import { Link } from "react-router";

import { SettingsSection } from "@/components/detail/settings-section";
import { CopyableValue } from "@/components/setup-steps/StepBlocks";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";
import { useOrganizationUserSessionIssuers } from "@gram/client/react-query/organizationUserSessionIssuers.js";

import {
  identityProvidersHref,
  oktaViewHref,
  speakeasyProviderHref,
} from "../tabs";
import { useEnterpriseIssuer } from "../useEnterpriseIssuer";
import {
  EmaProviderOrderEditor,
  type OrderCandidate,
} from "./EmaProviderOrderEditor";
import { ProviderTrust } from "./ProviderTrust";
import {
  issuerDisplayName,
  trustInstruction,
  type ProviderView,
} from "./emaServiceView";
import { serversSummary, useEmaServices } from "./useEmaServices";

/** One service: what it has to trust, in which order, and who reaches it. */
export function EmaServiceWorkspace({
  vendorIssuer,
}: {
  vendorIssuer: string;
}): JSX.Element {
  const services = useEmaServices();
  const row = services.rows.find((r) => r.vendorIssuer === vendorIssuer);
  const organization = useOrganization();
  const session = useSession();
  const { hasScope } = useRBAC();
  const orderFlag = useFeatureFlag(FEATURE_FLAGS.emaProviders);
  const canEdit =
    hasScope("org:admin", organization.id) &&
    orderFlag.status === "enabled" &&
    organization.slug !== DEMO_ORG_SLUG &&
    !session.organizationOverride &&
    !session.impersonatorEmail;
  const candidates = useOrderCandidates(canEdit);
  const name = row?.name ?? vendorIssuer;

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
        <Heading variant="h2">{name}</Heading>
        <Text muted>
          {row
            ? `${serversSummary(row)} reach ${name} through Enterprise Managed Authorization.`
            : "A service Enterprise Managed Authorization reaches."}
        </Text>
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
                    <TrustRow key={provider.key} provider={provider} />
                  ))}
                </ul>
              </SettingsSection.Body>
            </SettingsSection.Panel>
          </SettingsSection>
          <EmaProviderOrderEditor
            serviceName={name}
            remoteSessionIssuerId={row.remoteSessionIssuerId}
            current={row.providers}
            isDefault={row.isDefaultOrder}
            candidates={candidates}
            canEdit={canEdit}
          />
        </div>
      )}
    </section>
  );
}

function TrustRow({ provider }: { provider: ProviderView }): JSX.Element {
  // The default order's external placeholder stands for each server's own
  // identity provider, which has no single issuer URL to show here.
  const placeholder =
    provider.kind === "external" && provider.key === "external-trusted";
  return (
    <li className="space-y-2">
      <ProviderTrust
        provider={
          placeholder
            ? { ...provider, name: "Each server’s own identity provider" }
            : provider
        }
      />
      {placeholder && (
        <Text muted small className="block">
          Whichever identity provider a server’s sign-in uses. Its issuer URL is
          on that server’s Enterprise Managed Authorization settings.
        </Text>
      )}
      {!placeholder && provider.usable && provider.issuerUrl && (
        <div className="max-w-2xl">
          <CopyableValue
            label={`${provider.name}’s issuer URL`}
            value={provider.issuerUrl}
          />
        </div>
      )}
      {provider.fix && (
        <Link
          className="text-muted-foreground text-sm underline underline-offset-4"
          to={
            provider.fix === "speakeasy"
              ? speakeasyProviderHref()
              : oktaViewHref("setup")
          }
        >
          {provider.fix === "speakeasy" ? "Fix in Speakeasy" : "Fix in Okta"}
        </Link>
      )}
    </li>
  );
}

/** Identity providers an admin can put in an order: Speakeasy once it exists, and each organization-level identity provider a sign-in trusts. */
function useOrderCandidates(enabled: boolean): OrderCandidate[] {
  const speakeasy = useEnterpriseIssuer();
  const signIns = useOrganizationUserSessionIssuers(
    { tier: "organization" },
    { sessionHeaderGramSession: "" },
    { enabled, throwOnError: false, retry: false },
  );
  const externalIds = [
    ...new Set(
      (signIns.data?.result.items ?? []).flatMap((issuer) =>
        issuer.projectId === "" && issuer.trustedRemoteSessionIssuerId
          ? [issuer.trustedRemoteSessionIssuerId]
          : [],
      ),
    ),
  ];
  const issuers = useRemoteSessionIssuersByIds(externalIds, { enabled });
  const candidates: OrderCandidate[] = [];
  if (speakeasy.data?.issuer) {
    candidates.push({
      key: "speakeasy",
      name: "Speakeasy",
      input: { kind: "speakeasy" },
    });
  }
  for (const id of externalIds) {
    candidates.push({
      key: `external-${id}`,
      name: issuerDisplayName(issuers.items.find((issuer) => issuer.id === id)),
      input: { kind: "external", remoteSessionIssuerId: id },
    });
  }
  return candidates;
}
