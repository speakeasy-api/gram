import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { mcpServerRouteParam, tunneledMcpRouteParam } from "@/lib/sources";
import { TUNNELED_MCP_FEATURE_FLAG } from "@/lib/tunneledMcp";
import { useRoutes } from "@/routes";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import type {
  McpServer,
  TunneledMcpServer,
} from "@gram/client/models/components";
import { AlertCircle, Loader2, Network } from "lucide-react";
import { useState } from "react";
import { Navigate } from "react-router";
import { toast } from "sonner";
import { useCreateTunneledMcpSource } from "./hooks";
import { TunneledMcpSetupTabs } from "./TunneledMCPDetails";

function validateDisplayName(value: string): string | null {
  if (!value.trim()) return "Display name is required";
  return null;
}

type CreatedState = {
  tunneledMcpServer: TunneledMcpServer;
  tunnelKey: string;
  mcpServer: McpServer;
};

export default function CreateTunneledMcp(): JSX.Element | null {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isTunneledMcpEnabled = telemetry.isFeatureEnabled(
    TUNNELED_MCP_FEATURE_FLAG,
  );

  if (isTunneledMcpEnabled === undefined) {
    return null;
  }

  if (!isTunneledMcpEnabled) {
    return <Navigate to={routes.sources.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="mcp:write" level="page">
          <CreateTunneledMcpForm />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function CreateTunneledMcpForm() {
  const routes = useRoutes();
  const createSource = useCreateTunneledMcpSource();
  const [name, setName] = useState("");
  const [touched, setTouched] = useState(false);
  const [created, setCreated] = useState<CreatedState | null>(null);

  const validationError = touched ? validateDisplayName(name) : null;
  const submitDisabled =
    createSource.isPending || validateDisplayName(name) !== null;

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (validateDisplayName(name) !== null) return;

    try {
      const result = await createSource.mutateAsync({ name: name.trim() });
      setCreated(result);
      toast.success("Tunneled MCP server added");
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to add tunneled MCP server";
      toast.error(message);
    }
  };

  if (created) {
    return (
      <div className="max-w-4xl">
        <Stack gap={3} className="mb-8">
          <Stack direction="horizontal" gap={3} align="center">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-cyan-500/10 dark:bg-cyan-500/20">
              <Network className="h-5 w-5 text-cyan-700 dark:text-cyan-300" />
            </div>
            <Heading variant="h3">Tunneled MCP server added</Heading>
          </Stack>
          <Type muted>
            Use this tunnel key to connect an MCP server running in your own
            network. The key is only shown now.
          </Type>
        </Stack>

        <Stack gap={6}>
          <div className="rounded-lg border p-5">
            <div className="mb-3 flex items-center justify-between gap-3">
              <Type variant="subheading">Tunnel key</Type>
              <CopyButton text={created.tunnelKey} size="icon-sm" />
            </div>
            <CodeBlock language="text">{created.tunnelKey}</CodeBlock>
          </div>

          <TunneledMcpSetupTabs
            tunnelKey={created.tunnelKey}
            serverName={created.tunneledMcpServer.name}
          />

          <Stack direction="horizontal" gap={2}>
            <Button
              variant="primary"
              onClick={() =>
                routes.mcp.x.overview.goTo(
                  mcpServerRouteParam(created.mcpServer),
                )
              }
            >
              <Button.Text>Open MCP server</Button.Text>
            </Button>
            <Button
              variant="secondary"
              onClick={() =>
                routes.sources.source.goTo(
                  "tunneledmcp",
                  tunneledMcpRouteParam(created.tunneledMcpServer),
                )
              }
            >
              <Button.Text>View source</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </div>
    );
  }

  return (
    <div className="max-w-2xl">
      <Stack gap={3} className="mb-8">
        <Stack direction="horizontal" gap={3} align="center">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-cyan-500/10 dark:bg-cyan-500/20">
            <Network className="h-5 w-5 text-cyan-700 dark:text-cyan-300" />
          </div>
          <Heading variant="h3">Add a tunneled MCP server</Heading>
        </Stack>
        <Type muted>
          Register an MCP server that runs in your private network and connects
          outbound to Gram through a tunnel.
        </Type>
      </Stack>

      <form
        onSubmit={(e) => {
          void handleSubmit(e);
        }}
        noValidate
      >
        <Stack gap={4}>
          <Stack gap={1}>
            <label
              htmlFor="tunneled-mcp-name"
              className="text-sm leading-none font-medium"
            >
              Display name
            </label>
            <Input
              id="tunneled-mcp-name"
              autoFocus
              placeholder="Internal MCP server"
              value={name}
              onChange={(value) => {
                setName(value);
                if (!touched) setTouched(true);
              }}
              onBlur={() => setTouched(true)}
              aria-invalid={validationError ? true : undefined}
              aria-describedby={
                validationError ? "tunneled-mcp-name-error" : undefined
              }
            />
            {validationError && (
              <div
                id="tunneled-mcp-name-error"
                role="alert"
                className="text-destructive mt-2 flex items-center gap-1.5 text-xs"
              >
                <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                <span>{validationError}</span>
              </div>
            )}
          </Stack>

          <Stack gap={1}>
            <label className="text-sm leading-none font-medium">
              Transport
            </label>
            <Type muted small>
              Outbound tunnel to a normal MCP server
            </Type>
          </Stack>

          {createSource.isError && (
            <Alert variant="error" dismissible={false}>
              {createSource.error.message}
            </Alert>
          )}

          <Stack direction="horizontal" gap={2}>
            <Button type="submit" variant="primary" disabled={submitDisabled}>
              {createSource.isPending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Adding</Button.Text>
                </>
              ) : (
                <Button.Text>Add server</Button.Text>
              )}
            </Button>
            <Button
              type="button"
              variant="secondary"
              disabled={createSource.isPending}
              onClick={() => routes.sources.goTo()}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </form>
    </div>
  );
}
