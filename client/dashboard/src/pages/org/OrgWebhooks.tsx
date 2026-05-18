import { Page } from "@/components/page-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import {
  useCreatePortalSessionMutation,
  useDisableWebhooksMutation,
  useEnableWebhooksMutation,
  useOrganization,
  useProductFeatures,
} from "@gram/client/react-query";
import {
  Button as MoonshineButton,
  Stack,
  useMoonshineConfig,
} from "@speakeasy-api/moonshine";
import { Webhook } from "lucide-react";
import { AppPortal } from "svix-react";
import React, { JSX } from "react";

import "svix-react/style.css";
import { useTelemetry } from "@/contexts/Telemetry";
import { useSessionData } from "@/contexts/Auth";

export default function OrgWebhooks() {
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
        <Page.Header.Breadcrumbs />
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
    <>
      <Stack direction="horizontal" gap={2} align="center" className="mb-4">
        <Heading variant="h3">Webhooks</Heading>
        <ReleaseStageBadge stage="preview" />
      </Stack>
      <Type muted small className="mb-6">
        Configure webhook delivery for various platform events.
      </Type>
      <div className="border-border bg-card rounded-lg border p-4">
        <Stack gap={4}>
          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <Webhook className="text-muted-foreground h-4 w-4" />
                <Type variant="body" className="font-medium">
                  Enable Webhooks
                </Type>
              </Stack>
              <Type
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Enable or disable webhook delivery of organization events.
                Disabling this option does not destroy existing webhook
                configuration below.
              </Type>
            </Stack>
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
          </Stack>
        </Stack>
      </div>
      {orgResult.data?.webhooksOnboarded && <WebhookConfigPortal />}
    </>
  );
}

function WebhooksDisabled() {
  const telemetry = useTelemetry();
  const { session } = useSessionData();

  return (
    <div className="border-border bg-card rounded-lg border p-4">
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
    </div>
  );
}

function WebhookConfigPortal() {
  const { theme: rawTheme } = useMoonshineConfig();
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
