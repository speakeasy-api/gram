import { CodeBlock } from "@/components/code";
import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { Link } from "@/components/ui/link";
import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useSlugs } from "@/contexts/Sdk";
import { cn, getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Badge, Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { useParams } from "react-router";
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

const DOCS_MCP_TOOLS = [
  {
    name: "docs_search",
    description:
      "Search the documentation corpus. Returns ranked chunks matching the query, with optional taxonomy filters.",
  },
  {
    name: "RemoteSkill",
    description:
      "Invoke a skill by ID. Returns the full skill document. The skillID parameter is an enum of all available skills.",
  },
];

const TAB_TRIGGER_CLASS = cn(
  "relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none!",
  "text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent!",
  "after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary",
);

export function BuiltInMCPDetailPage() {
  const { builtInSlug } = useParams<{ builtInSlug: string }>();

  if (builtInSlug === "docs-mcp") {
    return <DocsMCPDetailPage />;
  }

  return <LogsMCPDetailPage />;
}

// ── Logs MCP (existing) ───────────────────────────────────────────────────

function LogsMCPDetailPage() {
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
                <CopyButton text={mcpUrl} />
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
              <ToolsListSection title="MCP Logs" tools={BUILT_IN_TOOLS} />
            </TabsContent>
          </div>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

// ── Docs MCP ──────────────────────────────────────────────────────────────

function DocsMCPDetailPage() {
  const { orgSlug } = useSlugs();
  const routes = useRoutes();
  const [activeTab, setActiveTab] = useState("overview");

  if (!orgSlug) {
    throw new Error("No org slug found.");
  }

  const mcpUrl = `${getServerURL()}/mcp/${orgSlug}-docs-mcp`;

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
                <Heading variant="h1">Docs MCP</Heading>
                <Badge variant="information">
                  <Badge.Text>Built-in</Badge.Text>
                </Badge>
              </div>
              <div className="flex items-center gap-2 ml-1">
                <Type className="max-w-2xl truncate text-muted-foreground">
                  {mcpUrl}
                </Type>
                <CopyButton text={mcpUrl} />
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
                <TabsTrigger value="content" className={TAB_TRIGGER_CLASS}>
                  Content
                </TabsTrigger>
              </TabsList>
            </div>
          </div>

          <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
            <TabsContent value="overview" className="mt-0 w-full">
              <DocsMCPOverviewTab
                mcpUrl={mcpUrl}
                contextHref={routes.context.href()}
              />
            </TabsContent>
            <TabsContent value="tools" className="mt-0 w-full">
              <ToolsListSection title="Docs MCP" tools={DOCS_MCP_TOOLS} />
            </TabsContent>
            <TabsContent value="content" className="mt-0 w-full">
              <DocsMCPContentTab contextHref={routes.context.href()} />
            </TabsContent>
          </div>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

function DocsMCPOverviewTab({
  mcpUrl,
  contextHref,
}: {
  mcpUrl: string;
  contextHref: string;
}) {
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
        description="Share this page to give simple instructions for getting started with this MCP server."
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

      <PageSection
        heading="Status"
        description="Current status of your Docs MCP server."
      >
        <div className="flex items-center gap-4 rounded-lg border p-4">
          <div className="flex items-center gap-2">
            <div className="h-2.5 w-2.5 rounded-full bg-emerald-500" />
            <Type>Active</Type>
          </div>
          <div className="text-sm text-muted-foreground">
            18 documents &middot; 6 skills &middot; 142 chunks indexed
          </div>
        </div>
      </PageSection>

      <PageSection
        heading="Manage Content"
        description="View and configure your documentation corpus, skills, and search settings."
      >
        <Link to={contextHref} noIcon>
          <Button variant="primary" className="px-4">
            <Button.LeftIcon>
              <Icon name="library" className="w-4 h-4" />
            </Button.LeftIcon>
            <Button.Text>Open Context Manager</Button.Text>
          </Button>
        </Link>
      </PageSection>
    </Stack>
  );
}

function DocsMCPContentTab({ contextHref }: { contextHref: string }) {
  return (
    <Stack gap={4}>
      <div className="flex items-center justify-between">
        <Stack gap={1}>
          <Heading variant="h3">Documentation Corpus</Heading>
          <Type muted small>
            Your documentation content is managed in the Context page.
          </Type>
        </Stack>
        <Link to={contextHref} noIcon>
          <Button variant="primary" className="px-4">
            <Button.LeftIcon>
              <Icon name="library" className="w-4 h-4" />
            </Button.LeftIcon>
            <Button.Text>Open Context Manager</Button.Text>
          </Button>
        </Link>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <StatBox label="Documents" value="18" />
        <StatBox label="Skills" value="6" />
        <StatBox label="Chunks Indexed" value="142" />
      </div>

      <div className="grid grid-cols-3 gap-4">
        <StatBox label="Searches (24h)" value="847" />
        <StatBox label="Skill Invocations (24h)" value="234" />
        <StatBox label="Avg Latency" value="38ms" />
      </div>
    </Stack>
  );
}

function StatBox({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border p-4">
      <Type muted small className="block mb-1">
        {label}
      </Type>
      <Type variant="subheading">{value}</Type>
    </div>
  );
}

// ── Shared components ─────────────────────────────────────────────────────

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

function ToolsListSection({
  title,
  tools,
}: {
  title: string;
  tools: Array<{ name: string; description: string }>;
}) {
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
          <p className="text-sm leading-6 text-foreground">{title}</p>
        </div>

        {tools.map((tool) => (
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

function CopyButton({ text }: { text: string }) {
  return (
    <Button
      variant="tertiary"
      size="sm"
      onClick={() => {
        navigator.clipboard.writeText(text);
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
