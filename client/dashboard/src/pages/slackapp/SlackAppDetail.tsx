import { AssetImage } from "@/components/asset-image";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import {
  useGetSlackApp,
  useConfigureSlackAppMutation,
  useUpdateSlackAppMutation,
  useDeleteSlackAppMutation,
  invalidateGetSlackApp,
  invalidateAllListSlackApps,
} from "@gram/client/react-query";
import { SlackAppResult } from "@gram/client/models/components/slackappresult.js";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router";
import { useRoutes } from "@/routes";
import { useToolsets } from "@/pages/toolsets/useToolsets";
import { StatusBadge } from "./SlackApp";
import { buildDeepLinkUrl, buildInviteUrl } from "./slackManifest";

export default function SlackAppDetailPage() {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const { slackAppId } = useParams<{ slackAppId: string }>();
  const { data: app, isLoading } = useGetSlackApp(
    { id: slackAppId! },
    undefined,
    { enabled: !!slackAppId },
  );
  const deleteMutation = useDeleteSlackAppMutation();
  const [confirmDelete, setConfirmDelete] = useState(false);

  const handleDelete = async () => {
    if (!slackAppId) return;
    await deleteMutation.mutateAsync({ request: { id: slackAppId } });
    invalidateAllListSlackApps(queryClient);
    routes.slackApps.goTo();
  };

  if (isLoading || !app) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Stack align="center" justify="center" className="py-16">
            <Icon
              name="loader-circle"
              className="text-muted-foreground h-6 w-6 animate-spin"
            />
          </Stack>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>
            <span className="flex items-center gap-3">
              {app.iconAssetId && (
                <AssetImage
                  assetId={app.iconAssetId}
                  className="h-8 w-8 rounded-lg"
                />
              )}
              {app.name}
              <StatusBadge
                status={app.status}
                installCount={app.slackTeamId ? 1 : 0}
              />
            </span>
          </Page.Section.Title>
          <Page.Section.MoreActions
            actions={[
              {
                icon: "trash-2",
                label: "Delete App",
                onClick: () => setConfirmDelete(true),
                destructive: true,
              },
            ]}
          />
          <Page.Section.Body>
            {app.status === "unconfigured" ? (
              <DraftState app={app} />
            ) : (
              <div className="grid grid-cols-1 gap-8 lg:grid-cols-2">
                <LeftPanel app={app} />
                <RightPanel />
              </div>
            )}
          </Page.Section.Body>
        </Page.Section>

        <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
          <Dialog.Content className="sm:max-w-md">
            <Dialog.Header>
              <Dialog.Title>Delete Assistant</Dialog.Title>
              <Dialog.Description>
                Are you sure you want to delete <strong>{app.name}</strong>?
                This action cannot be undone.
              </Dialog.Description>
            </Dialog.Header>
            <Dialog.Footer>
              <Button
                variant="tertiary"
                onClick={() => setConfirmDelete(false)}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleDelete}
                disabled={deleteMutation.isPending}
              >
                {deleteMutation.isPending ? "Deleting..." : "Delete Assistant"}
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}

