import { InputDialog } from "@/components/input-dialog";
import confetti from "canvas-confetti";
import { SettingsPage, SettingsSection } from "@/components/page-templates";
import { Card } from "@/components/ui/Card";
import { Grid } from "@/components/ui/Grid";
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
  Cable,
  Cloud,
  Code,
  FileCode,
  Layers,
} from "lucide-react";
import type { ReactNode } from "react";
import { useCallback, useEffect, useRef, useState } from "react";
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

// Brand language palette. canvas-confetti parses hex only, so the tokens are
// resolved from CSS at first use (they are hsl()) and cached.
const CONFETTI_TOKENS = [
  "brand-ruby",
  "brand-go",
  "brand-python",
  "brand-swift",
  "brand-java",
  "brand-terraform",
  "brand-unity",
  "brand-php",
  "brand-c",
];

let confettiColorCache: string[] | null = null;

function brandConfettiColors(): string[] {
  if (confettiColorCache) return confettiColorCache;
  const probe = document.createElement("span");
  probe.style.display = "none";
  document.body.appendChild(probe);
  const colors = CONFETTI_TOKENS.map((token) => {
    probe.style.color = `var(--color-${token})`;
    const rgb = getComputedStyle(probe).color.match(/\d+/g);
    if (!rgb) return "#888888";
    return `#${rgb
      .slice(0, 3)
      .map((n) => Number(n).toString(16).padStart(2, "0"))
      .join("")}`;
  });
  probe.remove();
  confettiColorCache = colors;
  return colors;
}

/**
 * Hover burst on the card's icon rail, fired through canvas-confetti so the
 * pieces get real physics — per-particle velocity, drift, gravity and tumble,
 * different on every fire. Sits behind the icon tile: the rail is given
 * `isolate` so `-z-10` lands between the rail's own background and the tile,
 * the same layering the assistants card uses for its brand mesh.
 *
 * The canvas is per-card and only ~160px wide, so the defaults (tuned for a
 * full-screen cannon) are scaled down: slower launch, smaller pieces, and a
 * short life so nothing lingers after the pointer leaves.
 */
function useIconConfetti(): {
  canvasRef: React.RefObject<HTMLCanvasElement | null>;
  fire: () => void;
} {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const fireRef = useRef<confetti.CreateTypes | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    fireRef.current = confetti.create(canvas, { resize: true });
    return () => {
      fireRef.current?.reset();
      fireRef.current = null;
    };
  }, []);

  const fire = useCallback(() => {
    void fireRef.current?.({
      particleCount: 110,
      spread: 360,
      // Tuned for a ~160px canvas: a slow launch with heavy drag keeps the
      // pieces inside the rail long enough to read, where the full-screen
      // defaults would shoot them off-canvas within a few frames.
      startVelocity: 11,
      gravity: 0.55,
      decay: 0.93,
      scalar: 0.6,
      ticks: 160,
      origin: { x: 0.5, y: 0.5 },
      colors: brandConfettiColors(),
      // The library honours the OS setting itself, so there is no separate
      // guard to keep in sync.
      disableForReducedMotion: true,
    });
  }, []);

  return { canvasRef, fire };
}

function AddOptionCard({
  href,
  onSelect,
  icon,
  title,
  description,
}: AddOption): JSX.Element {
  const { canvasRef, fire } = useIconConfetti();
  const body = (
    <Card.Entity
      icon={icon}
      iconRailClassName="isolate"
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
      <div className="h-full" onMouseEnter={fire}>
        {body}
      </div>
    );
  }
  return (
    <Link
      to={href}
      onMouseEnter={fire}
      className="focus-visible:ring-ring block h-full no-underline hover:no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      {body}
    </Link>
  );
}

function AddOptionGrid({ options }: { options: AddOption[] }): JSX.Element {
  return (
    <Grid columns={{ xs: 1, md: 2, "2xl": 3 }} gap={3}>
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
