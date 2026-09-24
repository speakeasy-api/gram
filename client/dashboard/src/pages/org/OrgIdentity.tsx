import { parseAsStringLiteral, useQueryState } from "nuqs";

import { TabbedPage, type PageTab } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { useOrganization, useSessionData } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query/generateWorkOSAdminPortalLink.js";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { FolderSync, Globe, Loader2, Lock } from "lucide-react";
import { toast } from "sonner";

import { EnterpriseManagedAuth } from "./identity-provider/EnterpriseManagedAuth";
import {
  enterpriseManagedAuthHref,
  IDENTITY_TABS,
  type IdentityPageTab,
} from "./identity-provider/tabs";

const UPSELL_COPY = "Contact our team to setup SSO and Directory Sync";

type IdentitySectionId = "domain_verification" | "sso" | "directory_sync";

type IdentityCardProps = {
  sectionId: IdentitySectionId;
  heading: string;
  description: React.ReactNode;
  providerIcon: React.ReactNode;
  providerTitle: string;
  providerSubtitle: string;
  learnMoreText: string;
  learnMoreHref: string;
  active?: boolean;
  activeLabel?: string;
  /** Dims the card and disables its control until a domain is verified. */
  blocked?: boolean;
  configureButton?: React.ReactNode;
  children?: React.ReactNode;
};

/**
 * Notifies our team that a non-entitled org wants SSO / Directory Sync. This
 * capture backs the "our team has been contacted" upsell toast, so it stays even
 * though the entitled-org configure buttons no longer emit tracking events.
 */
function useUpsellInterestCapture(sectionId: IdentitySectionId) {
  const telemetry = useTelemetry();
  const { session } = useSessionData();

  return () => {
    telemetry.capture("identity_provider_interest", {
      section: sectionId,
      action: "configure_clicked",
      email: session?.user.email ?? "",
      organization_id: session?.organization?.id ?? "",
      organization_name: session?.organization?.name ?? "",
      organization_slug: session?.organization?.slug ?? "",
    });
  };
}

function ConfigureButton({ sectionId }: { sectionId: IdentitySectionId }) {
  const captureInterest = useUpsellInterestCapture(sectionId);

  return (
    <SimpleTooltip tooltip={UPSELL_COPY}>
      <Button
        variant="secondary"
        size="sm"
        onClick={() => {
          captureInterest();
          toast.success(
            "Our team has been contacted to enable SSO and Directory Sync",
          );
        }}
      >
        Configure
      </Button>
    </SimpleTooltip>
  );
}

/**
 * Routes an admin into Gram's guided setup wizard at the relevant step instead
 * of bouncing them straight to the WorkOS admin portal. Used when SSO / Directory
 * Sync has not been configured yet so first-run setup happens in-product.
 */
function SetupStepButton() {
  const orgRoutes = useOrgRoutes();

  return (
    <RequireScope scope="org:admin" level="component">
      <orgRoutes.setup.Link queryParams={{ task: "idp" }}>
        <Button variant="secondary" size="sm">
          Configure
        </Button>
      </orgRoutes.setup.Link>
    </RequireScope>
  );
}

/** Launches the WorkOS admin portal for the given intent. */
function WorkOSPortalButton({
  intent,
  errorFallback,
  label = "Configure",
}: {
  intent: "sso" | "dsync" | "domain_verification";
  errorFallback: string;
  label?: string;
}) {
  const generatePortalLink = useGenerateWorkOSAdminPortalLinkMutation({
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : errorFallback);
    },
  });

  const launchPortal = () => {
    generatePortalLink.mutate(
      {
        request: {
          generateWorkOSAdminPortalLinkRequestBody: {
            intent,
          },
        },
      },
      {
        onSuccess: (data) => {
          if (openSafeExternalUrl(data.url)) {
            toast.info("Continue setup in the WorkOS portal");
          } else {
            toast.error("Unable to open the WorkOS portal");
          }
        },
      },
    );
  };

  return (
    <RequireScope scope="org:admin" level="component">
      <Button
        variant="secondary"
        size="sm"
        onClick={launchPortal}
        disabled={generatePortalLink.isPending}
      >
        {generatePortalLink.isPending && (
          <Button.LeftIcon>
            <Loader2 className="h-4 w-4 animate-spin" />
          </Button.LeftIcon>
        )}
        {label}
      </Button>
    </RequireScope>
  );
}

/**
 * Picks the SSO configure control: upsell when the feature is not entitled, the
 * WorkOS portal launcher once a connection exists, otherwise the in-product
 * setup wizard for first-run configuration.
 */
