import { useState } from "react";
import { Terminal } from "lucide-react";
import {
  DeviceAgentInstallStep,
  DeviceAgentOsPicker,
  type DeviceAgentOs,
} from "@/pages/device-agent/device-agent-setup";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { EnableLoggingSection } from "../enable-logging-section";
import { MarketplaceSection } from "../marketplace-section";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { isOtherPlatformSource } from "../hook-event-sources";
import { MdmRolloutTable } from "../mdm-rollout-table";
import { MdmRolloutRequirements } from "../mdm-rollout-requirements";

interface InstrumentAgentsStepProps {
  onComplete: () => void;
}

export function InstrumentAgentsStep({
  onComplete,
}: InstrumentAgentsStepProps): JSX.Element {
  const [os, setOs] = useState<DeviceAgentOs>("macos");

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <Terminal className="text-foreground h-6 w-6" />
        </div>
      }
      title="Set up observability in other platforms"
      description="Publish your plugin marketplace, install the Speakeasy device agent, and confirm events arrive. The agent enforces the observability plugin and MCP configuration across Cursor, Codex, and the other coding assistants on each machine. Claude Code and Claude Cowork are covered under Set up Anthropic observability."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <EnableLoggingSection index={1} />

        <MarketplaceSection
          index={2}
          description="The device agent installs the observability plugin from your marketplace, and servers you distribute later are published there too."
        />

        <StepSection
          index={3}
          slug="download-installer"
          title="Download installer"
          description="Pick the platform you're installing on. To cover a whole fleet, the MDM rollout in the next step does this on every managed machine."
        >
          <div className="space-y-4">
            <DeviceAgentOsPicker value={os} onChange={setOs} />
            <div className="border-border bg-card border p-5">
              <DeviceAgentInstallStep os={os} />
            </div>
          </div>
        </StepSection>

        <StepSection
          index={4}
          slug="mdm-rollout"
          title="MDM rollout"
          badge="Recommended"
          badgeVariant="success"
          description="Push the installer to every managed machine through your MDM, with identity set centrally so nobody enrolls by hand. Each guide opens in a new tab."
        >
          <div className="space-y-6">
            <MdmRolloutRequirements />
            <MdmRolloutTable />
          </div>
        </StepSection>

        <ConfirmTrafficSection
          index={5}
          description="Run any tool in a coding assistant on a machine with the agent installed. Its events show up here once the hooks are active."
          matchesSource={isOtherPlatformSource}
        />
      </div>
    </StepContainer>
  );
}
