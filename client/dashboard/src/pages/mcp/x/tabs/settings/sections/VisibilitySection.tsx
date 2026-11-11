import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { ServerUsageConfirmation } from "@/components/mcp-server-usage/ServerUsageConfirmation";
import { useServerUsageConfirmation } from "@/components/mcp-server-usage/useServerUsageConfirmation";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Spinner } from "@/components/ui/Spinner";
import { Text } from "@/components/ui/Text";
import { isKnownEnumValue } from "@/lib/enum";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import { cn } from "@/lib/utils";
import { useHostedVisibilityGuard } from "@/pages/mcp/x/hosted/useHostedVisibilityGuard";
import { standsInForToolset } from "@/pages/mcp/x/MCPServerDetailsRouting";
import {
  HOSTED_PUBLIC_DESCRIPTION,
  useMcpServerVisibilityUpdate,
} from "@/pages/mcp/x/mcpServerVisibility";
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import { UpdateMcpServerFormVisibility as KnownUpdateMcpServerFormVisibility } from "@gram/client/models/components/updatemcpserverform.js";
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { MCP_PUBLIC_ACCESS_SECTION_ID } from "./PublicAccessSection";

export const MCP_VISIBILITY_SECTION_ID = "visibility";

type Option = {
  value: McpServerVisibility;
  label: string;
  description: string;
};

// Remote and unproxied servers are either on or off.
const AVAILABILITY_OPTIONS: Option[] = [
  {
    value: "private",
    label: "Enabled",
    description: "People and agents with access can connect and call tools.",
  },
  {
    value: "disabled",
    label: "Disabled",
    description: "The server is offline. No one can connect to it.",
  },
];

const DISABLED_OPTION: Option = {
  value: "disabled",
  label: "Disabled",
  description: "The server is offline. No one can connect to it.",
};
const PRIVATE_OPTION: Option = {
  value: "private",
  label: "Private",
  description: "Only people and agents with access can connect.",
};
const PUBLIC_OPTION: Option = {
  value: "public",
  label: "Public",
  description:
    "Anyone can connect anonymously, without signing in. Every tool is open to the public internet.",
};

/**
 * Who can reach the server: off, people with access, or anyone. Remote and
 * unproxied servers only switch on and off. The choice waits for Save, then
 * asks for confirmation wherever the change could cut clients off.
 */
