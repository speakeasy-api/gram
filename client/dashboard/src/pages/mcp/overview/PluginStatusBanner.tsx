import { StatusDot } from "@/components/ui/StatusDot";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { Checkbox } from "@/components/ui/Checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import { useAddPluginServerMutation } from "@gram/client/react-query/addPluginServer";
import {
  invalidateAllPlugins,
  usePlugins,
} from "@gram/client/react-query/plugins";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import {
  invalidateAllPublishStatus,
  usePublishStatus,
} from "@gram/client/react-query/publishStatus";
import { useRemovePluginServerMutation } from "@gram/client/react-query/removePluginServer";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import type { HostedServerRef } from "./MCPOverviewTab";
import { serverMatchesRef } from "./serverMatchesRef";
import { Spinner } from "@/components/ui/Spinner";
import { SettingsSection } from "@/components/detail/settings-section";

function summarizePluginSelection(
  selectedIds: string[],
  plugins: Plugin[],
): string {
  if (selectedIds.length === 0) return "No plugins selected";
  const firstName =
    plugins.find((plugin) => plugin.id === selectedIds[0])?.name ??
    selectedIds[0];
  if (selectedIds.length === 1) return firstName ?? "1 plugin";
  return `${firstName} & ${selectedIds.length - 1} more`;
}

function describeSaveResult(addedCount: number, removedCount: number): string {
  if (addedCount > 0 && removedCount > 0) {
    return "Updated plugin membership and published to GitHub";
  }
  if (removedCount > 0) {
    return removedCount > 1
      ? `Removed from ${removedCount} plugins and published to GitHub`
      : "Removed from plugin and published to GitHub";
  }
  return addedCount > 1
    ? `Added to ${addedCount} plugins and published to GitHub`
    : "Added to plugin and published to GitHub";
}

/** The Plugins section on an MCP server's Settings tab. */
export function PluginStatusBanner({
  server,
}: {
  server: HostedServerRef;
}): React.JSX.Element | null {
  const canWritePlugins = usePluginWriteAccess();
  return canWritePlugins ? (
    <WritablePluginStatusBanner server={server} />
  ) : null;
}

