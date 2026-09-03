import { InputDialog } from "@/components/input-dialog";
import { SettingsPage, SettingsSection } from "@/components/page-templates";
import { Grid } from "@/components/ui/Grid";
import { Text } from "@/components/ui/Text";
import { useIsSpeakeasyStaff } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { createDefaultGatewayEndpoint } from "@/lib/mcpEndpoints";
import { cn } from "@/lib/utils";
import { TUNNELED_MCP_FEATURE_FLAG } from "@/lib/tunneledMcp";
import { useRoutes } from "@/routes";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useQueryClient } from "@tanstack/react-query";
import { Code, FileCode, Layers, Network, Server, Store } from "lucide-react";
import type { ReactNode } from "react";
import { useState } from "react";
import { Link, Outlet } from "react-router";
import { toast } from "sonner";

export function AddMcpServerRoot(): JSX.Element {
  return <Outlet />;
}

// Most options are plain navigation; the gateway is created in place from a
// dialog, so it carries `onSelect` instead of an `href`.
type AddOption = {
  icon: ReactNode;
  title: string;
  description: string;
  href?: string;
  onSelect?: () => void;
};

function AddOptionCard({
  href,
  onSelect,
  icon,
  title,
  description,
}: AddOption): JSX.Element {
  // h-full so cards in a row match height regardless of description length.
  const className =
    "border-foreground/10 hover:bg-muted/30 flex h-full items-start gap-3 border p-4 text-left no-underline transition-colors hover:no-underline";
  const body = (
    <>
      <div className="bg-muted flex size-10 shrink-0 items-center justify-center">
        {icon}
      </div>
      <div className="flex flex-col gap-0.5">
        <span className="font-medium">{title}</span>
        <Text muted small>
          {description}
        </Text>
      </div>
    </>
  );

  if (href) {
    return (
      <Link to={href} className={className}>
        {body}
      </Link>
    );
  }
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(className, "w-full")}
    >
      {body}
    </button>
  );
}

function AddOptionGrid({ options }: { options: AddOption[] }): JSX.Element {
  return (
    <Grid columns={{ xs: 1, md: 2 }} gap={3}>
      {options.map((option) => (
        <Grid.Item key={option.title}>
          <AddOptionCard {...option} />
        </Grid.Item>
      ))}
    </Grid>
  );
}

/**
 * The single entry point for bringing a server under the gateway. Options are
 * grouped by how the server is reached — hosted remotely, through a tunnel, or
 * already reachable by clients — rather than by which backend table backs it.
 * OpenAPI and functions stay reachable here, under Advanced, and nowhere else
 * in the navigation.
 */
