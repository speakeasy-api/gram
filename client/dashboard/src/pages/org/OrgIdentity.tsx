import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { Button } from "@speakeasy-api/moonshine";
import { FolderSync, Lock } from "lucide-react";
import { useRef, useState } from "react";

const CONTACT_SALES_URL = "https://www.speakeasy.com/book-demo";
const UPSELL_COPY =
  "SAML SSO is only available on Enterprise plans. Upgrade to get started.";

type IdentityCardProps = {
  heading: string;
  description: string;
  providerIcon: React.ReactNode;
  providerTitle: string;
  providerSubtitle: string;
  learnMoreText: string;
  learnMoreHref: string;
  children?: React.ReactNode;
};

function ConfigureButton() {
  const [open, setOpen] = useState(false);
  const hideTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);

  const show = () => {
    if (hideTimeout.current) clearTimeout(hideTimeout.current);
    setOpen(true);
  };

  const scheduleHide = () => {
    if (hideTimeout.current) clearTimeout(hideTimeout.current);
    hideTimeout.current = setTimeout(() => setOpen(false), 150);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="secondary"
          size="sm"
          aria-disabled="true"
          className="cursor-not-allowed opacity-60"
          onMouseEnter={show}
          onMouseLeave={scheduleHide}
          onFocus={show}
          onBlur={scheduleHide}
          onClick={(e) => e.preventDefault()}
        >
          Configure
        </Button>
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="end"
        sideOffset={8}
        className="w-72"
        onMouseEnter={show}
        onMouseLeave={scheduleHide}
        onOpenAutoFocus={(e) => e.preventDefault()}
      >
        <div className="flex flex-col items-center gap-3 text-center">
          <Type small>{UPSELL_COPY}</Type>
          <Button variant="brand" size="sm" asChild>
            <a
              href={CONTACT_SALES_URL}
              target="_blank"
              rel="noopener noreferrer"
            >
              Contact sales
            </a>
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function IdentitySection({
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
            <ConfigureButton />
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
        <Page.Header.Title>Identity</Page.Header.Title>
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
      <Heading variant="h4">Security</Heading>
      <div className="flex flex-col gap-6">
        <IdentitySection
          heading="SAML Single Sign-On"
          description="Set up SAML Single Sign-On (SSO) to allow your team to sign in to Speakeasy with your identity provider."
          providerIcon={<Lock className="text-muted-foreground h-5 w-5" />}
          providerTitle="SAML"
          providerSubtitle="Choose an identity provider to get started."
          learnMoreText="Learn more about SAML SSO"
          learnMoreHref="https://www.speakeasy.com/docs"
        >
          <RequireSsoRow />
        </IdentitySection>

        <IdentitySection
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