function SSOConfigureControl({
  featureEnabled,
  active,
}: {
  featureEnabled: boolean;
  active: boolean;
}) {
  if (!featureEnabled) return <ConfigureButton sectionId="sso" />;
  if (active) {
    return (
      <WorkOSPortalButton
        intent="sso"
        errorFallback="Failed to start SSO setup"
      />
    );
  }
  return <SetupStepButton />;
}

/**
 * Picks the Directory Sync configure control, mirroring {@link SSOConfigureControl}:
 * upsell, WorkOS portal launcher, or the in-product setup wizard.
 */
function DirectorySyncConfigureControl({
  featureEnabled,
  active,
}: {
  featureEnabled: boolean;
  active: boolean;
}) {
  if (!featureEnabled) return <ConfigureButton sectionId="directory_sync" />;
  if (active) {
    return (
      <WorkOSPortalButton
        intent="dsync"
        errorFallback="Failed to start Directory Sync setup"
      />
    );
  }
  return <SetupStepButton />;
}

function IdentitySection({
  sectionId,
  heading,
  description,
  providerIcon,
  providerTitle,
  providerSubtitle,
  learnMoreText,
  learnMoreHref,
  active,
  activeLabel = "Connected",
  blocked = false,
  configureButton,
  children,
}: IdentityCardProps) {
  let control = configureButton ?? <ConfigureButton sectionId={sectionId} />;
  // A blocked card replaces every control, including the upsell, so nothing
  // starts setup that WorkOS would reject without a verified domain.
  if (blocked) {
    control = (
      <Button variant="secondary" size="sm" disabled>
        Configure
      </Button>
    );
  }

  return (
    <section>
      <div className="flex flex-col">
        <Heading variant="h5" className="mb-1">
          {heading}
        </Heading>
        <Text as="div" muted small className="mb-4">
          {description}
        </Text>
        {blocked && (
          <Alert variant="warning" className="mb-4">
            <Text small className="text-inherit">
              Verify a domain above before setting up {heading}.
            </Text>
          </Alert>
        )}
        <div
          aria-disabled={blocked || undefined}
          className={cn(
            "border-border overflow-hidden border",
            blocked && "opacity-70",
          )}
        >
          <div className="flex items-center gap-4 p-4">
            <div className="bg-muted flex h-10 w-10 shrink-0 items-center justify-center rounded-full">
              {providerIcon}
            </div>
            <div className="min-w-0 flex-1">
              <Text variant="body" className="font-medium">
                {providerTitle}
              </Text>
              <Text muted small>
                {providerSubtitle}
              </Text>
              {active && (
                <Badge variant="success" className="mt-1.5">
                  <Badge.Text>{activeLabel}</Badge.Text>
                </Badge>
              )}
            </div>
            {control}
          </div>
          {children}
        </div>
        <a
          href={learnMoreHref}
          target="_blank"
          rel="noopener noreferrer"
          className="text-muted-foreground hover:text-foreground mt-4 ml-auto block text-sm underline underline-offset-4 transition-colors"
        >
          {learnMoreText}
        </a>
      </div>
    </section>
  );
}

export default function OrgIdentity(): JSX.Element {
  const [requestedTab] = useQueryState(
    "tab",
    parseAsStringLiteral(IDENTITY_TABS).withDefault("sso"),
  );
  // The only read of the rollout flag: without it (or org:admin) the tab does not exist.
  const providerFlag = useFeatureFlag(FEATURE_FLAGS.oktaConnections);
  const { hasScope } = useRBAC();
  const showEnterpriseManagedAuth =
    providerFlag.status === "enabled" && hasScope("org:admin");

  const activeTab: IdentityPageTab =
    requestedTab !== "sso" && !showEnterpriseManagedAuth ? "sso" : requestedTab;

  const tabs: PageTab[] = [
    { value: "sso", label: "Single sign-on", href: "?tab=sso" },
    ...(showEnterpriseManagedAuth
      ? [
          {
            value: "enterprise-managed-auth",
            label: "Enterprise Managed Auth",
            href: enterpriseManagedAuthHref(),
            stage: "preview" as const,
          },
        ]
      : []),
  ];

  return (
    <TabbedPage
      scope={["org:read", "org:admin"]}
      title="IDP and SSO"
      description="Manage employee sign-in, directory sync, and agent access through your identity providers."
      activeTab={activeTab}
      tabs={tabs}
    >
      {activeTab === "sso" ? <SingleSignOnTab /> : <EnterpriseManagedAuth />}
    </TabbedPage>
  );
}