function DraftState({ app }: { app: SlackAppResult }) {
  const queryClient = useQueryClient();
  const configureMutation = useConfigureSlackAppMutation();

  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [signingSecret, setSigningSecret] = useState("");

  const deepLinkUrl = buildDeepLinkUrl(app);
  const isValid =
    clientId.trim().length > 0 &&
    clientSecret.trim().length > 0 &&
    signingSecret.trim().length > 0;

  const handleConfigure = async () => {
    await configureMutation.mutateAsync({
      request: {
        id: app.id,
        slackClientId: clientId,
        slackClientSecret: clientSecret,
        slackSigningSecret: signingSecret,
      },
    });
    invalidateGetSlackApp(queryClient, [{ id: app.id }]);
    invalidateAllListSlackApps(queryClient);
  };

  return (
    <Stack gap={6} className="max-w-xl">
      <div>
        <Type variant="body" className="mb-2 font-medium">
          Step 1: Create the app in Slack
        </Type>
        <Type muted small className="mb-3 block">
          Click the button below to open Slack with a pre-filled manifest for
          your app.
        </Type>
        <Button
          variant="secondary"
          onClick={() => window.open(deepLinkUrl, "_blank")}
        >
          <Button.LeftIcon>
            <Icon name="external-link" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Open in Slack</Button.Text>
        </Button>
      </div>

      <div>
        <Type variant="body" className="mb-2 font-medium">
          Step 2: Paste your credentials
        </Type>
        <Type muted small className="mb-3 block">
          After creating the app in Slack, copy the credentials from the app's
          Basic Information page.
        </Type>
        <Stack gap={3}>
          <div>
            <Type variant="body" className="mb-1 text-sm font-medium">
              Client ID
            </Type>
            <Input
              value={clientId}
              onChange={setClientId}
              placeholder="Paste your Client ID"
            />
          </div>
          <div>
            <Type variant="body" className="mb-1 text-sm font-medium">
              Client Secret
            </Type>
            <Input
              type="password"
              value={clientSecret}
              onChange={setClientSecret}
              placeholder="Paste your Client Secret"
            />
          </div>
          <div>
            <Type variant="body" className="mb-1 text-sm font-medium">
              Signing Secret
            </Type>
            <Input
              type="password"
              value={signingSecret}
              onChange={setSigningSecret}
              placeholder="Paste your Signing Secret"
            />
          </div>
        </Stack>
      </div>

      <div>
        <Button
          onClick={handleConfigure}
          disabled={!isValid || configureMutation.isPending}
        >
          {configureMutation.isPending ? "Configuring..." : "Configure"}
        </Button>
      </div>
    </Stack>
  );
}

