import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { Link } from "@/components/ui/link";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { Navigate, useLocation, useParams } from "react-router";
import { activeTabFromPath, builtInTabHref } from "./BuiltInMCPDetailRouting";
import { BUILT_IN_TOOLS } from "./builtInMcpTools";
import { useRoutes } from "@/routes";

const BUILT_IN_TAB_URLS = ["overview", "tools"];

export function BuiltInMCPDetailPage(): JSX.Element {
  const { orgSlug } = useSlugs();
  const { builtInSlug } = useParams<{ builtInSlug: string }>();
  const location = useLocation();
  const routes = useRoutes();

  if (!orgSlug) {
    throw new Error("No org slug found.");
  }

  const idOrSlug = builtInSlug ?? "";
  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const mcpUrl = `${getServerURL()}/mcp/${orgSlug}-mcp-logs`;

  if (!activeTab) {
    return (
      <Navigate to={builtInTabHref(routes, idOrSlug, "overview")} replace />
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [idOrSlug]: "MCP Logs" }}
          skipSegments={BUILT_IN_TAB_URLS}
        />
      </Page.Header>
      <Page.Body fullWidth>
        <div className="mx-auto w-full max-w-[1270px] flex-1">
          {activeTab === "overview" && <BuiltInOverviewTab mcpUrl={mcpUrl} />}
          {activeTab === "tools" && <BuiltInToolsTab />}
        </div>
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
        <div className="bg-muted/20 flex items-center gap-2 rounded-lg border p-2">
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
                <Icon name="external-link" className="h-4 w-4" />
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

      <div className="border-neutral-softest w-full overflow-hidden rounded-lg border">
        <div className="bg-surface-secondary-default border-neutral-softest flex items-center border-b py-4 pr-3 pl-4">
          <p className="text-foreground text-sm leading-6">MCP Logs</p>
        </div>

        {BUILT_IN_TOOLS.map((tool) => (
          <div
            key={tool.name}
            className="border-neutral-softest flex items-center border-b py-4 pr-3 pl-4 last:border-b-0"
          >
            <div className="flex min-w-0 flex-1 flex-col">
              <p className="text-foreground text-sm leading-6">{tool.name}</p>
              <p className="text-muted-foreground truncate text-sm leading-6">
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
