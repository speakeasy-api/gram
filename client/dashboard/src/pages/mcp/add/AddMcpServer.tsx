import { InputDialog } from "@/components/input-dialog";
import { SettingsPage, SettingsSection } from "@/components/page-templates";
import { Card } from "@/components/ui/Card";
import { useIconConfetti } from "@/components/icon-confetti";
import { Text } from "@/components/ui/Text";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { createDefaultGatewayEndpoint } from "@/lib/mcpEndpoints";
import { TUNNELED_MCP_FEATURE_FLAG } from "@/lib/tunneledMcp";
import { useRoutes } from "@/routes";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  Blocks,
  Boxes,
  Cable,
  Cloud,
  Code,
  FileCode,
  Layers,
} from "lucide-react";
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
  const { canvasRef, start, stop } = useIconConfetti();
  const body = (
    <Card.Entity
      icon={icon}
      iconRailClassName="isolate"
      iconTileClassName="icon-hover-pulse"
      overlay={
        <canvas
          ref={canvasRef}
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 -z-10 size-full"
        />
      }
      // The gateway is created in place, so it takes the card's own click
      // handling; everything else is wrapped in a real link below.
      onClick={onSelect}
      className={onSelect ? "cursor-pointer text-left" : undefined}
    >
      <Text
        variant="subheading"
        as="div"
        className="text-md group-hover:text-primary transition-colors"
      >
        {title}
      </Text>
      <Text small muted className="mt-1">
        {description}
      </Text>
      <div className="mt-auto flex items-center justify-end pt-3">
        <span className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
          Continue
          <ArrowRight className="size-3.5" />
        </span>
      </div>
    </Card.Entity>
  );

  if (!href) {
    return (
      <div className="h-full" onMouseEnter={start} onMouseLeave={stop}>
        {body}
      </div>
    );
  }
  return (
    <Link
      to={href}
      onMouseEnter={start}
      onMouseLeave={stop}
      className="focus-visible:ring-ring block h-full no-underline hover:no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      {body}
    </Link>
  );
}

function AddOptionGrid({ options }: { options: AddOption[] }): JSX.Element {
  // Two columns at most. The page column is capped at max-w-7xl (1280px) and
  // each card spends a fixed 160px on its icon rail, so a third column leaves
  // the copy about 240px and it wraps to four lines. The 1→2 step is a
  // container query rather than a viewport one: the sidebar narrows this
  // column, so viewport width says nothing about the room the cards have.
  return (
    <div className="@4xl/main:grid-cols-2 grid grid-cols-1 gap-4">
      {options.map((option) => (
        <AddOptionCard key={option.title} {...option} />
      ))}
    </div>
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
      icon: <Blocks className="text-foreground size-10" strokeWidth={1.25} />,
      title: "From the catalog",
      description:
        "Pick a reviewed third-party server — Salesforce, Datadog, Linear, Slack, Okta and more.",
    },
    {
      href: routes.mcp.add.remote.href(),
      icon: <Cloud className="text-foreground size-10" strokeWidth={1.25} />,
      title: "Hosted remotely",
      description:
        "Add a server that already runs elsewhere by its URL, proxied or not.",
    },
    ...(isTunneledMcpEnabled
      ? [
          {
            href: routes.mcp.add.tunneled.href(),
            icon: (
              <Cable className="text-foreground size-10" strokeWidth={1.25} />
            ),
            title: "Reachable through a tunnel",
            description:
              "Connect a server running inside your own network through a tunnel.",
          },
        ]
      : []),
  ];

  const gatewayOptions: AddOption[] = [
    {
      onSelect: () => setGatewayDialogOpen(true),
      icon: <Layers className="text-foreground size-10" strokeWidth={1.25} />,
      title: "New gateway",
      description:
        "One address fronting a set of servers. Pick its members after creating it.",
    },
  ];

  const advancedOptions: AddOption[] = [
    {
      href: routes.mcp.add.openapi.href(),
      icon: <FileCode className="text-foreground size-10" strokeWidth={1.25} />,
      title: "From your API",
      description: "Upload an OpenAPI document to generate tools.",
    },
    {
      href: routes.mcp.add.fromSource.href(),
      icon: <Boxes className="text-foreground size-10" strokeWidth={1.25} />,
      title: "From an existing source",
      description:
        "Build a server from an OpenAPI document or function this project already has.",
    },
    ...(isFunctionsEnabled
      ? [
          {
            href: routes.mcp.add.function.href(),
            icon: (
              <Code className="text-foreground size-10" strokeWidth={1.25} />
            ),
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
      {/* SettingsPage stacks its sections at gap-8, and Page.Section already
          carries its own bottom margin — together that leaves a hole between
          the page title and the first group of options. One wrapper with its
          own spacing keeps the page tight without changing the shared
          template for every page that uses it. */}
      <div className="-mt-6 flex flex-col gap-6">
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
                Put several servers behind a single address so people connect
                once instead of per server.
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
        </SettingsSection>
      </div>

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
