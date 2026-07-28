import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization, useSessionData } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useOrgRoutes } from "@/routes";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query/generateWorkOSAdminPortalLink.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { FolderSync, Loader2, Lock } from "lucide-react";
import { toast } from "sonner";

const UPSELL_COPY = "Contact our team to setup SSO and Directory Sync";

type IdentitySectionId = "sso" | "directory_sync";

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
function SetupStepButton({ step }: { step: "connect-idp" | "directory-sync" }) {
  const orgRoutes = useOrgRoutes();

  return (
    <RequireScope scope="org:admin" level="component">
      <orgRoutes.setup.Link queryParams={{ step }}>
        <Button variant="secondary" size="sm">
          Configure
        </Button>
      </orgRoutes.setup.Link>
    </RequireScope>
  );
}

/** Launches the WorkOS admin portal to manage an existing SSO connection. */
function SSOConfigureButton() {
  const generatePortalLink = useGenerateWorkOSAdminPortalLinkMutation({
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to start SSO setup",
      );
    },
  });

  const launchPortal = () => {
    generatePortalLink.mutate(
      {
        request: {
          generateWorkOSAdminPortalLinkRequestBody: {
            intent: "sso",
          },
        },
      },
      {
        onSuccess: (data) => {
          window.open(data.url, "_blank", "noopener,noreferrer");
          toast.info("Continue setup in the WorkOS portal");
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
        Configure
      </Button>
    </RequireScope>
  );
}

/** Launches the WorkOS admin portal to manage an existing Directory Sync link. */
function DirectorySyncConfigureButton() {
  const generatePortalLink = useGenerateWorkOSAdminPortalLinkMutation({
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to start Directory Sync setup",
      );
    },
  });

  const launchPortal = () => {
    generatePortalLink.mutate(
      {
        request: {
          generateWorkOSAdminPortalLinkRequestBody: {
            intent: "dsync",
          },
        },
      },
      {
        onSuccess: (data) => {
          window.open(data.url, "_blank", "noopener,noreferrer");
          toast.info("Continue setup in the WorkOS portal");
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
        Configure
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
  if (active) return <SSOConfigureButton />;
  return <SetupStepButton step="connect-idp" />;
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
  if (active) return <DirectorySyncConfigureButton />;
  return <SetupStepButton step="directory-sync" />;
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
  configureButton,
  children,
}: IdentityCardProps) {
  return (
    <section>
      <div className="flex flex-col">
        <Heading variant="h5" className="mb-1">
          {heading}
        </Heading>
        <Type as="div" muted small className="mb-4">
          {description}
        </Type>
        <div className="border-border overflow-hidden rounded-lg border">
          <div className="flex items-center gap-4 p-4">
            <div className="bg-muted flex h-10 w-10 shrink-0 items-center justify-center rounded-full">
              {providerIcon}
            </div>
            <div className="min-w-0 flex-1">
              <Type variant="body" className="font-medium">
                {providerTitle}
              </Type>
              <Type muted small>
                {providerSubtitle}
              </Type>
              {active && (
                <Badge variant="success" className="mt-1.5">
                  <Badge.Text>Connected</Badge.Text>
                </Badge>
              )}
            </div>
            {configureButton ?? <ConfigureButton sectionId={sectionId} />}
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
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <OrgIdentityInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function OrgIdentityInner() {
  const organization = useOrganization();
  const { data: features } = useProductFeatures();

  const ssoFeatureEnabled = features?.ssoEnabled ?? false;
  const scimFeatureEnabled = features?.scimEnabled ?? false;
  const ssoActive = organization.ssoEnabled === true;
  const scimActive = organization.scimEnabled === true;

  return (
    <div className="flex flex-col gap-6">
      <Heading variant="h4">Identity</Heading>
      <div className="flex flex-col gap-6">
        <IdentitySection
          sectionId="sso"
          heading="Single Sign-On"
          description="Set up Single Sign-On (SSO) to allow your team to sign in to Speakeasy with your identity provider."
          providerIcon={<Lock className="text-muted-foreground h-5 w-5" />}
          providerTitle="SSO"
          providerSubtitle={
            ssoActive
              ? "Your identity provider is connected."
              : "Choose an identity provider to get started."
          }
          learnMoreText="Learn more about SSO"
          learnMoreHref="https://www.speakeasy.com/docs"
          active={ssoActive}
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
                <li>
                  Members are provisioned automatically from your directory
                </li>
                <li>Roles are assigned from your IDP group mappings</li>
                <li>Members can&apos;t be invited manually</li>
                <li>Roles can&apos;t be assigned to members manually</li>
              </ul>
            </>
          }
          providerIcon={
            <FolderSync className="text-muted-foreground h-5 w-5" />
          }
          providerTitle="SCIM"
          providerSubtitle={
            scimActive
              ? "Your directory provider is connected."
              : "Choose an identity provider to get started."
          }
          learnMoreText="Learn more about SCIM Directory Sync"
          learnMoreHref="https://www.speakeasy.com/docs"
          active={scimActive}
          configureButton={
            <DirectorySyncConfigureControl
              featureEnabled={scimFeatureEnabled}
              active={scimActive}
            />
          }
        />
      </div>
    </div>
  );
}