function SingleSignOnTab(): JSX.Element {
  const organization = useOrganization();
  const { data: features } = useProductFeatures({
    organizationId: organization.id,
  });
  const { data: onboardingStatus } = useOnboardingStatus(undefined, undefined, {
    throwOnError: false,
  });

  const ssoFeatureEnabled = features?.ssoEnabled ?? false;
  const scimFeatureEnabled = features?.scimEnabled ?? false;
  const ssoActive = organization.ssoEnabled === true;
  const scimActive = organization.scimEnabled === true;
  // Active SSO proves a domain was verified, even for orgs set up before
  // verified domains were tracked. The server completes the setup task the
  // same way.
  const domainVerified =
    !!onboardingStatus?.domainVerified || !!onboardingStatus?.ssoConfigured;
  const verifiedDomains = onboardingStatus?.verifiedDomains ?? [];
  // WorkOS needs a verified domain before either connection can be set up.
  // Only a status response can say the domain is unverified: while loading or
  // after an error nothing is blocked, and WorkOS still enforces the rule.
  const domainUnverified = onboardingStatus !== undefined && !domainVerified;
  const ssoBlocked = !ssoActive && domainUnverified;
  const scimBlocked = !scimActive && domainUnverified;

  let domainSubtitle = "Add a DNS record to verify your domain.";
  if (verifiedDomains.length > 0)
    domainSubtitle = "SSO applies to users on these domains.";
  else if (domainVerified) domainSubtitle = "Your domain is verified.";

  let ssoSubtitle = "Choose an identity provider to get started.";
  if (ssoActive) ssoSubtitle = "Your identity provider is connected.";
  else if (ssoBlocked) ssoSubtitle = "Verify a domain first.";

  let scimSubtitle = "Choose an identity provider to get started.";
  if (scimActive) scimSubtitle = "Your directory provider is connected.";
  else if (scimBlocked) scimSubtitle = "Verify a domain first.";

  return (
    <div className="flex max-w-4xl flex-col gap-8">
      <IdentitySection
        sectionId="domain_verification"
        heading="Domain verification"
        description="Prove your organization owns its email domain. Single Sign-On needs a verified domain."
        providerIcon={<Globe className="text-muted-foreground h-5 w-5" />}
        providerTitle="Domain"
        providerSubtitle={domainSubtitle}
        learnMoreText="Learn more about domain verification"
        learnMoreHref="https://www.speakeasy.com/docs/ai-control-plane/org-admin/identity"
        active={domainVerified}
        activeLabel="Verified"
        configureButton={
          <WorkOSPortalButton
            intent="domain_verification"
            errorFallback="Failed to start domain verification"
            label={domainVerified ? "Manage" : "Verify domain"}
          />
        }
      >
        {verifiedDomains.length > 0 && (
          <ul
            aria-label="Verified domains"
            className="border-border flex flex-wrap gap-2 border-t p-4"
          >
            {verifiedDomains.map((domain) => (
              <li key={domain}>
                <Badge variant="neutral" background>
                  <Badge.Text>{domain}</Badge.Text>
                </Badge>
              </li>
            ))}
          </ul>
        )}
      </IdentitySection>

      <IdentitySection
        sectionId="sso"
        heading="Single Sign-On"
        description="Set up Single Sign-On (SSO) to allow your team to sign in to Speakeasy with your identity provider."
        providerIcon={<Lock className="text-muted-foreground h-5 w-5" />}
        providerTitle="SSO"
        providerSubtitle={ssoSubtitle}
        learnMoreText="Learn more about SSO"
        learnMoreHref="https://www.speakeasy.com/docs/ai-control-plane/org-admin/identity"
        active={ssoActive}
        blocked={ssoBlocked}
        configureButton={
          <SSOConfigureControl
            featureEnabled={ssoFeatureEnabled}
            active={ssoActive}
          />
        }
      />

      <IdentitySection
        sectionId="directory_sync"
        heading="Directory Sync"
        description={
          <>
            Sync members and roles directly from your identity provider:
            <ul className="mt-1.5 list-disc space-y-0.5 pl-5">
              <li>Members are provisioned automatically from your directory</li>
              <li>Roles are assigned from your IDP group mappings</li>
              <li>Members can&apos;t be invited manually</li>
              <li>Roles can&apos;t be assigned to members manually</li>
            </ul>
          </>
        }
        providerIcon={<FolderSync className="text-muted-foreground h-5 w-5" />}
        providerTitle="SCIM"
        providerSubtitle={scimSubtitle}
        learnMoreText="Learn more about SCIM Directory Sync"
        learnMoreHref="https://www.speakeasy.com/docs/ai-control-plane/org-admin/identity"
        active={scimActive}
        blocked={scimBlocked}
        configureButton={
          <DirectorySyncConfigureControl
            featureEnabled={scimFeatureEnabled}
            active={scimActive}
          />
        }
      />
    </div>
  );
}
