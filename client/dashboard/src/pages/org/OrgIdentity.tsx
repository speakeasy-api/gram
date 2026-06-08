import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization, useSessionData } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  useGenerateWorkOSAdminPortalLinkMutation,
  useProductFeatures,
} from "@gram/client/react-query";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { FolderSync, Loader2, Lock } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

const UPSELL_COPY = "Contact our team to setup SSO and Directory Sync";

type IdentitySectionId = "sso" | "directory_sync";

type IdentityCardProps = {
  sectionId: IdentitySectionId;
  heading: string;
  description: string;
  providerIcon: React.ReactNode;
  providerTitle: string;
  providerSubtitle: string;
  learnMoreText: string;
  learnMoreHref: string;
  active?: boolean;
  configureButton?: React.ReactNode;
  children?: React.ReactNode;
};

function useIdentityInterestCapture(sectionId: IdentitySectionId) {
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
  const captureInterest = useIdentityInterestCapture(sectionId);

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

function useDirectorySyncPortalTelemetry() {
  const telemetry = useTelemetry();
  const { session } = useSessionData();

  return () => {
    telemetry.capture("identity_provider_interest", {
      section: "directory_sync",
      action: "dsync_portal_opened",
      email: session?.user.email ?? "",
      organization_id: session?.organization?.id ?? "",
      organization_name: session?.organization?.name ?? "",
      organization_slug: session?.organization?.slug ?? "",
    });
  };
}

function SSOConfigureButton({ skipModal }: { skipModal?: boolean }) {
  const [open, setOpen] = useState(false);
  const captureInterest = useIdentityInterestCapture("sso");

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
          captureInterest();
          window.open(data.url, "_blank", "noopener,noreferrer");
          toast.info("Continue setup in the WorkOS portal");
          setOpen(false);
        },
      },
    );
  };

  return (
    <RequireScope scope="org:admin" level="component">
      <Button
        variant="secondary"
        size="sm"
        onClick={() => (skipModal ? launchPortal() : setOpen(true))}
        disabled={generatePortalLink.isPending}
      >
        {generatePortalLink.isPending && (
          <Button.LeftIcon>
            <Loader2 className="h-4 w-4 animate-spin" />
          </Button.LeftIcon>
        )}
        Configure
      </Button>
      {!skipModal && (
        <Dialog
          open={open}
          onOpenChange={(next) => {
            if (!generatePortalLink.isPending) {
              setOpen(next);
            }
          }}
        >
          <Dialog.Content className="sm:max-w-lg">
            <Dialog.Header>
              <Dialog.Title>Set up Single Sign-On</Dialog.Title>
              <Dialog.Description>
                Connecting SSO lets your team sign in to Speakeasy using your
                identity provider. Once configured, members will authenticate
                through your IdP instead of using email and password.
              </Dialog.Description>
            </Dialog.Header>
            <div className="space-y-3 pt-2">
              <Type muted small>
                You&apos;ll be redirected to configure your identity provider
                connection.
              </Type>
            </div>
            <div className="flex justify-end gap-2 pt-4">
              <Button
                variant="secondary"
                onClick={() => setOpen(false)}
                disabled={generatePortalLink.isPending}
              >
                Cancel
              </Button>
              <Button
                onClick={launchPortal}
                disabled={generatePortalLink.isPending}
              >
                {generatePortalLink.isPending && (
                  <Button.LeftIcon>
                    <Loader2 className="h-4 w-4 animate-spin" />
                  </Button.LeftIcon>
                )}
                <Button.Text>Continue to setup</Button.Text>
              </Button>
            </div>
          </Dialog.Content>
        </Dialog>
      )}
    </RequireScope>
  );
}

function DirectorySyncConfigureButton({ skipModal }: { skipModal?: boolean }) {
  const [open, setOpen] = useState(false);
  const captureOpened = useDirectorySyncPortalTelemetry();

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
          captureOpened();
          window.open(data.url, "_blank", "noopener,noreferrer");
          toast.info("Continue setup in the WorkOS portal");
          setOpen(false);
        },
      },
    );
  };

  return (
    <RequireScope scope="org:admin" level="component">
      <Button
        variant="secondary"
        size="sm"
        onClick={() => (skipModal ? launchPortal() : setOpen(true))}
        disabled={generatePortalLink.isPending}
      >
        {generatePortalLink.isPending && (
          <Button.LeftIcon>
            <Loader2 className="h-4 w-4 animate-spin" />
          </Button.LeftIcon>
        )}
        Configure
      </Button>
      {!skipModal && (
        <Dialog
          open={open}
          onOpenChange={(next) => {
            if (!generatePortalLink.isPending) {
              setOpen(next);
            }
          }}
        >
          <Dialog.Content className="sm:max-w-lg">
            <Dialog.Header>
              <Dialog.Title>Set up Directory Sync</Dialog.Title>
              <Dialog.Description>
                Connecting Directory Sync lets your identity provider manage who
                belongs to this workspace and which role each directory user
                has. You&apos;ll still create roles and grants in Speakeasy, but
                role assignments for synced users will be controlled by your
                identity provider&apos;s groups.
              </Dialog.Description>
            </Dialog.Header>
            <div className="space-y-3 pt-2">
              <Type muted small>
                Members you invite outside of your directory will continue to be
                managed here.
              </Type>
              <Type muted small>
                You&apos;ll be redirected to your identity provider to complete
                setup.
              </Type>
            </div>
            <div className="flex justify-end gap-2 pt-4">
              <Button
                variant="secondary"
                onClick={() => setOpen(false)}
                disabled={generatePortalLink.isPending}
              >
                Cancel
              </Button>
              <Button
                onClick={launchPortal}
                disabled={generatePortalLink.isPending}
              >
                {generatePortalLink.isPending && (
                  <Button.LeftIcon>
                    <Loader2 className="h-4 w-4 animate-spin" />
                  </Button.LeftIcon>
                )}
                <Button.Text>Continue to setup</Button.Text>
              </Button>
            </div>
          </Dialog.Content>
        </Dialog>
      )}
    </RequireScope>
  );
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
        <Type muted small className="mb-4">
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
            ssoFeatureEnabled ? (
              <SSOConfigureButton skipModal={ssoActive} />
            ) : (
              <ConfigureButton sectionId="sso" />
            )
          }
        />

        <IdentitySection
          sectionId="directory_sync"
          heading="Directory Sync"
          description="Automatically provision and deprovision users from your identity provider."
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
            scimFeatureEnabled ? (
              <DirectorySyncConfigureButton skipModal={scimActive} />
            ) : (
              <ConfigureButton sectionId="directory_sync" />
            )
          }
        />
      </div>
    </div>
  );
}
