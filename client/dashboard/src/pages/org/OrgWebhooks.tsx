import { Page } from "@/components/page-layout";
import { SettingsLayout } from "@/components/layouts/settings-layout";
import { RequireScope } from "@/components/require-scope";
import { Card } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Switch } from "@/components/ui/switch";
import { useCreatePortalSessionMutation } from "@gram/client/react-query/createPortalSession.js";
import { useDisableWebhooksMutation } from "@gram/client/react-query/disableWebhooks.js";
import { useEnableWebhooksMutation } from "@gram/client/react-query/enableWebhooks.js";
import { useOrganization } from "@gram/client/react-query/organization.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { Stack } from "@/components/ui/stack";
import { Button as MoonshineButton } from "@/components/ui/button";
import { useTheme } from "@/contexts/theme-context";
import { Webhook } from "lucide-react";
import { AppPortal } from "svix-react";
import React, { JSX } from "react";

import "svix-react/style.css";
import { useTelemetry } from "@/contexts/Telemetry";
import { useSessionData } from "@/contexts/Auth";

export default function OrgWebhooks(): React.JSX.Element {
  const { data: features, isLoading } = useProductFeatures();

  let content: JSX.Element | null = null;
  if (isLoading) {
    content = null;
  } else if (features?.webhooks) {
    content = (
      <RequireScope scope={["org:read"]} level="page">
        <OrgWebhooksInner />
      </RequireScope>
    );
  } else {
    content = <WebhooksDisabled />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs stage="preview" />
      </Page.Header>
      <Page.Body>{content}</Page.Body>
    </Page>
  );
}

function OrgWebhooksInner() {
  const orgResult = useOrganization();
  const enableWebhooks = useEnableWebhooksMutation({
    onSettled: () => orgResult.refetch(),
  });
  const disableWebhooks = useDisableWebhooksMutation({
    onSettled: () => orgResult.refetch(),
  });

  const editable =
    orgResult.status === "success" &&
    enableWebhooks.status !== "pending" &&
    disableWebhooks.status !== "pending";

  return (
    <SettingsLayout>
      <SettingsLayout.Header
        title="Webhooks"
        subtitle="Configure webhook delivery for various platform events."
      />
      <SettingsLayout.Body>
        <SettingsLayout.Group
          label="Enable Webhooks"
          description="Enable or disable webhook delivery of organization events. Disabling this option does not destroy existing webhook configuration below."
          actions={
            <RequireScope scope="org:admin" level="component">
              <Switch
                checked={orgResult.data?.webhooksEnabled || false}
                onCheckedChange={function (checked) {
                  if (checked) {
                    enableWebhooks.mutate({});
                  } else {
                    disableWebhooks.mutate({});
                  }
                }}
                disabled={!editable}
                aria-label="Toggle webhooks"
              />
            </RequireScope>
          }
        >
          {orgResult.data?.webhooksOnboarded && <WebhookConfigPortal />}
        </SettingsLayout.Group>
      </SettingsLayout.Body>
    </SettingsLayout>
  );
}

function WebhooksDisabled() {
  const telemetry = useTelemetry();
  const { session } = useSessionData();

  return (
    <Card>
      <Stack gap={4} align="center" justify="center">
        <Webhook className="text-muted-foreground h-10 w-10" />
        <div>
          <Heading variant="h4" className="text-center font-medium">
            Webhooks are currently in preview
          </Heading>
        </div>

        <MoonshineButton variant="brand" asChild>
          <a
            href="https://www.speakeasy.com/book-demo"
            target="_blank"
            rel="noopener noreferrer"
            onClick={() => {
              telemetry.capture("webhooks_interest", {
                action: "webhook_design_partner_clicked",
                email: session?.user.email ?? "",
                organization_id: session?.organization?.id ?? "",
                organization_name: session?.organization?.name ?? "",
                organization_slug: session?.organization?.slug ?? "",
              });
            }}
          >
            Talk to our team
          </a>
        </MoonshineButton>
      </Stack>
    </Card>
  );
}

function WebhookConfigPortal() {
  const { theme: rawTheme } = useTheme();
  const { mutate: createSession } = useCreatePortalSessionMutation();
  const [portalURL, setPortalURL] = React.useState<string | null>(null);
  React.useEffect(() => {
    createSession(
      {},
      {
        onSuccess(data) {
          setPortalURL(data.url);
        },
      },
    );
  }, [createSession]);

  if (!portalURL) {
    return null;
  }

  let theme: boolean | "auto" | undefined = undefined;
  if (rawTheme === "light") {
    theme = false;
  } else if (rawTheme === "dark") {
    theme = true;
  } else {
    theme = "auto";
  }

  return (
    <>
      <Heading variant="h4">Webhook Configuration</Heading>
      <AppPortal
        url={portalURL}
        darkMode={theme}
        style={{
          border: "1px solid var(--border)",
        }}
      />
    </>
  );
}
