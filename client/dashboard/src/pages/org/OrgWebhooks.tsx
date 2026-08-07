import { PageEyebrow } from "@/components/page-eyebrow";
import { Page } from "@/components/page-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useCreatePortalSessionMutation } from "@gram/client/react-query/createPortalSession.js";
import { useDisableWebhooksMutation } from "@gram/client/react-query/disableWebhooks.js";
import { useEnableWebhooksMutation } from "@gram/client/react-query/enableWebhooks.js";
import { useOrganization } from "@gram/client/react-query/organization.js";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import { Stack } from "@/components/ui/Stack";
import { Webhook } from "lucide-react";
import { AppPortal } from "svix-react";
import React from "react";

import "svix-react/style.css";

export default function OrgWebhooks(): React.JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read"]} level="page">
          <OrgWebhooksInner />
        </RequireScope>
      </Page.Body>
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
      <div className="mb-6">
        <PageEyebrow className="mb-2" />
        <Stack direction="horizontal" align="center" gap={2}>
          <Heading variant="h4" className="text-display-sm font-thin">
            Webhooks
          </Heading>
          <ReleaseStageBadge stage="beta" />
        </Stack>
        <Text muted small className="mt-1">
          Configure webhook delivery for various platform events.
        </Text>
      </div>
      <div className="border-border bg-card border p-4">
        <Stack gap={4}>
          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <Webhook className="text-muted-foreground h-4 w-4" />
                <Text variant="body" className="font-medium">
                  Enable Webhooks
                </Text>
              </Stack>
              <Text
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Enable or disable webhook delivery of organization events.
                Disabling this option does not destroy existing webhook
                configuration below.
              </Text>
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
      <Heading variant="h4" className="mt-8 mb-4 text-display-xs font-thin">
        Webhook Configuration
      </Heading>
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
