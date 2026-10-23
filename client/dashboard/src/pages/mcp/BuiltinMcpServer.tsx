import { CodeBlock } from "@/components/code";
import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { extractToolParameters } from "@/components/mcp/tool-parameters";
import {
  McpToolParameters,
  McpToolRow,
  McpToolRows,
} from "@/components/mcp/tool-browser";
import { useBuiltinMcpCredentials } from "@/components/mcp/useBuiltinMcpCredentials";
import { useBuiltinMcpServers } from "@/components/mcp/useBuiltinMcpServers";
import { useUpdateBuiltinMcpServer } from "@/components/mcp/useUpdateBuiltinMcpServer";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Field, FieldLabel } from "@/components/ui/Field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useTabScrollReset } from "@/hooks/useTabScrollReset";
import { useRoutes } from "@/routes";
import type { BuiltinMcpServer } from "@gram/client/models/components/builtinmcpserver.js";
import { Check, Minus } from "lucide-react";
import { useState } from "react";
import { Link, Navigate, useLocation, useParams } from "react-router";
import {
  activeBuiltinTabFromPath,
  builtinMcpTabHref,
  BUILTIN_MCP_TAB_URLS,
  type BuiltinMcpTab,
} from "./BuiltinMcpServerRouting";

/** The sidebar area names the gateway; this page is a server inside it. */
const MCP_SERVERS_AREA = "MCP Servers";

/**
 * A built-in server's detail page. It is shaped like any other MCP server's —
 * the rail carries identity, visibility and the address; the tabs carry
 * connecting, browsing tools and credentials — because nothing about being
 * built in changes what someone came here to do. What it leaves out is what it
 * genuinely has none of: there are no tools to add or remove, no sources, and
 * no server to delete.
 */
export default function BuiltinMcpServerPage(): JSX.Element | null {
  const organization = useOrganization();
  const project = useProject();
  return (
    <RequireScope scope="org:admin" resourceId={organization.id} level="page">
      <BuiltinMcpServerContent key={project.id} />
    </RequireScope>
  );
}