export default function AddMcpServer(): JSX.Element {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();
  const isSpeakeasyStaff = useIsSpeakeasyStaff();
  const [gatewayDialogOpen, setGatewayDialogOpen] = useState(false);
  const [gatewayName, setGatewayName] = useState("");
  // Gateways are behind a rollout flag: opt-in, so an unresolved flag keeps the
  // option hidden. The flag gates discoverability only — the backend enforces
  // mcp:write regardless.
  const gatewaysEnabled =
    useFeatureFlag(FEATURE_FLAGS.gatewayEndpoints).status === "enabled";
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;
  const isTunneledMcpEnabled =
    telemetry.isFeatureEnabled(TUNNELED_MCP_FEATURE_FLAG) ?? false;

  // Creation is two calls: the gateway, then its default address. The address
  // is best-effort (createDefaultGatewayEndpoint warns rather than throwing),
  // so a failure there still lands the user on a usable gateway page.
  const handleCreateGateway = async () => {
    const gateway = await client.metaMcp.create({
      createMetaMcpServerForm: { name: gatewayName },
    });

    if (orgSlug) {
      await createDefaultGatewayEndpoint(
        client,
        gateway.id,
        gateway.name,
        orgSlug,
      );
    }

    await Promise.all([
      invalidateAllMetaMcpServers(queryClient, { refetchType: "all" }),
      invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
    ]);
    toast.success(`Gateway "${gateway.name}" created`);
    routes.mcp.gateway.overview.goTo(gateway.id);
  };

  const connectOptions: AddOption[] = [
    {
      href: routes.mcp.add.catalog.href(),
      icon: <Store className="text-foreground size-5" />,
      title: "From the catalog",
      description:
        "Pick a reviewed third-party server — Salesforce, Datadog, Linear, Slack, Okta and more.",
    },
    {
      href: routes.mcp.add.remote.href(),
      icon: <Network className="text-foreground size-5" />,
      title: "Hosted remotely",
      description:
        "Add an existing remote server by URL. We proxy requests to it.",
    },
    ...(isTunneledMcpEnabled
      ? [
          {
            href: routes.mcp.add.tunneled.href(),
            icon: <Network className="text-foreground size-5" />,
            title: "Reachable through a tunnel",
            description:
              "Connect a server running inside your own network through a tunnel.",
          },
        ]
      : []),
    ...(isSpeakeasyStaff
      ? [
          {
            href: routes.mcp.add.unproxied.href(),
            icon: <Server className="text-foreground size-5" />,
            title: "Already reachable by clients",
            description:
              "Track a server your people connect to directly, without proxying it.",
          },
        ]
      : []),
  ];

  const gatewayOptions: AddOption[] = [
    {
      onSelect: () => setGatewayDialogOpen(true),
      icon: <Layers className="text-foreground size-5" />,
      title: "New gateway",
      description:
        "One address fronting a set of servers. Pick its members after creating it.",
    },
  ];

  const advancedOptions: AddOption[] = [
    {
      href: routes.mcp.add.openapi.href(),
      icon: <FileCode className="text-foreground size-5" />,
      title: "From your API",
      description: "Upload an OpenAPI document to generate tools.",
    },
    ...(isFunctionsEnabled
      ? [
          {
            href: routes.mcp.add.function.href(),
            icon: <Code className="text-foreground size-5" />,
            title: "Write custom code",
            description: "Create tools with TypeScript functions.",
          },
        ]
      : []),
  ];

  return (
    <SettingsPage
      scope="mcp:write"
      title="Add MCP server"
      description="Bring a server under the gateway so you can put it in front of your people."
    >
      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>Add a server</SettingsSection.Title>
          <SettingsSection.Description>
            Pick the option that matches how the server is reached.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <AddOptionGrid options={connectOptions} />
      </SettingsSection>

      {gatewaysEnabled && (
        <SettingsSection>
          <SettingsSection.Header>
            <SettingsSection.Title>Group servers</SettingsSection.Title>
            <SettingsSection.Description>
              Put several servers behind a single address so people connect once
              instead of per server.
            </SettingsSection.Description>
          </SettingsSection.Header>
          <AddOptionGrid options={gatewayOptions} />
        </SettingsSection>
      )}

      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>Advanced</SettingsSection.Title>
          <SettingsSection.Description>
            Build a server from your own API or code instead of connecting one
            that already exists.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <AddOptionGrid options={advancedOptions} />
        <Text muted small>
          Already uploaded something?{" "}
          <routes.mcp.sources.Link>Browse sources</routes.mcp.sources.Link> to
          manage the OpenAPI documents and functions in this project.
        </Text>
      </SettingsSection>

      <InputDialog
        open={gatewayDialogOpen}
        onOpenChange={setGatewayDialogOpen}
        title="Create Gateway"
        description="One MCP endpoint fronting a set of MCP servers. Add members after creating it."
        submitButtonText="Create"
        inputs={{
          label: "Gateway name",
          placeholder: "My Gateway",
          value: gatewayName,
          onChange: setGatewayName,
          onSubmit: () => void handleCreateGateway(),
          validate: (value) => value.length > 0 && value.length <= 40,
          hint: (value) => (
            <div className="flex w-full justify-between">
              <p className="text-destructive">
                {value.length > 40 && "Must be 40 characters or less"}
              </p>
              <p>{value.length}/40</p>
            </div>
          ),
        }}
      />
    </SettingsPage>
  );
}
