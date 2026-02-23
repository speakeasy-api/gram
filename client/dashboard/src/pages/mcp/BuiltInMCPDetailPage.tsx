import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { MCPHeroIllustration } from "@/components/sources/SourceCardIllustrations";
import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useSlugs } from "@/contexts/Sdk";
import { cn, getServerURL } from "@/lib/utils";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { useParams } from "react-router";
import { toast } from "sonner";

const BUILT_IN_TOOLS = [
  {
    name: "gram_search_logs",
    description:
      "Search telemetry logs with filters like time range, status codes, and text queries.",
  },
  {
    name: "gram_search_tool_calls",
    description: "Search tool call history across your MCP servers.",
  },
  {
    name: "gram_search_chats",
    description: "Search chat sessions and conversation history.",
  },
  {
    name: "gram_get_project_metrics_summary",
    description:
      "Get a summary of project metrics including request counts and error rates.",
  },
  {
    name: "gram_get_observability_overview",
    description:
      "Get observability dashboard data with aggregated telemetry insights.",
  },
  {
    name: "gram_list_filter_options",
    description:
      "List available filter values for narrowing down log searches.",
  },
];

const TAB_TRIGGER_CLASS = cn(
  "relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none!",
  "text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent!",
  "after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary",
);

export function BuiltInMCPDetailPage() {
  const { builtInSlug } = useParams();
  const { projectSlug } = useSlugs();
  const [activeTab, setActiveTab] = useState("overview");

  const mcpUrl = `${getServerURL()}/mcp/speakeasy-team-gram`;

  const configJson = `{
  "mcpServers": {
    "GramLogs": {
      "command": "npx",
      "args": [
        "mcp-remote@0.1.25",
        "${mcpUrl}",
        "--header",
        "MCP-Gram-Key:\${GRAM_KEY}",
        "--header",
        "MCP-Gram-Project:${projectSlug}"
      ],
      "env": {
        "GRAM_KEY": "<your-gram-api-key>"
      }
    }
  }
}`;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullWidth noPadding>
        {/* Hero Header */}
        <div className="relative w-full h-64 overflow-hidden">
          <MCPHeroIllustration
            toolsetSlug={`built-in-${builtInSlug}`}
            className="saturate-[.3]"
          />

          {/* Overlay for text readability */}
          <div className="absolute inset-0 bg-gradient-to-t from-foreground/50 via-foreground/20 to-transparent" />
          <div className="absolute bottom-0 left-0 right-0 px-8 py-8 max-w-[1270px] mx-auto w-full">
            <div className="flex items-end justify-between">
              <Stack gap={2}>
                <div className="flex items-center gap-3 ml-1">
                  <Heading variant="h1" className="text-background">
                    Gram Logs
                  </Heading>
                  <Badge variant="information">
                    <Badge.Text>Built-in</Badge.Text>
                  </Badge>
                </div>
                <div className="flex items-center gap-2 ml-1">
                  <Type className="max-w-2xl truncate !text-background/70">
                    {mcpUrl}
                  </Type>
                  <button
                    type="button"
                    className="shrink-0 text-background/70 hover:text-background transition-colors"
                    onClick={() => {
                      navigator.clipboard.writeText(mcpUrl);
                      toast.success("URL copied to clipboard");
                    }}
                  >
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
                  </button>
                </div>
              </Stack>
            </div>
          </div>
        </div>

        {/* Sub-navigation tabs */}
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

          {/* Tab Content */}
          <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
            <TabsContent value="overview" className="mt-0 w-full">
              <BuiltInOverviewTab mcpUrl={mcpUrl} configJson={configJson} />
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

function BuiltInOverviewTab({
  mcpUrl,
  configJson,
}: {
  mcpUrl: string;
  configJson: string;
}) {
  const { projectSlug } = useSlugs();

  return (
    <Stack className="mb-4">
      <PageSection
        heading="Hosted URL"
        description="The URL to connect to this MCP server from Claude Desktop, Cursor, or any MCP-compatible client."
      >
        <CodeBlock className="mb-2">{mcpUrl}</CodeBlock>
      </PageSection>

      <PageSection
        heading="Configuration"
        description="Add this to your MCP client configuration. Replace the API key placeholder with your Gram API key from Settings."
      >
        <Type className="font-medium">
          Claude Desktop / Cursor Configuration
        </Type>
        <Type muted small className="max-w-3xl mb-2!">
          Uses <code>mcp-remote</code> to connect via stdio. The project slug{" "}
          <code>{projectSlug}</code> is pre-filled.
        </Type>
        <CodeBlock>{configJson}</CodeBlock>
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
        {/* Group header */}
        <div className="bg-surface-secondary-default flex items-center pl-4 pr-3 py-4 border-b border-neutral-softest">
          <p className="text-sm leading-6 text-foreground">Gram Telemetry</p>
        </div>

        {/* Tool rows */}
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