function BuiltinMcpServerContent() {
  const { builtinKey = "" } = useParams<{ builtinKey: string }>();
  const { servers, isLoading, isError, refetch } = useBuiltinMcpServers();
  const routes = useRoutes();
  const location = useLocation();
  const server = servers.find((entry) => entry.key === builtinKey);
  const activeTab = activeBuiltinTabFromPath(location.pathname, builtinKey);
  const tabContentRef = useTabScrollReset(activeTab ?? undefined);

  if (!isLoading && !isError && server && !activeTab) {
    return (
      <Navigate
        to={builtinMcpTabHref(routes, builtinKey, "overview")}
        replace
      />
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={server ? { [builtinKey]: server.name } : {}}
          skipSegments={["builtin", ...BUILTIN_MCP_TAB_URLS]}
        />
      </Page.Header>
      <Page.Body fullWidth className="gap-0">
        <div
          ref={tabContentRef}
          className="mx-auto w-full max-w-[1270px] flex-1"
        >
          {isLoading ? (
            <BuiltinLoading />
          ) : isError ? (
            <Page.Section>
              <Page.Section.Title area={MCP_SERVERS_AREA}>
                Built-in MCP server
              </Page.Section.Title>
              <Page.Section.Body>
                <InlineEmptyState
                  icon="triangle-alert"
                  heading="Couldn't load this server"
                  description="The built-in catalog didn't answer. Nothing has changed."
                  action={
                    <Button variant="secondary" onClick={() => void refetch()}>
                      <Button.Text>Try again</Button.Text>
                    </Button>
                  }
                />
              </Page.Section.Body>
            </Page.Section>
          ) : !server ? (
            <RouteNotFoundState
              title="Built-in server not found"
              description="Speakeasy has no built-in server by this name."
              action={
                <routes.mcp.Link>
                  <SecondaryRouteAction>
                    Back to MCP servers
                  </SecondaryRouteAction>
                </routes.mcp.Link>
              }
            />
          ) : (
            renderBuiltinTab(activeTab ?? "overview", server)
          )}
        </div>
      </Page.Body>
    </Page>
  );
}

function renderBuiltinTab(tab: BuiltinMcpTab, server: BuiltinMcpServer) {
  switch (tab) {
    case "overview":
      return <BuiltinOverviewTab server={server} />;
    case "tools":
      return <BuiltinToolsTab server={server} />;
    case "authentication":
      return (
        <BuiltinAuthenticationTab
          key={`${server.id}:${server.environmentId ?? ""}`}
          server={server}
        />
      );
  }
}

function BuiltinLoading() {
  return (
    <div className="space-y-6 py-2">
      <Skeleton className="h-3 w-24" />
      <Skeleton className="h-9 w-64" />
      <Skeleton className="h-40 w-full" />
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* Overview                                                                    */
/* -------------------------------------------------------------------------- */

function BuiltinOverviewTab({ server }: { server: BuiltinMcpServer }) {
  const routes = useRoutes();
  const { ready, isLoading, isError } = useBuiltinMcpCredentials(server);
  const authenticationHref = builtinMcpTabHref(
    routes,
    server.key,
    "authentication",
  );

  return (
    <div className="mb-8 space-y-6">
      <Page.Section>
        <Page.Section.Title area={MCP_SERVERS_AREA}>
          {server.name}
        </Page.Section.Title>
        <Page.Section.Description>
          {server.description}
        </Page.Section.Description>
      </Page.Section>

      {/* The only thing the page interrupts for: a missing credential. */}
      {!isLoading && !isError && !ready && (
        <Alert variant="warning" title={null}>
          <div className="flex w-full flex-wrap items-center justify-between gap-3">
            <span>Add {server.name} credentials to use these tools.</span>
            <Button variant="secondary" size="sm" asChild>
              <Link to={authenticationHref}>
                <Button.Text>Add credentials</Button.Text>
              </Link>
            </Button>
          </div>
        </Alert>
      )}

      <BuiltinConnectSection server={server} />
    </div>
  );
}

type ConnectRecipe = { id: string; label: string; language: string };

const CONNECT_RECIPES: ConnectRecipe[] = [
  { id: "url", label: "URL and header", language: "http" },
  { id: "json", label: "mcp.json", language: "json" },
  { id: "cli", label: "CLI", language: "bash" },
];

/** Stands in for the key on the runtimes that read it from the environment. */
const KEY_ENV = "SPEAKEASY_AGENT_KEY";

function connectSnippet(recipe: string, name: string, url: string): string {
  const handle = name.toLowerCase().replace(/[^a-z0-9]+/g, "-");
  switch (recipe) {
    case "json":
      return JSON.stringify(
        {
          mcpServers: {
            [handle]: {
              type: "http",
              url,
              headers: { Authorization: `Bearer \${env:${KEY_ENV}}` },
            },
          },
        },
        null,
        2,
      );
    case "cli":
      return [
        `claude mcp add --transport http ${handle} ${url} \\`,
        `  --header "Authorization: Bearer $${KEY_ENV}"`,
      ].join("\n");
    default:
      return `${url}\n\nAuthorization: Bearer $${KEY_ENV}`;
  }
}

function BuiltinConnectSection({ server }: { server: BuiltinMcpServer }) {
  const routes = useRoutes();
  const [recipe, setRecipe] = useState(CONNECT_RECIPES[0]!.id);
  const active =
    CONNECT_RECIPES.find((entry) => entry.id === recipe) ?? CONNECT_RECIPES[0]!;

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Direct connection</SettingsSection.Title>
        <SettingsSection.Description>
          Point an agent at this project&rsquo;s endpoint for {server.name} and
          authenticate with an agent key.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {/* The address is in the rail and inside every snippet below; a
              third copy of it here was the same fact said three times. */}
          <SegmentedControl
            ariaLabel="Snippet format"
            value={active.id}
            onChange={setRecipe}
            options={CONNECT_RECIPES.map(({ id, label }) => ({
              value: id,
              label,
            }))}
          />
          <CodeBlock language={active.language} copyLabel="snippet">
            {connectSnippet(active.id, server.name, server.url)}
          </CodeBlock>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Need a key?{" "}
            <Link to={routes.identities.agents.href()} className="underline">
              Create an agent
            </Link>{" "}
            and issue one from its credentials.
          </SettingsSection.FooterHint>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

/* -------------------------------------------------------------------------- */
/* Tools                                                                       */
/* -------------------------------------------------------------------------- */

function BuiltinToolsTab({ server }: { server: BuiltinMcpServer }) {
  const [search, setSearch] = useState("");
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const query = search.trim().toLowerCase();
  const tools = query
    ? server.tools.filter((tool) =>
        [tool.name, tool.description].some((value) =>
          value.toLowerCase().includes(query),
        ),
      )
    : server.tools;
  // Resolved during render so a search that excludes the open tool closes the
  // drawer instead of showing a row that is no longer in the list.
  const selected =
    selectedName !== null
      ? (tools.find((tool) => tool.name === selectedName) ?? null)
      : null;

  return (
    <div className="mb-8 space-y-4">
      <Page.Section>
        <Page.Section.Title area={MCP_SERVERS_AREA}>Tools</Page.Section.Title>
        <Page.Section.Description>
          Everything an agent connected to this server can call. Speakeasy
          maintains this set; it is the same in every project.
        </Page.Section.Description>
      </Page.Section>

      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={setSearch}
          placeholder="Search tools"
        />
        <Page.Toolbar.Actions>
          <Text muted small>
            {tools.length} of {server.tools.length} tools
          </Text>
        </Page.Toolbar.Actions>
      </Page.Toolbar>

      {tools.length === 0 ? (
        <InlineEmptyState
          icon="search"
          heading="No tools match that search"
          description="Try a shorter word, or clear the search to see all tools."
          action={
            <Button variant="secondary" onClick={() => setSearch("")}>
              <Button.Text>Clear search</Button.Text>
            </Button>
          }
        />
      ) : (
        <McpToolRows>
          {tools.map((tool) => (
            <McpToolRow
              key={tool.name}
              name={tool.name}
              description={tool.description}
              selected={tool.name === selectedName}
              onSelect={() => setSelectedName(tool.name)}
            />
          ))}
        </McpToolRows>
      )}

      <Sheet
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedName(null);
        }}
      >
        <SheetContent className="w-full gap-0 sm:max-w-md">
          {selected && (
            <>
              <SheetHeader className="gap-2 border-b">
                <SheetTitle className="pr-8 font-mono text-sm break-all">
                  {selected.name}
                </SheetTitle>
                <SheetDescription className="whitespace-pre-line">
                  {selected.description || "No description provided."}
                </SheetDescription>
              </SheetHeader>
              <div className="flex-1 overflow-y-auto p-4">
                <Text variant="subheading" as="h4" className="mb-3">
                  Parameters
                </Text>
                <McpToolParameters
                  parameters={extractToolParameters(selected.inputSchema)}
                />
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* Authentication                                                              */
/* -------------------------------------------------------------------------- */

const PROJECT_DEFAULT_VALUE = "project-default";

function BuiltinAuthenticationTab({ server }: { server: BuiltinMcpServer }) {
  const routes = useRoutes();
  const { environments, environment, credentials, isLoading, isError } =
    useBuiltinMcpCredentials(server);
  const [environmentId, setEnvironmentId] = useState(
    server.environmentId ?? PROJECT_DEFAULT_VALUE,
  );
  const update = useUpdateBuiltinMcpServer();
  const unchanged =
    environmentId === (server.environmentId ?? PROJECT_DEFAULT_VALUE);

  return (
    <div className="mb-8 space-y-6">
      <Page.Section>
        <Page.Section.Title area={MCP_SERVERS_AREA}>
          Authentication
        </Page.Section.Title>
        <Page.Section.Description>
          What Speakeasy uses to call {server.name} on your behalf.
        </Page.Section.Description>
      </Page.Section>

      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>Upstream authentication</SettingsSection.Title>
          <SettingsSection.Description>
            Speakeasy reads your {server.name} credentials from an environment
            in this project, the same way your own MCP servers get theirs.
            Values are never shown here.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <Field>
              <FieldLabel htmlFor="builtin-environment">Environment</FieldLabel>
              <Select
                value={environmentId}
                onValueChange={setEnvironmentId}
                disabled={isLoading || isError || update.isPending}
              >
                <SelectTrigger id="builtin-environment" className="max-w-md">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={PROJECT_DEFAULT_VALUE}>
                    Project default
                  </SelectItem>
                  {environments.map((candidate) => (
                    <SelectItem key={candidate.id} value={candidate.id}>
                      {candidate.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            {isError ? (
              <Text small className="text-destructive">
                Couldn&rsquo;t load this project&rsquo;s environments, so
                Speakeasy can&rsquo;t say which credentials are stored.
              </Text>
            ) : (
              <CredentialList
                credentials={credentials}
                serverName={server.name}
                environmentName={environment?.name}
                environmentHref={
                  environment
                    ? routes.environments.environment.href(environment.slug)
                    : routes.environments.href()
                }
                isLoading={isLoading}
              />
            )}
          </SettingsSection.Body>
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              Each tool uses the credential it needs; some require the user
              token.
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              <FooterSaveButton
                pending={update.isPending}
                disabled={update.isPending || unchanged || isError}
                onClick={() =>
                  update.save({
                    key: server.key,
                    expectedVersion: server.version,
                    hidden: server.hidden,
                    environmentId:
                      environmentId === PROJECT_DEFAULT_VALUE
                        ? undefined
                        : environmentId,
                  })
                }
              />
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        </SettingsSection.Panel>
      </SettingsSection>

      <Text small muted as="p">
        These credentials are what Speakeasy sends to {server.name}. They are
        not what a client sends to reach this server: a connection made through
        a gateway is authenticated by that gateway.
      </Text>
    </div>
  );
}

function CredentialList({
  credentials,
  serverName,
  environmentName,
  environmentHref,
  isLoading,
}: {
  credentials: { variable: string; label: string; set: boolean }[];
  serverName: string;
  environmentName: string | undefined;
  environmentHref: string;
  isLoading: boolean;
}) {
  const anySet = credentials.some((credential) => credential.set);
  if (isLoading) {
    return (
      <div className="space-y-3">
        {credentials.map((credential) => (
          <Skeleton key={credential.variable} className="h-5 w-full max-w-md" />
        ))}
      </div>
    );
  }
  return (
    <>
      <ul className="divide-border max-w-xl divide-y border-y">
        {credentials.map((credential) => (
          <li
            key={credential.variable}
            className="flex items-center justify-between gap-4 py-2.5"
          >
            <div className="flex min-w-0 flex-col">
              <Text small>{credential.label}</Text>
              <Text muted small className="font-mono text-xs">
                {credential.variable}
              </Text>
            </div>
            {credential.set ? (
              <span className="text-default-success flex shrink-0 items-center gap-1.5 text-sm">
                <Check className="size-3.5" aria-hidden="true" />
                Set
              </span>
            ) : (
              <span className="text-muted-foreground flex shrink-0 items-center gap-1.5 text-sm">
                <Minus className="size-3.5" aria-hidden="true" />
                Not set
              </span>
            )}
          </li>
        ))}
      </ul>
      <Text small muted>
        {!environmentName ? (
          <>
            This project has no environment to read from yet.{" "}
            <Link to={environmentHref} className="underline">
              Create one
            </Link>
            .
          </>
        ) : anySet ? (
          <>
            Stored in{" "}
            <Link to={environmentHref} className="underline">
              {environmentName}
            </Link>
            .
          </>
        ) : (
          <>
            Add one of these to{" "}
            <Link to={environmentHref} className="underline">
              {environmentName}
            </Link>{" "}
            so agents can call {serverName}.
          </>
        )}
      </Text>
    </>
  );
}
