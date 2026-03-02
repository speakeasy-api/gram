import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { useCommandPalette } from "@/contexts/CommandPalette";
import { sourceTypeToUrnKind, type SourceType } from "@/lib/sources";
import { useRoutes } from "@/routes";
import {
  useLatestDeployment,
  useListAssets,
} from "@gram/client/react-query/index.js";
import { Icon, IconName, Badge } from "@speakeasy-api/moonshine";
import { Plus, Upload, MessageSquare } from "lucide-react";
import { useMemo } from "react";
import { useToolsets } from "@/pages/toolsets/Toolsets";

interface SourceItem {
  name: string;
  slug: string;
  type: SourceType;
}

function useSources(): SourceItem[] {
  const { data: deploymentResult } = useLatestDeployment();
  const { data: assets } = useListAssets();

  return useMemo(() => {
    const deployment = deploymentResult?.deployment;
    if (!deployment) return [];

    const openApiSources: SourceItem[] = assets
      ? deployment.openapiv3Assets
          .filter((da) => assets.assets.some((a) => a.id === da.assetId))
          .map((da) => ({ name: da.name, slug: da.slug, type: "openapi" }))
      : [];

    const functionSources: SourceItem[] = assets
      ? (deployment.functionsAssets ?? [])
          .filter((da) => assets.assets.some((a) => a.id === da.assetId))
          .map((da) => ({ name: da.name, slug: da.slug, type: "function" }))
      : [];

    const externalMcpSources: SourceItem[] = (
      deployment.externalMcps ?? []
    ).map((em) => ({
      name: em.name,
      slug: em.slug,
      type: "externalmcp",
    }));

    return [...openApiSources, ...functionSources, ...externalMcpSources];
  }, [deploymentResult, assets]);
}

export function CommandPalette() {
  const { isOpen, close, actions, contextBadge } = useCommandPalette();
  const routes = useRoutes();
  const toolsets = useToolsets();
  const sources = useSources();

  // Group context-registered actions by their group property
  const groupedActions = actions.reduce(
    (acc, action) => {
      const group = action.group || "Actions";
      if (!acc[group]) {
        acc[group] = [];
      }
      acc[group].push(action);
      return acc;
    },
    {} as Record<string, typeof actions>,
  );

  // Sort groups: Tool Actions first (when present), then others alphabetically
  const sortedGroups = Object.entries(groupedActions).sort(([a], [b]) => {
    if (a === "Tool Actions") return -1;
    if (b === "Tool Actions") return 1;
    return a.localeCompare(b);
  });

  const handleSelect = (action: (typeof actions)[0]) => {
    action.onSelect();
    close();
  };

  return (
    <CommandDialog open={isOpen} onOpenChange={close}>
      {contextBadge && (
        <div className="px-3 pt-3 pb-2">
          <Badge variant="neutral">
            <Badge.Text>{contextBadge}</Badge.Text>
          </Badge>
        </div>
      )}
      <CommandInput placeholder="Type a command or search..." />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>

        {/* Context-registered action groups (Navigation, Tool Actions, etc.) */}
        {sortedGroups.map(([groupName, groupActions]) => (
          <CommandGroup key={groupName} heading={groupName}>
            {groupActions.map((action) => (
              <CommandItem
                key={action.id}
                onSelect={() => handleSelect(action)}
                keywords={action.keywords}
              >
                {action.icon && (
                  <Icon name={action.icon as IconName} className="size-4" />
                )}
                <div className="flex flex-col">
                  <span>{action.label}</span>
                  {action.description && (
                    <span className="text-xs text-muted-foreground">
                      {action.description}
                    </span>
                  )}
                </div>
                {action.shortcut && (
                  <span className="ml-auto text-xs text-muted-foreground">
                    {action.shortcut}
                  </span>
                )}
              </CommandItem>
            ))}
          </CommandGroup>
        ))}

        {/* MCP Servers — live from useToolsets() */}
        {toolsets.length > 0 && (
          <CommandGroup heading="MCP Servers">
            {toolsets.map((toolset) => (
              <CommandItem
                key={`mcp-${toolset.slug}`}
                onSelect={() => {
                  routes.mcp.details.goTo(toolset.slug);
                  close();
                }}
                keywords={[toolset.slug, "mcp", "server", "toolset"]}
              >
                <Icon name="network" className="size-4" />
                <div className="flex flex-col">
                  <span>{toolset.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {toolset.slug}
                    {toolset.tools.length > 0 &&
                      ` · ${toolset.tools.length} tools`}
                  </span>
                </div>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {/* Sources — live from deployment data */}
        {sources.length > 0 && (
          <CommandGroup heading="Sources">
            {sources.map((source) => (
              <CommandItem
                key={`source-${source.type}-${source.slug}`}
                onSelect={() => {
                  routes.sources.source.goTo(
                    sourceTypeToUrnKind(source.type),
                    source.slug,
                  );
                  close();
                }}
                keywords={[source.slug, source.type, "source"]}
              >
                <Icon name="file-code" className="size-4" />
                <div className="flex flex-col">
                  <span>{source.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {source.slug} · {source.type}
                  </span>
                </div>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {/* Quick Actions */}
        <CommandGroup heading="Quick Actions">
          <CommandItem
            onSelect={() => {
              routes.sources.addOpenAPI.goTo();
              close();
            }}
            keywords={["upload", "openapi", "spec", "api"]}
          >
            <Upload className="size-4" />
            <span>Upload OpenAPI</span>
          </CommandItem>
          <CommandItem
            onSelect={() => {
              routes.sources.addFromCatalog.goTo();
              close();
            }}
            keywords={["add", "source", "catalog", "third-party", "mcp"]}
          >
            <Plus className="size-4" />
            <span>Add source from catalog</span>
          </CommandItem>
          <CommandItem
            onSelect={() => {
              routes.playground.goTo();
              close();
            }}
            keywords={["chat", "test", "try", "playground"]}
          >
            <MessageSquare className="size-4" />
            <span>Open Playground</span>
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