function LeftPanel({ app }: { app: SlackAppResult }) {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const updateMutation = useUpdateSlackAppMutation();
  const [copied, setCopied] = useState(false);
  const [systemPrompt, setSystemPrompt] = useState(app.systemPrompt ?? "");
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const allToolsets = useToolsets();

  const inviteUrl = buildInviteUrl(
    app,
    app.slackClientId ?? "",
    window.location.href,
  );

  const handleCopyInviteLink = async () => {
    await navigator.clipboard.writeText(inviteUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const appToolsets = allToolsets.filter((ts) =>
    app.toolsetIds.includes(ts.id),
  );

  const saveSystemPrompt = useCallback(
    (value: string) => {
      updateMutation.mutate(
        {
          request: {
            id: app.id,
            systemPrompt: value,
          },
        },
        {
          onSuccess: () => {
            invalidateGetSlackApp(queryClient, [{ id: app.id }]);
            invalidateAllListSlackApps(queryClient);
          },
        },
      );
    },
    [app.id, queryClient, updateMutation],
  );

  const handlePromptChange = (value: string) => {
    setSystemPrompt(value);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      saveSystemPrompt(value);
    }, 800);
  };

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  return (
    <Stack gap={6}>
      {/* Actions */}
      <Stack direction="horizontal" gap={2}>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => window.open("https://api.slack.com/apps", "_blank")}
        >
          <Button.LeftIcon>
            <Icon name="external-link" className="h-3.5 w-3.5" />
          </Button.LeftIcon>
          <Button.Text>Manage on Slack</Button.Text>
        </Button>
        <Button variant="secondary" size="sm" onClick={handleCopyInviteLink}>
          <Button.LeftIcon>
            <Icon name={copied ? "check" : "copy"} className="h-3.5 w-3.5" />
          </Button.LeftIcon>
          <Button.Text>{copied ? "Copied!" : "Copy Invite Link"}</Button.Text>
        </Button>
      </Stack>

      {/* MCPs */}
      <Stack gap={3}>
        <Type variant="body" className="font-medium">
          MCPs
        </Type>
        {appToolsets.length === 0 ? (
          <Type muted small>
            No MCPs attached to this app.
          </Type>
        ) : (
          <div className="space-y-2">
            {appToolsets.map((ts) => (
              <div
                key={ts.id}
                className="bg-card flex items-center justify-between rounded-lg border p-3"
              >
                <div className="min-w-0">
                  <Type className="block truncate font-medium">
                    {ts.name || ts.slug}
                  </Type>
                  <Type muted small className="block truncate">
                    {ts.slug}
                    {ts.tools
                      ? ` \u00B7 ${ts.tools.length} tool${ts.tools.length !== 1 ? "s" : ""}`
                      : ""}
                  </Type>
                </div>
                <routes.mcp.details.Link
                  params={[ts.slug]}
                  className="no-underline hover:no-underline"
                >
                  <Button variant="tertiary" size="sm">
                    <Button.LeftIcon>
                      <Icon name="external-link" className="h-3.5 w-3.5" />
                    </Button.LeftIcon>
                    <Button.Text>View</Button.Text>
                  </Button>
                </routes.mcp.details.Link>
              </div>
            ))}
          </div>
        )}
      </Stack>

      {/* Installations */}
      <Stack gap={3}>
        <Type variant="body" className="font-medium">
          Installations
        </Type>
        {app.slackTeamId ? (
          <div className="bg-card flex items-center justify-between rounded-lg border p-3">
            <div className="min-w-0">
              <Type className="block truncate font-medium">
                {app.slackTeamName || app.slackTeamId}
              </Type>
              <Type muted small className="block truncate">
                {app.slackTeamId}
              </Type>
            </div>
            <Button variant="tertiary" size="sm" onClick={() => {}}>
              <Button.LeftIcon>
                <Icon name="scroll-text" className="h-3.5 w-3.5" />
              </Button.LeftIcon>
              <Button.Text>Logs</Button.Text>
            </Button>
          </div>
        ) : (
          <div className="flex flex-col items-center justify-center rounded-lg border border-dashed px-6 py-10">
            <Icon name="slack" className="text-muted-foreground mb-3 h-6 w-6" />
            <Type muted small className="mb-3 text-center">
              No installs yet. Share the invite link to get your first workspace
              connected.
            </Type>
            <Button
              variant="secondary"
              size="sm"
              onClick={handleCopyInviteLink}
            >
              <Button.LeftIcon>
                <Icon
                  name={copied ? "check" : "copy"}
                  className="h-3.5 w-3.5"
                />
              </Button.LeftIcon>
              <Button.Text>
                {copied ? "Copied!" : "Copy Invite Link"}
              </Button.Text>
            </Button>
          </div>
        )}
      </Stack>

      {/* System Prompt */}
      <Stack gap={3}>
        <Type variant="body" className="font-medium">
          System Prompt
        </Type>
        <Type muted small className="block">
          Instructions that guide how the bot responds in Slack conversations.
        </Type>
        <textarea
          value={systemPrompt}
          onChange={(e) => handlePromptChange(e.target.value)}
          placeholder="You are a helpful assistant..."
          rows={6}
          className="placeholder:text-muted-foreground focus-visible:ring-ring w-full resize-y rounded-lg border bg-transparent px-3 py-2 text-sm focus-visible:ring-1 focus-visible:outline-none"
        />
      </Stack>
    </Stack>
  );
}

function RightPanel() {
  return (
    <Stack gap={3}>
      <Type variant="body" className="font-medium">
        Chat Logs
      </Type>
      <div className="border-warning/40 bg-warning/5 flex flex-col items-center justify-center rounded-lg border border-dashed px-6 py-16">
        <Icon name="triangle-alert" className="text-warning mb-3 h-6 w-6" />
        <Type small className="text-warning text-center">
          I need to be wired up before we remove the feature flag
        </Type>
      </div>
    </Stack>
  );
}
