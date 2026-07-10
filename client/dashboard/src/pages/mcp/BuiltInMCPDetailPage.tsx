import { CodeBlock } from "@/components/code";
import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useSlugs } from "@/contexts/Sdk";
import { cn, getServerURL } from "@/lib/utils";
import { Badge, Button, Stack } from "@/components/ui/moonshine";
import { ExternalLink } from "lucide-react";
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
    name: "gram_get_deployment_logs",
    description: "Get logs for a deployment.",
  },
  {
    name: "gram_list_chats",
    description: "List all chats for a project",
  },
];

const TAB_TRIGGER_CLASS = cn(
  "relative h-11 rounded-none border-none bg-transparent! px-1 pt-3 pb-3 shadow-none!",
  "text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent!",
  "data-[state=active]:after:bg-primary after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5 after:bg-transparent",
);

export function BuiltInMCPDetailPage(): JSX.Element {
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
              <div className="ml-1 flex items-center gap-3">
                <Heading variant="h1">MCP Logs</Heading>
                <Badge variant="information">
                  <Badge.Text>Built-in</Badge.Text>
                </Badge>
              </div>
              <div className="ml-1 flex items-center gap-2">
                <Type className="text-muted-foreground max-w-2xl truncate">
                  {mcpUrl}
                </Type>
                <CopyButton
                  text={mcpUrl}
                  tooltip="Copy URL"
                  size="icon-sm"
                  className="text-muted-foreground hover:text-foreground shrink-0"
                  onCopy={() => {
                    toast.success("URL copied to clipboard");
                  }}
                />
              </div>
            </Stack>
          </div>
        </DetailHero>

        <Tabs
          value={activeTab}
          onValueChange={setActiveTab}
          className="flex w-full flex-1 flex-col"
        >
          <div className="border-b">
            <div className="mx-auto max-w-[1270px] px-8">
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <TabsTrigger value="overview" className={TAB_TRIGGER_CLASS}>
                  Overview
                </TabsTrigger>
                <TabsTrigger value="tools" className={TAB_TRIGGER_CLASS}>
                  Tools
                </TabsTrigger>
              </TabsList>
            </div>
          </div>

          <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
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
        <Card className="flex-row items-center gap-2 p-2">
          <CodeBlock
            className="flex-grow overflow-hidden"
            innerClassName="!p-2 !pr-10 !bg-white dark:!bg-zinc-950"
            preClassName="whitespace-nowrap overflow-auto"
            copyable={true}
          >
            {`${mcpUrl}/install`}
          </CodeBlock>
          <Button asChild variant="primary" className="px-4">
            <a
              href={`${mcpUrl}/install`}
              target="_blank"
              rel="noopener noreferrer"
            >
              <Button.LeftIcon>
                <ExternalLink className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>View</Button.Text>
            </a>
          </Button>
        </Card>
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

      <Card className="w-full gap-0 overflow-hidden p-0">
        <div className="bg-surface-secondary-default border-neutral-softest flex items-center border-b py-4 pr-3 pl-4">
          <Type muted className="font-mono text-xs tracking-[0.08em] uppercase">
            MCP Logs
          </Type>
        </div>

        {BUILT_IN_TOOLS.map((tool) => (
          <div
            key={tool.name}
            className="border-neutral-softest flex items-center border-b py-4 pr-3 pl-4 last:border-b-0"
          >
            <div className="flex min-w-0 flex-1 flex-col">
              <Type>{tool.name}</Type>
              <Type muted small className="truncate">
                {tool.description}
              </Type>
            </div>
          </div>
        ))}
      </Card>
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
