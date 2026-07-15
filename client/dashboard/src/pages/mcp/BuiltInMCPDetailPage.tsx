import { CodeBlock } from "@/components/code";
import { DetailLayout } from "@/components/layouts/detail-layout";
import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useSlugs } from "@/contexts/Sdk";
import { cn, getServerURL } from "@/lib/utils";
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { Navigate, useLocation, useNavigate, useParams } from "react-router";
import { useRoutes } from "@/routes";
import {
  activeTabFromPath,
  builtInTabHref,
  type TabValue,
} from "./BuiltInMCPDetailRouting";
import { BUILT_IN_TOOLS } from "./builtInMcpTools";

// Kept in sync with the built-in tab subroutes (see routes.tsx) so the
// breadcrumb hides the tab segment.
const BUILT_IN_TAB_URLS = ["overview", "tools"];

const TAB_TRIGGER_CLASS = cn(
  "relative h-11 rounded-none border-none bg-transparent! px-1 pt-3 pb-3 shadow-none!",
  "text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent!",
  "data-[state=active]:after:bg-primary after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5 after:bg-transparent",
);

export function BuiltInMCPDetailPage(): JSX.Element {
  const { orgSlug } = useSlugs();
  const { builtInSlug } = useParams<{ builtInSlug: string }>();
  const location = useLocation();
  const routes = useRoutes();
  const navigate = useNavigate();

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
      <Page.Body>
        <DetailLayout>
          <DetailLayout.Header
            eyebrow="MCP Server"
            title={
              <span className="inline-flex items-center gap-3">
                MCP Logs
                <Badge variant="information">
                  <Badge.Text>Built-in</Badge.Text>
                </Badge>
              </span>
            }
            subtitle={
              <span className="inline-flex items-center gap-2">
                <span className="max-w-2xl truncate">{mcpUrl}</span>
                <CopyButton
                  text={mcpUrl}
                  tooltip="Copy URL"
                  size="icon-sm"
                  className="text-muted-foreground hover:text-foreground shrink-0"
                  onCopy={() => {
                    toast.success("URL copied to clipboard");
                  }}
                />
              </span>
            }
          />

          <Tabs
            value={activeTab}
            onValueChange={(tab) => {
              void navigate(builtInTabHref(routes, idOrSlug, tab as TabValue));
            }}
            className="flex w-full flex-1 flex-col"
          >
            <DetailLayout.Tabs>
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <TabsTrigger value="overview" className={TAB_TRIGGER_CLASS}>
                  Overview
                </TabsTrigger>
                <TabsTrigger value="tools" className={TAB_TRIGGER_CLASS}>
                  Tools
                </TabsTrigger>
              </TabsList>
            </DetailLayout.Tabs>

            <DetailLayout.Content>
              <DetailLayout.Main>
                <TabsContent value="overview" className="mt-0 w-full">
                  <BuiltInOverviewTab mcpUrl={mcpUrl} />
                </TabsContent>

                <TabsContent value="tools" className="mt-0 w-full">
                  <BuiltInToolsTab />
                </TabsContent>
              </DetailLayout.Main>
            </DetailLayout.Content>
          </Tabs>
        </DetailLayout>
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
