import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { useSendEnterpriseAdminOnboardingEmailMutation } from "@gram/client/react-query/sendEnterpriseAdminOnboardingEmail.js";
import { useState } from "react";
import { toast } from "sonner";
import { AdminSection } from "./AdminSection";
import { PlatformAdminGate } from "./PlatformAdminGate";

export default function PlatformAdminOnboarding(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title area="Platform Admin">
            Onboarding
          </Page.Section.Title>
          <Page.Section.Description>
            Send the enterprise admin setup wizard to new administrators.
          </Page.Section.Description>
          <Page.Section.Body>
            <PlatformAdminGate>
              <OnboardingSection />
            </PlatformAdminGate>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function OnboardingSection(): JSX.Element {
  const [emailsInput, setEmailsInput] = useState("");

  const sendEmail = useSendEnterpriseAdminOnboardingEmailMutation({
    onSuccess: (data) => {
      toast.success(
        `Sent ${data.sentCount} email${data.sentCount === 1 ? "" : "s"}.`,
      );
      setEmailsInput("");
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Failed to send onboarding email",
      );
    },
  });

  const recipients = emailsInput
    .split(",")
    .map((e) => e.trim())
    .filter((e) => e.length > 0);

  const handleSend = () => {
    if (recipients.length === 0) return;
    sendEmail.mutate({
      request: {
        sendEnterpriseAdminOnboardingEmailRequestBody: { recipients },
      },
    });
  };

  return (
    <AdminSection
      title="Enterprise admin onboarding"
      description="Send the setup-wizard link to people you want to onboard as enterprise admins of this organization. Separate multiple addresses with commas."
    >
      <div className="flex flex-col gap-3 px-4 py-3">
        <div className="flex items-center gap-2">
          <Input
            name="onboarding_emails"
            placeholder="alice@example.com, bob@example.com"
            value={emailsInput}
            onChange={setEmailsInput}
            disabled={sendEmail.isPending}
            className="max-w-md"
          />
          <Button
            size="sm"
            onClick={handleSend}
            disabled={recipients.length === 0 || sendEmail.isPending}
          >
            Send to{" "}
            {recipients.length === 0
              ? "0 recipients"
              : `${recipients.length} recipient${recipients.length === 1 ? "" : "s"}`}
          </Button>
        </div>

        {sendEmail.data?.setupLink && (
          <p className="text-muted-foreground text-sm break-all">
            Setup link:{" "}
            <code className="bg-muted px-1 py-0.5 font-mono text-xs">
              {sendEmail.data.setupLink}
            </code>
          </p>
        )}
      </div>
    </AdminSection>
  );
}
