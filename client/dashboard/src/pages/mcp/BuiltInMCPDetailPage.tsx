import { CodeBlock } from "@/components/code";
import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { Link } from "@/components/ui/link";
import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useSlugs } from "@/contexts/Sdk";
import { cn, getServerURL } from "@/lib/utils";
import { Badge, Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { toast } from "sonner";

const BUILT_IN_TOOLS = [
  {
    name: "gram_list_tools",
    description: "List all tools for a project",
  },
  {
    name: "platform_search_logs",
    description: "Search and inspect telemetry logs for the current project.",
  },
  {
    name: "gram_list_global_variations",
    description: "List globally defined tool variations.",
  },
  {
    name: "gram_search_tool_calls",
    description: "Search and list tool calls that match a search filter",
  },
  {
    name: "gram_get_toolset",
    description:
      "Get detailed information about a toolset including full HTTP tool definitions",
  },
  {
    name: "gram_get_observability_overview",
    description:
      "Get observability overview metrics including time series, tool breakdowns, and summary stats",
  },
  {
    name: "gram_list_toolsets",
    description: "List all toolsets for a project",
  },
  {
    name: "gram_get_mcp_metadata",
    description: "Fetch the metadata that powers the MCP install page.",
  },
  {
    name: "gram_list_chats_with_resolutions",
    description: "List all chats for a project with their resolutions",
  },
  {
    name: "gram_get_deployment_logs",
    description: "Get logs for a deployment.",
  },
  {
    name: "gram_list_chats",
    description: "List all chats for a project",
  },
];

const TAB_TRIGGER_CLASS = cn(
  "relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none!",
  "text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent!",
  "after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary",
);

export function BuiltInMCPDetailPage() {
  const { orgSlug } = useSlugs();
  const [activeTab, setActiveTab] = useState("overview");

  if (!orgSlug) {
    throw new Error("No org slug found.");
  }

  const mcpUrl = `${getServerURL()}/mcp/${orgSlug}-mcp-logs`;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullWidth noPadding>
        <DetailHero>
          <div className="flex items-end justify-between">
            <Stack gap={2}>
              <div className="flex items-center gap-3 ml-1">
                <Heading variant="h1">MCP Logs</Heading>
                <Badge variant="information">
                  <Badge.Text>Built-in</Badge.Text>
                </Badge>
              </div>
              <div className="flex items-center gap-2 ml-1">
                <Type className="max-w-2xl truncate text-muted-foreground">
                  {mcpUrl}
                </Type>
                <Button
                  variant="tertiary"
                  size="sm"
                  onClick={() => {
                    navigator.clipboard.writeText(mcpUrl);
                    toast.success("URL copied to clipboard");
                  }}
                  className="shrink-0 text-muted-foreground hover:text-foreground"
                >
                  <Button.LeftIcon>
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
                      <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
                    </svg>
                  </Button.LeftIcon>
                  <Button.Text className="sr-only">Copy URL</Button.Text>
                </Button>
              </div>
            </Stack>
          </div>
        </DetailHero>

        <Tabs
          value={activeTab}
          onValueChange={setActiveTab}
          className="w-full flex-1 flex flex-col"
        >
          <div className="border-b">
            <div className="max-w-[1270px] mx-auto px-8">
              <TabsList className="h-auto bg-transparent p-0 gap-6 rounded-none">
                <TabsTrigger value="overview" className={TAB_TRIGGER_CLASS}>
                  Overview
                </TabsTrigger>
                <TabsTrigger value="tools" className={TAB_TRIGGER_CLASS}>
                  Tools
                </TabsTrigger>
              </TabsList>
            </div>
          </div>

          <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
            <TabsContent value="overview" className="mt-0 w-full">
              <BuiltInOverviewTab mcpUrl={mcpUrl} />
            </TabsContent>

            <TabsContent value="tools" className="mt-0 w-full">
              <BuiltInToolsTab />
            </TabsContent>
          </div>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

function BuiltInOverviewTab({ mcpUrl }: { mcpUrl: string }) {
  return (
    <Stack className="mb-4">
      <PageSection
        heading="Hosted URL"
        description="The URL to connect to this MCP server from Claude Desktop, Cursor, or any MCP-compatible client."
      >
        <CodeBlock className="mb-2">{mcpUrl}</CodeBlock>
      </PageSection>

      <PageSection
        heading="Install Page"
        description="Share this page to give simple instructions for getting started with this MCP server in Cursor or Claude Desktop."
      >
        <div className="flex items-center gap-2 rounded-lg border bg-muted/20 p-2">
          <CodeBlock
            className="flex-grow overflow-hidden"
            innerClassName="!p-2 !pr-10 !bg-white dark:!bg-zinc-950"
            preClassName="whitespace-nowrap overflow-auto"
            copyable={true}
          >
            {`${mcpUrl}/install`}
          </CodeBlock>
          <Link external to={`${mcpUrl}/install`} noIcon>
            <Button variant="primary" className="px-4">
              <Button.LeftIcon>
                <Icon name="external-link" className="w-4 h-4" />
              </Button.LeftIcon>
              <Button.Text>View</Button.Text>
            </Button>
          </Link>
        </div>
      </PageSection>
    </Stack>
  );
}

function BuiltInToolsTab() {
  return (
    <Stack className="mb-4">
      <Stack
        direction="horizontal"
        justify="space-between"
        align="center"
        className="mb-4"
      >
        <Heading variant="h3">Tools</Heading>
      </Stack>

      <div className="border border-neutral-softest rounded-lg overflow-hidden w-full">
        <div className="bg-surface-secondary-default flex items-center pl-4 pr-3 py-4 border-b border-neutral-softest">
          <p className="text-sm leading-6 text-foreground">MCP Logs</p>
        </div>

        {BUILT_IN_TOOLS.map((tool) => (
          <div
            key={tool.name}
            className="flex items-center pl-4 pr-3 py-4 border-b border-neutral-softest last:border-b-0"
          >
            <div className="flex flex-col min-w-0 flex-1">
              <p className="text-sm leading-6 text-foreground">{tool.name}</p>
              <p className="text-sm leading-6 text-muted-foreground truncate">
                {tool.description}
              </p>
            </div>
          </div>
        ))}
      </div>
    </Stack>
  );
}

function PageSection({
  heading,
  description,
  children,
}: {
  heading: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <Stack gap={2} className="mb-8">
      <Heading variant="h3">{heading}</Heading>
      <Type muted small className="max-w-2xl">
        {description}
      </Type>
      {children}
    </Stack>
  );
}
