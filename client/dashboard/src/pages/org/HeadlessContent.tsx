import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { agentProvidersForSurface } from "@/components/agent-providers/agent-providers";
import { GramIcon } from "@/components/gram-logo/variants/icon";
import { ModeSwitchStarfield } from "@/components/mode-switch-starfield";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { SourceSurface } from "@gram/client/models/components/startonboardingrequestbody.js";
import { ArrowRight, ChevronRight } from "lucide-react";
import { useState } from "react";
import { PlatformMCPOnboardingContent } from "./PlatformMCP";

// The agents the Platform MCP walkthrough supports, with the same brand marks
// and copy the setup wizard uses.
const AGENTS = agentProvidersForSurface("plugins").filter(
  (provider) => provider.available,
);

const STEPS = [
  { index: "01", label: "Pick your agent" },
  { index: "02", label: "Run one command" },
  { index: "03", label: "Approve access" },
];

/**
 * What headless mode shows below the strip: no page chrome, no breadcrumb bar,
 * no onboarding banner — one hero whose whole job is getting an agent
 * connected. The mesh behind the app chrome shows through, so this surface
 * paints no background of its own.
 */
export function HeadlessContent(): JSX.Element {
  // Same gate the standalone Platform MCP page applies: connecting an agent is
  // an organization-admin action, so members never see the flow offered.
  return (
    <RequireScope scope="org:admin" level="page">
      <HeadlessHero />
    </RequireScope>
  );
}

function HeadlessHero(): JSX.Element {
  const [setupOpen, setSetupOpen] = useState(false);
  const openSetup = () => setSetupOpen(true);

  return (
    <div className="bg-surface-tertiary-fixed-dark relative flex min-h-full w-full justify-center px-8 pt-20 pb-16">
      {/* The same screensaver the tab grid runs, held at rest behind the hero. */}
      <ModeSwitchStarfield direction={1} className="absolute" />
      {/* One centered column: the hero and the agent list read as a single
          block on the mesh rather than two things pushed to opposite edges. */}
      {/* z-10: the starfield canvas is positioned, so unpositioned content
          would paint beneath it and get crossed by the streaks. */}
      <div className="relative z-10 flex w-full max-w-xl flex-col items-center gap-8 text-center">
        {/* The eyebrow labels the headline, so it sits tight against it
            rather than on the column's own rhythm. */}
        <div className="flex flex-col items-center gap-3">
          {/* The mark, not the wordmark. Its paths ship a fixed ink fill, so
              they are re-pointed at currentColor to sit on the dark surface. */}
          <div
            className="mb-2 [&>svg]:h-9 [&>svg]:w-auto [&_path]:fill-current"
            style={{ color: "var(--text-default-fixed-light)" }}
          >
            <GramIcon />
          </div>
          <span
            className="text-eyebrow"
            style={{ color: "var(--text-muted-fixed-light)" }}
          >
            Platform MCP
          </span>
          <h1 className="text-default-fixed-light font-display text-[48px] leading-[0.9] font-thin tracking-[-0.045em] lg:text-[68px]">
            Connect your
            <br />
            coding agent
          </h1>
        </div>
        <p
          className="max-w-md text-base leading-relaxed"
          style={{ color: "var(--text-muted-fixed-light)" }}
        >
          Drive Speakeasy from the agent you already work in — install reviewed
          MCP servers, distribute them, and see what they are doing, without
          leaving your terminal.
        </p>

        <div className="flex flex-col items-center gap-3">
          <Button
            size="lg"
            onClick={openSetup}
            // Inverted for the ink hero: the default primary is ink on ink, so
            // it reads as invisible until hover.
            className="bg-surface-primary-fixed-light hover:bg-surface-tertiary-fixed-light [&_*]:text-default-fixed-dark"
          >
            <Button.Text>Connect your coding agent</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
          <span
            className="text-eyebrow"
            style={{ color: "var(--text-muted-fixed-light)" }}
          >
            ~2 minutes
          </span>
        </div>

        {/* Step one, made real: the same agent list the setup sheet opens on,
            so the page itself is the first move rather than a picture of it. */}
        <div className="border-neutral-softest w-full border bg-white/5 text-left backdrop-blur-sm">
          <div className="border-neutral-softest border-b px-5 py-3">
            <p className="text-eyebrow text-muted-fixed-light">
              Choose your agent
            </p>
          </div>
          <ul>
            {AGENTS.map((agent) => (
              <li key={agent.id}>
                <button
                  type="button"
                  onClick={openSetup}
                  className="border-neutral-softest group flex w-full items-center gap-4 border-b px-5 py-3 text-left transition-colors last:border-b-0 hover:bg-white/10"
                >
                  {/* The vendor marks clash out of the box — an orange
                      squircle next to a flat black cube next to a line glyph.
                      Desaturated they read as one set; the row's own hover
                      brings the brand color back. */}
                  <span className="border-neutral-softest flex h-8 w-8 shrink-0 items-center justify-center border bg-white/5">
                    <AgentProviderIcon
                      source={agent.iconSource}
                      className="h-4 w-4 opacity-80 grayscale transition group-hover:opacity-100 group-hover:grayscale-0"
                    />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="text-default-fixed-light block text-sm font-medium">
                      {agent.name}
                    </span>
                    <span className="text-muted-fixed-light block truncate text-xs">
                      {agent.description}
                    </span>
                  </span>
                  <ChevronRight className="text-muted-fixed-light group-hover:text-default-fixed-light h-4 w-4 shrink-0 transition-colors" />
                </button>
              </li>
            ))}
          </ul>
        </div>

        <ol className="flex flex-wrap justify-center gap-x-8 gap-y-2">
          {STEPS.map((step) => (
            <li key={step.index} className="flex items-baseline gap-2">
              <span
                className="text-eyebrow"
                style={{ color: "var(--text-default-fixed-light)" }}
              >
                {step.index}
              </span>
              <span
                className="text-eyebrow"
                style={{ color: "var(--text-muted-fixed-light)" }}
              >
                {step.label}
              </span>
            </li>
          ))}
        </ol>
      </div>

      {/* The existing walkthrough, driven entirely as sheets: agent picker →
          install method → step-by-step setup. */}
      <PlatformMCPOnboardingContent
        sheetOnly
        setupOpen={setupOpen}
        onSetupOpenChange={setSetupOpen}
        initialSourceSurface={SourceSurface.PlatformMcpSettings}
      />
    </div>
  );
}
