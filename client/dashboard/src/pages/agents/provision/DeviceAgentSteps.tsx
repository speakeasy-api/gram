import { Label } from "@/components/ui/Label";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useState, type JSX } from "react";
import {
  REVIEW_FILE,
  reviewCommands,
  type DeviceAgentRunMode,
} from "./device-agent";
import { Copyable } from "./StepProvision";
import { WizardStepHeader } from "./WizardChrome";

/** The wizard steps specific to a device agent: its project and its install. */

export function StepDeviceAgentProject({
  projectId,
  onChange,
  disabled,
}: {
  projectId: string;
  onChange: (projectId: string) => void;
  disabled?: boolean;
}): JSX.Element {
  const organization = useOrganization();
  return (
    <div className="space-y-6">
      <WizardStepHeader
        title="Project"
        description="Where this agent's hook events and telemetry are recorded."
      />
      <div className="max-w-xl space-y-2">
        <Label>Project</Label>
        <Select value={projectId} onValueChange={onChange} disabled={disabled}>
          <SelectTrigger aria-label="Project">
            <SelectValue placeholder="Select a project" />
          </SelectTrigger>
          <SelectContent>
            {organization.projects.map((project) => (
              <SelectItem key={project.id} value={project.id}>
                {project.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Text muted small>
          The agent can sync device agent plugins and send hook events for the
          organization, and read this project. It cannot connect to MCP servers.
        </Text>
      </div>
    </div>
  );
}

export function StepProvisionDeviceAgent({
  command,
  commandError,
  minting,
  onRegenerate,
  mode,
  onModeChange,
  canRegenerate,
}: {
  command: string | null;
  commandError: string | null;
  minting: boolean;
  onRegenerate: () => void;
  mode: DeviceAgentRunMode;
  onModeChange: (mode: DeviceAgentRunMode) => void;
  canRegenerate: boolean;
}): JSX.Element {
  const [tab, setTab] = useState("one-line");
  const review = command ? reviewCommands(command) : null;

  return (
    <div className="space-y-5">
      <WizardStepHeader
        title="Install the device agent"
        description="Run this on the Linux host the agent works on. macOS hosts install the signed package through MDM instead."
      />
      <div className="space-y-2">
        <Label>Runs as</Label>
        <SegmentedControl
          value={mode}
          onChange={onModeChange}
          disabled={minting}
          options={[
            {
              value: "ephemeral",
              label: "Ephemeral",
              tooltip: "Sandboxes and CI: sync once, then exit",
            },
            {
              value: "service",
              label: "Persistent",
              tooltip: "Long-lived hosts: run as a background service",
            },
          ]}
        />
        <Text muted small>
          {mode === "ephemeral"
            ? "Syncs once and exits. To sync again, run speakeasyd sync --once; the setup link works only once."
            : "Installs a background service for the account that runs it, and keeps it running after logout. Run it as that account, not as root."}
        </Text>
      </div>
      <div className="border-border border">
        <Tabs value={tab} onValueChange={setTab} className="gap-0">
          <div className="border-border bg-muted/30 border-b px-4">
            <PageTabsList>
              <PageTabsTrigger value="one-line">One-line setup</PageTabsTrigger>
              <PageTabsTrigger value="review">Review first</PageTabsTrigger>
            </PageTabsList>
          </div>
          <TabsContent
            value="one-line"
            forceMount
            className="data-[state=inactive]:hidden"
          >
            <div className="space-y-4 p-4">
              {command ? (
                <Copyable value={command} label="setup command" />
              ) : (
                <Text muted small>
                  {minting
                    ? "Preparing the setup command…"
                    : "No setup command yet."}
                </Text>
              )}
              <ol className="space-y-1 text-sm">
                {[
                  "Downloads and verifies the latest device agent over HTTPS.",
                  "Writes the agent's key to /etc/speakeasy/managed.json, readable only by this account.",
                  mode === "ephemeral"
                    ? "Syncs once, then exits."
                    : "Installs and starts the background service.",
                ].map((line, index) => (
                  <li key={line} className="flex gap-3">
                    <span className="text-muted-foreground font-mono text-xs">
                      {String(index + 1).padStart(2, "0")}
                    </span>
                    <span>{line}</span>
                  </li>
                ))}
              </ol>
            </div>
          </TabsContent>
          <TabsContent
            value="review"
            forceMount
            className="data-[state=inactive]:hidden"
          >
            <div className="space-y-4 p-4">
              {review ? (
                <>
                  <Copyable value={review.fetch} label="download command" />
                  <Text muted small>
                    Read {REVIEW_FILE}, then run it. The file carries the key,
                    so it is readable only by you, and this deletes it once it
                    has run, whether or not the install succeeded.
                  </Text>
                  <Copyable value={review.run} label="run command" />
                </>
              ) : (
                <Text muted small>
                  {minting
                    ? "Preparing the setup command…"
                    : "No setup command yet."}
                </Text>
              )}
            </div>
          </TabsContent>
        </Tabs>
      </div>
      <Text muted small>
        The setup link is single-use and expires in 15 minutes, whichever form
        uses it.{" "}
        <button
          type="button"
          className="underline"
          disabled={minting || !canRegenerate}
          onClick={onRegenerate}
        >
          Regenerate
        </button>
      </Text>
      {commandError && (
        <Text role="alert" small>
          {commandError}
        </Text>
      )}
    </div>
  );
}