export function VisibilitySection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const { hasWriteScope, canWrite, updateVisibility, updating } =
    useMcpServerVisibilityUpdate(mcpServer);
  const hosted = standsInForToolset(mcpServer);
  const guard = useHostedVisibilityGuard({
    server: mcpServer,
    hosted,
    apply: updateVisibility,
    isApplying: updating,
  });
  const isTunneled = !!mcpServer.tunneledMcpServerId;
  const { data: tunneledSource } = useGetTunneledMcpServer(
    getTunneledMcpServerArgs(mcpServer.tunneledMcpServerId ?? ""),
    undefined,
    { enabled: isTunneled },
  );
  // Public needs the tunnel source owner's consent first (double opt-in).
  const publicBlocked = isTunneled && !(tunneledSource?.allowPublic ?? false);

  const onOff = !hosted && !isTunneled;
  const options = onOff
    ? AVAILABILITY_OPTIONS
    : [DISABLED_OPTION, PRIVATE_OPTION, PUBLIC_OPTION];
  // An on/off server that is public counts as on; saving "on" leaves it public.
  const current: McpServerVisibility =
    onOff && mcpServer.visibility === "public"
      ? "private"
      : mcpServer.visibility;
  const known = isKnownEnumValue(
    KnownUpdateMcpServerFormVisibility,
    mcpServer.visibility,
  );

  const [draft, setDraft] = useState<McpServerVisibility>(current);
  useEffect(() => {
    setDraft(current);
  }, [mcpServer.id, current]);

  // Disabling cuts clients off, and enabling exposes the tools again, so both
  // ask first. Hosted servers keep the toolset page's own confirmations.
  const [pendingAvailability, setPendingAvailability] =
    useState<McpServerVisibility | null>(null);

  const dirty = draft !== current;
  const save = () => {
    if (!canWrite || !dirty || updating) return;
    if (hosted) guard.request(draft);
    else if (draft === "disabled" || current === "disabled")
      setPendingAvailability(draft);
    else updateVisibility(draft);
  };

  const describe = (option: Option): string => {
    if (option.value !== "public") return option.description;
    if (publicBlocked)
      return "Allow public access on the tunnel source first. Anyone could then connect without signing in.";
    if (hosted && guard.publicUnavailable) return "Loading environment data…";
    return hosted ? HOSTED_PUBLIC_DESCRIPTION : option.description;
  };
  const optionBlocked = (option: Option) =>
    option.value === "public" &&
    (publicBlocked || (hosted && guard.publicUnavailable));

  const title = onOff ? "Availability" : "Visibility";
  const currentOption = options.find((option) => option.value === current);

  let hint: React.ReactNode = null;
  if (!hasWriteScope) {
    hint = "Only people who can edit MCP servers can change this.";
  } else if (!known) {
    hint = `Refresh the page before changing this server’s ${title.toLowerCase()}.`;
  } else if (publicBlocked) {
    hint = (
      <Link
        to={{ hash: MCP_PUBLIC_ACCESS_SECTION_ID }}
        className="text-foreground underline underline-offset-2"
      >
        Allow public access below
      </Link>
    );
  }

  return (
    <SettingsSection id={MCP_VISIBILITY_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>{title}</SettingsSection.Title>
        <SettingsSection.Description>
          {onOff
            ? "Turn the server off to stop every connection without deleting anything."
            : "Choose who can connect to this server."}
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {canWrite ? (
            <RadioGroup
              aria-label={title}
              value={draft}
              onValueChange={(value) => {
                const next = options.find((option) => option.value === value);
                if (next && !optionBlocked(next)) setDraft(next.value);
              }}
              disabled={updating}
              className="gap-0 divide-y"
            >
              {options.map((option) => {
                const blocked = optionBlocked(option);
                const id = `mcp-visibility-${option.value}`;
                return (
                  <label
                    key={option.value}
                    htmlFor={id}
                    className={cn(
                      "flex items-start gap-3 py-3 first:pt-0 last:pb-0",
                      blocked ? "cursor-not-allowed" : "cursor-pointer",
                    )}
                  >
                    <RadioGroupItem
                      id={id}
                      value={option.value}
                      disabled={blocked}
                      className="mt-0.5"
                    />
                    <span className="min-w-0 space-y-0.5">
                      <Text
                        as="span"
                        small
                        className={cn(
                          "block font-medium",
                          blocked && "opacity-60",
                        )}
                      >
                        {option.label}
                        {option.value === current ? (
                          <span className="text-muted-foreground font-normal">
                            {" "}
                            · Current
                          </span>
                        ) : null}
                      </Text>
                      <Text as="span" muted small className="block max-w-2xl">
                        {describe(option)}
                      </Text>
                    </span>
                  </label>
                );
              })}
            </RadioGroup>
          ) : (
            <div className="space-y-0.5">
              <Text small className="font-medium">
                {currentOption?.label ?? "Unknown"}
              </Text>
              {currentOption ? (
                <Text muted small className="block max-w-2xl">
                  {describe(currentOption)}
                </Text>
              ) : null}
            </div>
          )}
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>{hint}</SettingsSection.FooterHint>
          {canWrite ? (
            <SettingsSection.FooterActions>
              <FooterSaveButton
                pending={updating}
                disabled={!dirty || updating}
                onClick={save}
              />
            </SettingsSection.FooterActions>
          ) : null}
        </SettingsSection.Footer>
      </SettingsSection.Panel>
      {guard.dialogs}
      <ServerAvailabilityDialog
        mcpServer={mcpServer}
        targetVisibility={pendingAvailability}
        isLoading={updating}
        onClose={() => setPendingAvailability(null)}
        onConfirm={() => {
          if (pendingAvailability) updateVisibility(pendingAvailability);
          setPendingAvailability(null);
        }}
      />
    </SettingsSection>
  );
}

function ServerAvailabilityDialog({
  mcpServer,
  targetVisibility,
  isLoading,
  onClose,
  onConfirm,
}: {
  mcpServer: McpServer;
  targetVisibility: McpServerVisibility | null;
  isLoading: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const isOpen = targetVisibility != null;
  // Keep the last target through the close animation so the dialog doesn't
  // switch to its enable layout while it fades out.
  const [lastTarget, setLastTarget] = useState(targetVisibility);
  if (targetVisibility != null && targetVisibility !== lastTarget) {
    setLastTarget(targetVisibility);
  }
  const enabling = (targetVisibility ?? lastTarget) !== "disabled";
  // Only disabling cuts clients off; enabling needs no usage check.
  const usage = useServerUsageConfirmation({
    mcpServerId: mcpServer.id,
    serverName: mcpServer.name,
    open: isOpen && !enabling,
  });
  let title = "Disable MCP server?";
  let message =
    "You are about to disable this MCP server. Users will no longer be able to connect to it. Continue?";

  if (enabling) {
    title = "Enable MCP server?";
    message =
      "You are about to enable this MCP server. Users will be able to connect to it and perform tool calls. Continue?";
  }

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className={enabling ? "max-w-md" : "max-w-2xl!"}>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{message}</Dialog.Description>
        </Dialog.Header>
        {!enabling && (
          <ServerUsageConfirmation state={usage} disabled={isLoading} />
        )}
        <Dialog.Footer>
          <Button variant="secondary" disabled={isLoading} onClick={onClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant={enabling ? "primary" : "destructive-primary"}
            disabled={isLoading || (!enabling && !usage.canConfirm)}
            onClick={onConfirm}
          >
            {isLoading && (
              <Button.LeftIcon>
                <Spinner className="mr-0" />
              </Button.LeftIcon>
            )}
            <Button.Text>{isLoading ? "Saving" : "Continue"}</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
