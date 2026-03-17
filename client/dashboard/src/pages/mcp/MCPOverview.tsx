import { InputDialog } from "@/components/input-dialog";
import { BuiltInMCPCard } from "@/components/mcp/BuiltInMCPCard";
import { MCPCard } from "@/components/mcp/MCPCard";
import { MCPTableRow } from "@/components/mcp/MCPTableRow";
import { Page } from "@/components/page-layout";
import { DotCard } from "@/components/ui/dot-card";
import { DotTable } from "@/components/ui/dot-table";
import { Type } from "@/components/ui/type";
import { ViewToggle, useViewMode } from "@/components/ui/view-toggle";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useIsProjectEmpty } from "@/pages/onboarding/UploadOpenAPI";
import { InitialChoiceStep } from "@/pages/onboarding/Wizard";
import { useRoutes } from "@/routes";
import { MCPRegistry } from "@gram/client/models/components";
import { useListMCPRegistries } from "@gram/client/react-query";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Layers, Network, Plus, Wrench } from "lucide-react";
import { useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { toast } from "sonner";
import { useToolsets } from "../toolsets/Toolsets";
import { MCPEmptyState } from "./MCPEmptyState";

const BUILT_IN_SERVERS = [
  {
    name: "MCP Logs",
    description:
      "Search and analyze your project's MCP server logs, tool calls, and chat sessions.",
    slug: "logs",
  },
];

function CollectionCard({ registry }: { registry: MCPRegistry }) {
  const [open, setOpen] = useState(false);
  const client = useSdkClient();
  const routes = useRoutes();

  const { data: serversData } = useQuery({
    queryKey: ["serveMCPRegistry", registry.slug],
    queryFn: () => client.mcpRegistries.serve({ registrySlug: registry.slug! }),
    enabled: !!registry.slug,
  });

  const servers = serversData?.servers ?? [];
  const pileCount = Math.min(servers.length || 2, 3);

  return (
    <div className={`select-none ${open ? "col-span-full" : ""}`}>
      <div
        className="relative cursor-pointer"
        onClick={() => setOpen((prev) => !prev)}
        style={{
          paddingBottom: open ? 0 : `${pileCount * 5}px`,
          transition: "padding-bottom 0.4s ease",
        }}
      >
        {/* Stacked card shadows behind */}
        {Array.from({ length: pileCount }, (_, i) => (
          <div
            key={i}
            className="absolute pointer-events-none"
            style={{
              left: `${(i + 1) * 4}px`,
              right: `${(i + 1) * 4}px`,
              top: `${(i + 1) * 5}px`,
              bottom: `-${(i + 1) * 5}px`,
              zIndex: 3 - i,
              opacity: open ? 0 : Math.max(0.4, 1 - i * 0.25),
              transition: open
                ? "opacity 0.2s ease"
                : `opacity 0.3s ease ${i * 40}ms`,
            }}
          >
            <div className="h-full rounded-xl border border-foreground/15 bg-card shadow-[0_1px_3px_rgba(0,0,0,0.08)]" />
          </div>
        ))}

        <div
          className="relative bg-card rounded-xl border !border-foreground/10 overflow-hidden hover:!border-foreground/30 hover:shadow-md transition-all shadow-lg"
          style={{ zIndex: 10 }}
        >
          {/* Header row — dot sidebar + info */}
          <div className="flex flex-row min-h-[156px]">
            {/* Dot pattern sidebar */}
            <div className="w-40 shrink-0 overflow-hidden border-r relative bg-muted/30 text-muted-foreground/20 isolate">
              <div
                className="absolute inset-0 z-0"
                style={{
                  backgroundImage:
                    "radial-gradient(circle, currentColor 1px, transparent 1px)",
                  backgroundSize: "16px 16px",
                }}
              />
              <div className="absolute inset-0 z-10 flex items-center justify-center">
                <div className="bg-background/90 backdrop-blur-sm rounded-lg p-3 shadow-lg">
                  <Layers className="w-8 h-8 text-muted-foreground" />
                </div>
              </div>
            </div>

            {/* Content */}
            <div className="p-4 flex flex-col flex-1 min-w-0">
              <div className="flex items-start justify-between gap-2 mb-2">
                <Type
                  variant="subheading"
                  as="div"
                  className="truncate flex-1 text-md group-hover:text-primary transition-colors"
                >
                  {registry.name}
                </Type>
                {servers.length > 0 && (
                  <Badge variant="information">
                    <Badge.Text>
                      {servers.length} server
                      {servers.length !== 1 ? "s" : ""}
                    </Badge.Text>
                  </Badge>
                )}
              </div>
              {registry.slug && (
                <Type small muted className="font-mono text-xs opacity-60">
                  {registry.slug}
                </Type>
              )}
              <div className="flex items-center justify-between gap-2 mt-auto pt-2">
                <div className="flex items-center gap-1.5 text-muted-foreground">
                  <Wrench className="w-3.5 h-3.5" />
                  <Type variant="small" muted>
                    {servers.reduce(
                      (sum, s) => sum + (s.tools?.length ?? 0),
                      0,
                    )}{" "}
                    tools
                  </Type>
                </div>
                <div className="flex items-center gap-1 text-muted-foreground group-hover:text-primary transition-colors text-sm">
                  <span>{open ? "Collapse" : "Expand"}</span>
                  <ChevronRight
                    className={`w-3.5 h-3.5 transition-transform duration-300 ${open ? "rotate-90" : ""}`}
                  />
                </div>
              </div>
            </div>
          </div>

          {/* Expanded servers — inside the card */}
          {open && (
            <div className="border-t border-border/50 bg-muted/20 p-4">
              <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
                {servers.map((server, i) => (
                  <div
                    key={server.registrySpecifier}
                    className="cursor-pointer"
                    onClick={(e) => {
                      e.stopPropagation();
                      if (server.registrySpecifier) {
                        routes.mcp.details.goTo(server.registrySpecifier);
                      }
                    }}
                    style={{
                      animation: `collection-server-in 0.35s cubic-bezier(0.175, 0.885, 0.32, 1.275) ${i * 60}ms both`,
                    }}
                  >
                    <DotCard
                      icon={
                        server.iconUrl ? (
                          <img
                            src={server.iconUrl}
                            alt={server.title ?? ""}
                            className="w-6 h-6 object-contain"
                          />
                        ) : (
                          <Network className="w-6 h-6 text-muted-foreground" />
                        )
                      }
                    >
                      <div className="flex items-start justify-between gap-2 mb-2">
                        <Type
                          variant="subheading"
                          as="div"
                          className="truncate flex-1 text-md"
                        >
                          {server.title || server.registrySpecifier}
                        </Type>
                      </div>
                      {server.description && (
                        <Type variant="small" muted className="line-clamp-2">
                          {server.description}
                        </Type>
                      )}
                      <div className="flex items-center gap-1 text-muted-foreground mt-auto pt-2">
                        <Wrench className="w-3.5 h-3.5" />
                        <Type variant="small" muted>
                          {server.tools?.length ?? 0} tools
                        </Type>
                      </div>
                    </DotCard>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export function MCPRoot() {
  return <Outlet />;
}

export function MCPOverview() {
  const toolsets = useToolsets();
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();
  const { isEmpty: isProjectEmpty, isLoading: isProjectLoading } =
    useIsProjectEmpty();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;
  const [viewMode, setViewMode] = useViewMode();
  const [newMcpDialogOpen, setNewMcpDialogOpen] = useState(false);
  const [newMcpServerName, setNewMcpServerName] = useState("");

  const { data: registriesData } = useListMCPRegistries();
  const collections = (registriesData?.registries ?? []).filter(
    (r) => r.source === "internal",
  );

  const handleCreateMcpServerSubmit = async () => {
    const result = await client.toolsets.create({
      createToolsetRequestBody: {
        name: newMcpServerName,
      },
    });

    toast.success(`MCP server "${result.name}" created`);

    navigate(routes.mcp.details.href(result.slug) + "#tools");
  };

  const newMcpServerButton = (
    <Button size="sm" onClick={() => setNewMcpDialogOpen(true)}>
      <Button.LeftIcon>
        <Plus />
      </Button.LeftIcon>
      <Button.Text>New MCP Server</Button.Text>
    </Button>
  );

  const newMcpServerDialog = (
    <InputDialog
      open={newMcpDialogOpen}
      onOpenChange={setNewMcpDialogOpen}
      title="Create MCP Server"
      description={`Create a new MCP server`}
      submitButtonText="Create"
      inputs={{
        label: "MCP server name",
        placeholder: "My MCP Server",
        value: newMcpServerName,
        onChange: setNewMcpServerName,
        onSubmit: handleCreateMcpServerSubmit,
        validate: (value) => value.length > 0 && value.length <= 40,
        hint: (value) => (
          <div className="flex justify-between w-full">
            <p className="text-destructive">
              {value.length > 40 && "Must be 40 characters or less"}
            </p>
            <p>{value.length}/40</p>
          </div>
        ),
      }}
    />
  );

  const builtInSection = (
    <Page.Section>
      <Page.Section.Title>Built-in MCP Servers</Page.Section.Title>
      <Page.Section.Description>
        Pre-configured MCP servers provided by Gram for your project. Connect
        from Claude Desktop, Cursor, or any MCP client.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
          {BUILT_IN_SERVERS.map((server) => (
            <BuiltInMCPCard key={server.slug} {...server} />
          ))}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );

  if (!toolsets.isLoading && toolsets.length === 0) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          {isProjectEmpty && !isProjectLoading ? (
            <InitialChoiceStep
              routes={routes}
              isFunctionsEnabled={isFunctionsEnabled}
            />
          ) : (
            <MCPEmptyState nonEmptyProjectCTA={newMcpServerButton} />
          )}
          {builtInSection}
          {newMcpServerDialog}
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
          <Page.Section.Title>Hosted MCP Servers</Page.Section.Title>
          <Page.Section.CTA>
            <ViewToggle value={viewMode} onChange={setViewMode} />
          </Page.Section.CTA>
          <Page.Section.CTA>{newMcpServerButton}</Page.Section.CTA>
          <Page.Section.Description className="max-w-2xl">
            Each source is exposed as an MCP server. First-party sources like
            functions and OpenAPI specs are private by default, while catalog
            servers are public.
          </Page.Section.Description>
          <Page.Section.Body>
            {viewMode === "grid" ? (
              <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
                {collections.map((registry) => (
                  <CollectionCard key={registry.id} registry={registry} />
                ))}
                {toolsets.map((toolset) => (
                  <MCPCard key={toolset.id} toolset={toolset} />
                ))}
              </div>
            ) : (
              <DotTable
                headers={[
                  { label: "Name" },
                  { label: "Visibility" },
                  { label: "URL" },
                  { label: "Tools" },
                ]}
              >
                {toolsets.map((toolset) => (
                  <MCPTableRow key={toolset.id} toolset={toolset} />
                ))}
              </DotTable>
            )}
          </Page.Section.Body>
        </Page.Section>
        {builtInSection}
        {newMcpServerDialog}
      </Page.Body>
    </Page>
  );
}