function WritablePluginStatusBanner({
  server,
}: {
  server: HostedServerRef;
}): React.JSX.Element | null {
  const scope = usePluginQueryScope();
  const routes = useRoutes();
  const queryClient = useQueryClient();
  // throwOnError: false — this query calls EnsureDefaultPlugin server-side,
  // which self-heals most conflicts but can still fail; a section on the MCP
  // Settings tab shouldn't crash the whole page via the error boundary when
  // it does. The existing `if (!data) return null` below already degrades
  // gracefully on a failed fetch.
  const { data } = usePlugins(scope, undefined, {
    throwOnError: false,
  });
  // Polled so the section picks up the Temporal generator-rollout schedule's
  // auto-sync without a manual refresh. The poll is silent: a spinner keyed
  // to it would reappear every 5s beside the status and never settle. Saving
  // shows its own spinner on the Publish button.
  const { data: publishStatus } = usePublishStatus(scope, undefined, {
    refetchInterval: 5_000,
  });
  const [selectedPluginIds, setSelectedPluginIds] = useState<string[]>([]);
  const [isPickerOpen, setIsPickerOpen] = useState(false);

  const addServerMutation = useAddPluginServerMutation();
  const removeServerMutation = useRemovePluginServerMutation();
  const publishMutation = usePublishPluginsMutation();

  // Sort+join so the effect only re-fires when membership actually changes,
  // not on every unrelated `data` refetch.
  const memberKey = (data?.plugins ?? [])
    .filter((plugin) =>
      plugin.servers?.some((s) => serverMatchesRef(s, server)),
    )
    .map((plugin) => plugin.id)
    .sort()
    .join(",");

  // Seed the picker's staged selection from committed membership on load and
  // whenever it changes underneath us (e.g. after a successful save). Not
  // tied to isPickerOpen — losing focus/closing the popover must not discard
  // an in-progress, unsaved selection.
  useEffect(() => {
    setSelectedPluginIds(memberKey ? memberKey.split(",") : []);
  }, [memberKey]);

  // Don't flash the "not published" warning while plugins are still loading.
  if (!data) return null;

  const plugins = data.plugins;
  const memberPlugins = plugins.filter((plugin) =>
    plugin.servers?.some((s) => serverMatchesRef(s, server)),
  );
  const isPublished = memberPlugins.length > 0;
  // A repo existing isn't enough — a marketplace with no active collaborator
  // isn't discoverable in Claude/Codex/etc, so it isn't truly set up yet.
  // Mirrors the connected/hasCollaborators gate on the Plugins page.
  const marketplaceReady = !!(
    publishStatus?.repoOwner &&
    publishStatus.repoName &&
    publishStatus.hasCollaborators !== false
  );
  // "Published" means a teammate can actually install it — server
  // membership in a plugin alone isn't enough if the marketplace repo
  // itself was never set up on GitHub.
  const isTrulyPublished = isPublished && marketplaceReady;
  const memberIdSet = new Set(memberPlugins.map((plugin) => plugin.id));
  const selectedIdSet = new Set(selectedPluginIds);
  const hasChanges =
    selectedPluginIds.some((id) => !memberIdSet.has(id)) ||
    memberPlugins.some((plugin) => !selectedIdSet.has(plugin.id));
  const isSaving =
    addServerMutation.isPending ||
    removeServerMutation.isPending ||
    publishMutation.isPending;

  const togglePlugin = (pluginId: string) => {
    setSelectedPluginIds((prev) =>
      prev.includes(pluginId)
        ? prev.filter((id) => id !== pluginId)
        : [...prev, pluginId],
    );
  };

  const handleSave = async () => {
    const toAdd = selectedPluginIds.filter((id) => !memberIdSet.has(id));
    const toRemove = memberPlugins.filter(
      (plugin) => !selectedIdSet.has(plugin.id),
    );
    if (toAdd.length === 0 && toRemove.length === 0) return;
    try {
      await Promise.all([
        ...toAdd.map((pluginId) =>
          addServerMutation.mutateAsync({
            security: { sessionHeaderGramSession: "" },
            request: {
              addPluginServerForm: {
                pluginId,
                ...(server.kind === "toolset"
                  ? { toolsetId: server.id }
                  : { mcpServerId: server.id }),
                policy: "required",
              },
            },
          }),
        ),
        ...toRemove.map((plugin) => {
          const serverId = plugin.servers?.find((s) =>
            serverMatchesRef(s, server),
          )?.id;
          if (!serverId) return Promise.resolve();
          return removeServerMutation.mutateAsync({
            security: { sessionHeaderGramSession: "" },
            request: { id: serverId, pluginId: plugin.id },
          });
        }),
      ]);
      const publishResult = await publishMutation.mutateAsync({
        security: { sessionHeaderGramSession: "" },
        request: {
          publishPluginsRequestBody: { githubUsernames: [] },
        },
      });
      await Promise.all([
        invalidateAllPlugins(queryClient),
        invalidateAllPublishStatus(queryClient),
      ]);
      toast.success(describeSaveResult(toAdd.length, toRemove.length), {
        description: (
          <a
            href={publishResult.repoUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="underline underline-offset-2"
          >
            {publishResult.repoUrl}
          </a>
        ),
      });
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update plugin membership",
      );
    }
  };

  let saveButtonText = "Publish";
  if (isPublished) saveButtonText = "Update";
  if (isSaving) saveButtonText = "Publishing";

  // Alphabetical, so a plugin is found where its name says it should be.
  const sortedPlugins = [...plugins].sort((a, b) =>
    a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
  );
  const picker = (
    <Popover open={isPickerOpen} onOpenChange={setIsPickerOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="Plugins"
          className="border-input bg-background hover:bg-muted flex h-9 w-full max-w-md min-w-0 items-center justify-between gap-2 border px-3 text-sm shadow-xs outline-none"
        >
          <span
            className={cn(
              "truncate",
              selectedPluginIds.length === 0 && "text-muted-foreground",
            )}
          >
            {summarizePluginSelection(selectedPluginIds, plugins)}
          </span>
          <ChevronDown className="text-muted-foreground size-4 shrink-0" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 p-1">
        <div className="flex max-h-56 flex-col gap-0.5 overflow-y-auto">
          {sortedPlugins.map((plugin) => (
            <label
              key={plugin.id}
              className="hover:bg-accent flex cursor-pointer items-start gap-2 px-2 py-1.5 text-sm"
            >
              <Checkbox
                checked={selectedPluginIds.includes(plugin.id)}
                onCheckedChange={() => togglePlugin(plugin.id)}
                className="mt-0.5"
              />
              {/* The checkbox lines up with the name; the description, when
                  there is one, sits under it. */}
              <div className="flex min-w-0 flex-col">
                <span>{plugin.name}</span>
                {plugin.description && (
                  <span className="text-muted-foreground text-xs">
                    {plugin.description}
                  </span>
                )}
              </div>
            </label>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );

  const publishButton = (
    <Button
      size="md"
      variant="primary"
      disabled={!hasChanges || isSaving}
      onClick={() => void handleSave()}
    >
      {isSaving && (
        <Button.LeftIcon>
          <Spinner />
        </Button.LeftIcon>
      )}
      <Button.Text>{saveButtonText}</Button.Text>
    </Button>
  );

  const statusLabel = isTrulyPublished
    ? `In ${memberPlugins.length} plugin${memberPlugins.length > 1 ? "s" : ""}`
    : isPublished
      ? "Marketplace needs setup"
      : "Not in a plugin";

  // Plugins are optional, so a server in none is neutral; one in a plugin
  // nobody can install yet is the case worth a warning.
  const statusTone = isTrulyPublished
    ? "success"
    : isPublished
      ? "warning"
      : "neutral";

  const statusHint = isTrulyPublished
    ? memberPlugins.map((plugin) => plugin.name).join(", ")
    : isPublished && !marketplaceReady
      ? "Set up the marketplace on the Plugins page to make it installable."
      : "Add it to a plugin so your team can install it.";

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Plugins</SettingsSection.Title>
        <SettingsSection.Description>
          Choose which plugins distribute this server to your team&apos;s
          agents.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="space-y-0.5">
            <div className="flex items-center gap-2 text-sm font-medium">
              <StatusDot tone={statusTone} />
              {statusLabel}
            </div>
            <Text muted small className="block max-w-2xl pl-4">
              {statusHint}
            </Text>
          </div>
          {plugins.length > 0 ? (
            picker
          ) : (
            <Text muted small className="block">
              This project has no plugins yet.{" "}
              <routes.plugins.Link className="text-foreground underline underline-offset-2">
                Create one on the Plugins page
              </routes.plugins.Link>
              .
            </Text>
          )}
        </SettingsSection.Body>
        {plugins.length > 0 ? (
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              Publishing pushes your plugins to your GitHub marketplace.
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              {publishButton}
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        ) : null}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
