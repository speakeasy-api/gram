import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useSessionData } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { Button } from "@speakeasy-api/moonshine";
import { FolderSync, Lock } from "lucide-react";
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

function IdentitySection({
  sectionId,
  heading,
  description,
  providerIcon,
  providerTitle,
  providerSubtitle,
  learnMoreText,
  learnMoreHref,
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
            </div>
            <ConfigureButton sectionId={sectionId} />
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

function RequireSsoRow() {
  const [requireSso, setRequireSso] = useState(false);
  return (
    <div className="border-border bg-muted/30 flex items-center justify-between gap-4 border-t p-4">
      <Type variant="body" className="font-medium">
        Require workspace members to login with SSO to access this workspace
      </Type>
      <Switch
        checked={requireSso}
        onCheckedChange={setRequireSso}
        disabled
        aria-label="Require workspace members to login with SSO"
      />
    </div>
  );
}

export default function OrgIdentity() {
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
          providerSubtitle="Choose an identity provider to get started."
          learnMoreText="Learn more about SSO"
          learnMoreHref="https://www.speakeasy.com/docs"
        >
          <RequireSsoRow />
        </IdentitySection>

        <IdentitySection
          sectionId="directory_sync"
          heading="Directory Sync"
          description="Automatically provision and deprovision users from your identity provider."
          providerIcon={
            <FolderSync className="text-muted-foreground h-5 w-5" />
          }
          providerTitle="SCIM"
          providerSubtitle="Choose an identity provider to get started."
          learnMoreText="Learn more about SCIM Directory Sync"
          learnMoreHref="https://www.speakeasy.com/docs"
        />
      </div>
    </div>
  );
}
