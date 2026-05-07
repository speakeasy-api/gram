import { AssetImage } from "@/components/asset-image";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { CompactUpload } from "@/components/upload";
import { useAssetImageUploadHandler } from "@/components/useAssetImageUploadHandler";
import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { useToolsets } from "@/pages/toolsets/useToolsets";
import {
  useListSlackApps,
  useCreateSlackAppMutation,
} from "@gram/client/react-query";
import { SlackAppResult } from "@gram/client/models/components/slackappresult.js";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { Outlet } from "react-router";
import React, { useState } from "react";

export function StatusBadge({
  status,
  installCount,
}: {
  status: string;
  installCount: number;
}) {
  if (status === "active" && installCount > 0) {
    return (
      <Badge variant="default">
        {installCount} installation{installCount !== 1 ? "s" : ""}
      </Badge>
    );
  }
  if (status === "active") {
    return <Badge variant="secondary">Ready</Badge>;
  }
  if (status === "unconfigured") {
    return <Badge variant="secondary">Unconfigured</Badge>;
  }
  return <Badge variant="secondary">{status}</Badge>;
}

export function SlackAppsRoot() {
  return <Outlet />;
}

function SlackAppsEmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name="bot" className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No assistants yet
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        Create an assistant to let your team interact with Gram MCP servers
        directly.
      </Type>
      <Button onClick={onCreate}>
        <Button.LeftIcon>
          <Icon name="plus" className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Create new Assistant</Button.Text>
      </Button>
    </div>
  );
}

function SlackAppCard({ app }: { app: SlackAppResult }) {
  const routes = useRoutes();
  return (
    <routes.slackApps.detail.Link
      params={[app.id]}
      className="no-underline hover:no-underline"
    >
      <div className="bg-card hover:bg-muted/50 rounded-lg border p-4 transition-colors">
        <div className="mb-2 flex items-start justify-between">
          <Type className="truncate font-semibold">{app.name}</Type>
          <StatusBadge
            status={app.status}
            installCount={app.slackTeamId ? 1 : 0}
          />
        </div>
        <div className="space-y-1">
          <Type muted small>
            {app.slackTeamName || "\u2014"}
          </Type>
          <div className="flex items-center justify-between">
            <Type muted small>
              {app.toolsetIds.length} MCP server
              {app.toolsetIds.length !== 1 ? "s" : ""}
            </Type>
            <Type muted small>
              <HumanizeDateTime date={new Date(app.createdAt)} />
            </Type>
          </div>
        </div>
      </div>
    </routes.slackApps.detail.Link>
  );
}

function CreateSlackAppDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const routes = useRoutes();
  const toolsets = useToolsets();
  const createMutation = useCreateSlackAppMutation();

  const [name, setName] = useState("");
  const [iconAssetId, setIconAssetId] = useState<string | null>(null);
  const [selectedToolsetIds, setSelectedToolsetIds] = useState<Set<string>>(
    new Set(),
  );

  const handleIconUpload = useAssetImageUploadHandler((res) => {
    setIconAssetId(res.asset.id);
  });

  const isValid = name.trim().length > 0 && selectedToolsetIds.size > 0;

  const reset = () => {
    setName("");
    setIconAssetId(null);
    setSelectedToolsetIds(new Set());
  };

  const handleOpenChange = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  const toggleToolset = (id: string) => {
    setSelectedToolsetIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const handleCreate = async () => {
    const result = await createMutation.mutateAsync({
      request: {
        createSlackAppRequestBody: {
          name,
          toolsetIds: Array.from(selectedToolsetIds),
          ...(iconAssetId && { iconAssetId }),
        },
      },
    });
    handleOpenChange(false);
    routes.slackApps.detail.goTo(result.app.id);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content className="sm:max-w-xl">
        <Dialog.Header>
          <Dialog.Title>Create Assistant</Dialog.Title>
          <Dialog.Description>
            Name your assistant and choose which MCP servers it can access.
          </Dialog.Description>
        </Dialog.Header>

        <Stack gap={4}>
          <div>
            <Type variant="body" className="mb-1 font-medium">
              App Name
            </Type>
            <Input
              value={name}
              onChange={setName}
              placeholder="My Assistant"
              maxLength={36}
              validate={(v) =>
                v.length > 36 ? "Name must be 36 characters or fewer" : true
              }
            />
          </div>

          <div>
            <Type variant="body" className="mb-2 font-medium">
              Icon
            </Type>
            <CompactUpload
              onUpload={handleIconUpload}
              className="h-24 w-24"
              renderFilePreview={() =>
                iconAssetId ? (
                  <AssetImage
                    assetId={iconAssetId}
                    className="h-16 w-16 rounded-lg"
                  />
                ) : undefined
              }
            />
          </div>

          <div>
            <Type variant="body" className="mb-2 font-medium">
              MCP Servers
            </Type>
            {toolsets.length === 0 ? (
              <Type muted small>
                No MCP servers available. Create an MCP server in this project
                first.
              </Type>
            ) : (
              <div className="grid max-h-60 grid-cols-2 gap-2 overflow-y-auto">
                {toolsets.map((ts) => {
                  const selected = selectedToolsetIds.has(ts.id);
                  return (
                    <button
                      key={ts.id}
                      type="button"
                      onClick={() => toggleToolset(ts.id)}
                      className={cn(
                        "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors",
                        selected
                          ? "border-primary bg-primary/5"
                          : "border-border hover:border-muted-foreground/30",
                      )}
                    >
                      <div
                        className={cn(
                          "mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border",
                          selected
                            ? "border-primary bg-primary text-primary-foreground"
                            : "border-muted-foreground/40",
                        )}
                      >
                        {selected && <Icon name="check" className="h-3 w-3" />}
                      </div>
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
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </Stack>

        <Dialog.Footer>
          <Button variant="tertiary" onClick={() => handleOpenChange(false)}>
            Cancel
          </Button>
          <Button
            onClick={handleCreate}
            disabled={!isValid || createMutation.isPending}
          >
            {createMutation.isPending ? "Creating..." : "Create Assistant"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

export default function SlackAppsIndex() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["mcp:read", "mcp:write"]} level="page">
          <SlackAppsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function SlackAppsInner() {
  const [dialogOpen, setDialogOpen] = useState(false);
  const { data, isLoading } = useListSlackApps(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });

  const apps = data?.items ?? [];

  return (
    <EnterpriseGate
      icon="bot"
      description="Assistants are available on the Enterprise plan. Book a time to get started."
    >
      <Page.Section>
        <Page.Section.Title>Assistants</Page.Section.Title>
        <Page.Section.Description>
          Create and manage assistants that connect your team to Gram MCP
          servers in Slack.
        </Page.Section.Description>
        <Page.Section.CTA>
          {apps.length > 0 && (
            <Button onClick={() => setDialogOpen(true)}>
              <Button.LeftIcon>
                <Icon name="plus" className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Create new Assistant</Button.Text>
            </Button>
          )}
        </Page.Section.CTA>
        <Page.Section.Body>
          {isLoading ? (
            <Stack align="center" justify="center" className="py-16">
              <Icon
                name="loader-circle"
                className="text-muted-foreground h-6 w-6 animate-spin"
              />
            </Stack>
          ) : apps.length === 0 ? (
            <SlackAppsEmptyState onCreate={() => setDialogOpen(true)} />
          ) : (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
              {apps.map((app) => (
                <SlackAppCard key={app.id} app={app} />
              ))}
            </div>
          )}
        </Page.Section.Body>
      </Page.Section>

      <CreateSlackAppDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </EnterpriseGate>
  );
}
