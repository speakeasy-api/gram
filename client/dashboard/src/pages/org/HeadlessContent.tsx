import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { agentProvidersForSurface } from "@/components/agent-providers/agent-providers";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
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
  const [setupOpen, setSetupOpen] = useState(false);
  const openSetup = () => setSetupOpen(true);

  return (
    <div className="relative flex min-h-full w-full justify-center px-8 pt-20 pb-16">
      {/* One centered column: the hero and the agent list read as a single
          block on the mesh rather than two things pushed to opposite edges. */}
      <div className="flex w-full max-w-xl flex-col items-center gap-8 text-center">
        {/* The eyebrow labels the headline, so it sits tight against it
            rather than on the column's own rhythm. */}
        <div className="flex flex-col gap-3">
          <span className="text-eyebrow">Platform MCP</span>
          <h1 className="text-foreground font-display text-[48px] leading-[0.9] font-thin tracking-[-0.045em] lg:text-[68px]">
            Connect your
            <br />
            coding agent
          </h1>
        </div>
        <Text muted className="max-w-md text-base">
          Drive Speakeasy from the agent you already work in — install reviewed
          MCP servers, distribute them, and see what they are doing, without
          leaving your terminal.
        </Text>

        <div className="flex flex-col items-center gap-3">
          <Button size="lg" onClick={openSetup}>
            <Button.Text>Connect your coding agent</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
          <span className="text-eyebrow">~2 minutes</span>
        </div>

        {/* Step one, made real: the same agent list the setup sheet opens on,
            so the page itself is the first move rather than a picture of it. */}
        <div className="border-border bg-card/90 w-full border text-left backdrop-blur-sm">
          <div className="border-border border-b px-5 py-3">
            <p className="text-eyebrow">Choose your agent</p>
          </div>
          <ul>
            {AGENTS.map((agent) => (
              <li key={agent.id}>
                <button
                  type="button"
                  onClick={openSetup}
                  className="border-border hover:bg-background group flex w-full items-center gap-4 border-b px-5 py-3 text-left transition-colors last:border-b-0"
                >
                  {/* The vendor marks clash out of the box — an orange
                      squircle next to a flat black cube next to a line glyph.
                      Desaturated they read as one set; the row's own hover
                      brings the brand color back. */}
                  <span className="bg-background border-border flex h-8 w-8 shrink-0 items-center justify-center border">
                    <AgentProviderIcon
                      source={agent.iconSource}
                      className="h-4 w-4 opacity-80 grayscale transition group-hover:opacity-100 group-hover:grayscale-0"
                    />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="text-foreground block text-sm font-medium">
                      {agent.name}
                    </span>
                    <span className="text-muted-foreground block truncate text-xs">
                      {agent.description}
                    </span>
                  </span>
                  <ChevronRight className="text-muted-foreground group-hover:text-foreground h-4 w-4 shrink-0 transition-colors" />
                </button>
              </li>
            ))}
          </ul>
        </div>

        <ol className="flex flex-wrap justify-center gap-x-8 gap-y-2">
          {STEPS.map((step) => (
            <li key={step.index} className="flex items-baseline gap-2">
              <span className="text-eyebrow text-foreground">{step.index}</span>
              <span className="text-eyebrow">{step.label}</span>
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
