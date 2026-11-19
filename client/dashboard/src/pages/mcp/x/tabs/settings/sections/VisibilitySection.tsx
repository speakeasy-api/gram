import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { useReportUnsavedChanges } from "@/components/detail/settings-layout";
import { SETTINGS_GROUPS } from "@/components/detail/settings-groups";
import { ServerUsageConfirmation } from "@/components/mcp-server-usage/ServerUsageConfirmation";
import { useServerUsageConfirmation } from "@/components/mcp-server-usage/useServerUsageConfirmation";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Spinner } from "@/components/ui/Spinner";
import { Text } from "@/components/ui/Text";
import { isKnownEnumValue } from "@/lib/enum";
import { getTunneledMcpServerArgs, mcpServerRouteParam } from "@/lib/sources";
import { cn } from "@/lib/utils";
import { useHostedVisibilityGuard } from "@/pages/mcp/x/hosted/useHostedVisibilityGuard";
import {
  mcpServerTabHref,
  standsInForToolset,
} from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useRoutes } from "@/routes";
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
import { useUpdateTunneledMcpServerMutation } from "@gram/client/react-query/updateTunneledMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { MCP_PUBLIC_ACCESS_SECTION_ID } from "./PublicAccessSection";
import { invalidateTunneledMcpSourceViews } from "./sourceInvalidation";

/** The Availability (on or off) section; readiness links land here. */
export const MCP_VISIBILITY_SECTION_ID = "visibility";

type Option = {
  value: McpServerVisibility;
  label: string;
  description: string;
};

// Every server is either on or off. "Enabled" writes private when the server
// was off; a server that is already on keeps its private or public choice.
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

// Hosted and tunneled servers, once on, also choose who may connect.
const VISIBILITY_OPTIONS: Option[] = [
  {
    value: "private",
    label: "Private",
    description: "Only people and agents with access can connect.",
  },
  {
    value: "public",
    label: "Public",
    description:
      "Anyone can connect anonymously, without signing in. Every tool is open to the public internet.",
  },
];

/**
 * Whether the server is on. Every server kind shows this at the top of
 * General; unproxied servers can't be switched at all. The choice waits for
 * Save, then asks for confirmation, since either way changes who is cut off.
 */
export function AvailabilitySection({
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
  // A public server counts as on; saving "on" leaves it public.
  const current: McpServerVisibility =
    mcpServer.visibility === "public" ? "private" : mcpServer.visibility;
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
  useReportUnsavedChanges(canWrite && dirty);
  const save = () => {
    if (!canWrite || !dirty || updating) return;
    if (hosted) guard.request(draft);
    else setPendingAvailability(draft);
  };

  const currentOption = AVAILABILITY_OPTIONS.find(
    (option) => option.value === current,
  );

  let hint: React.ReactNode = null;
  if (!hasWriteScope) {
    hint = "Only people who can edit MCP servers can change this.";
  } else if (!known) {
    hint = "Refresh the page before changing this server’s availability.";
  }

  // Clients connect straight to the vendor, so Speakeasy has no switch that
  // could stop them; the old status control hid itself here too.
  if (mcpServer.unproxiedMcpServerId) {
    return (
      <SettingsSection id={MCP_VISIBILITY_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>Availability</SettingsSection.Title>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <Text muted small className="block max-w-2xl">
              Clients connect straight to the vendor with the vendor's own
              credentials, so Speakeasy can't turn this server off. To stop
              access, revoke it with the vendor.
            </Text>
          </SettingsSection.Body>
        </SettingsSection.Panel>
      </SettingsSection>
    );
  }

  return (
    <SettingsSection id={MCP_VISIBILITY_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Availability</SettingsSection.Title>
        <SettingsSection.Description>
          Turn the server off to stop every connection without deleting
          anything.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {canWrite ? (
            <OptionRadios
              label="Availability"
              options={AVAILABILITY_OPTIONS}
              value={draft}
              current={current}
              disabled={updating}
              onChange={setDraft}
            />
          ) : (
            <CurrentOption option={currentOption} />
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

/**
 * Who may connect to a hosted or tunneled server that is on: people with
 * access, or anyone. A tunneled server going public also records the tunnel
 * source's consent, in the same Save.
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
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const teamAccessHref = mcpServerTabHref(
    routes,
    mcpServerRouteParam(mcpServer),
    "team-access",
  );
  const allowPublicUpdate = useUpdateTunneledMcpServerMutation();
  const [allowingPublic, setAllowingPublic] = useState(false);
  // Allowing the tunnel to serve anonymous callers is a typed confirmation,
  // as it was on the tunnel source's own page.
  const [confirmingPublic, setConfirmingPublic] = useState(false);
  const [confirmPhrase, setConfirmPhrase] = useState("");
  const pending = updating || allowingPublic;

  const off = mcpServer.visibility === "disabled";
  const current = mcpServer.visibility;
  const known = isKnownEnumValue(
    KnownUpdateMcpServerFormVisibility,
    mcpServer.visibility,
  );

  const [draft, setDraft] = useState<McpServerVisibility>(current);
  useEffect(() => {
    setDraft(current);
  }, [mcpServer.id, current]);

  const dirty = !off && draft !== current;
  useReportUnsavedChanges(canWrite && dirty);
  const goingPublic = dirty && draft === "public";

  const save = async () => {
    if (!canWrite || !dirty || pending) return;
    if (hosted) {
      guard.request(draft);
      return;
    }
    // Public needs the tunnel source's consent, confirmed by typing it.
    if (draft === "public" && isTunneled && !tunneledSource?.allowPublic) {
      if (!tunneledSource) return;
      setConfirmPhrase("");
      setConfirmingPublic(true);
      return;
    }
    updateVisibility(draft);
  };

  const allowPublicThenSave = async () => {
    if (!tunneledSource) return;
    setAllowingPublic(true);
    try {
      await allowPublicUpdate.mutateAsync({
        request: {
          updateTunneledMcpServerForm: {
            id: tunneledSource.id,
            allowPublic: true,
          },
        },
      });
      await invalidateTunneledMcpSourceViews(queryClient);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : "Couldn't allow public access on the tunnel",
      );
      return;
    } finally {
      setAllowingPublic(false);
    }
    setConfirmingPublic(false);
    updateVisibility(draft);
  };

  const describe = (option: Option): React.ReactNode => {
    if (option.value === "private") {
      return (
        <>
          {option.description}{" "}
          <Link
            to={teamAccessHref}
            className="text-foreground underline underline-offset-2"
          >
            Manage who has access
          </Link>
        </>
      );
    }
    if (hosted && guard.publicUnavailable) return "Loading environment data…";
    return hosted ? HOSTED_PUBLIC_DESCRIPTION : option.description;
  };
  const publicUnavailable =
    (hosted && guard.publicUnavailable) || (isTunneled && !tunneledSource);
  const currentOption = VISIBILITY_OPTIONS.find(
    (option) => option.value === current,
  );

  let hint: React.ReactNode = null;
  if (!hasWriteScope) {
    hint = "Only people who can edit MCP servers can change this.";
  } else if (!known) {
    hint = "Refresh the page before changing this server’s visibility.";
  } else if (off) {
    hint = (
      <>
        The server is off.{" "}
        <Link
          to={{ hash: SETTINGS_GROUPS.general.id }}
          className="text-foreground underline underline-offset-2"
        >
          Turn it on under Availability
        </Link>{" "}
        to choose who can connect.
      </>
    );
  }

  return (
    <SettingsSection id={MCP_PUBLIC_ACCESS_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Visibility</SettingsSection.Title>
        <SettingsSection.Description>
          Choose who can connect to this server.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {canWrite && !off ? (
            <OptionRadios
              label="Visibility"
              options={VISIBILITY_OPTIONS}
              value={draft}
              current={current}
              disabled={pending}
              blocked={(option) =>
                option.value === "public" && publicUnavailable
              }
              describe={describe}
              onChange={setDraft}
            />
          ) : off ? (
            <Text muted small className="block max-w-2xl">
              No one can connect while the server is off.
            </Text>
          ) : (
            <CurrentOption option={currentOption} describe={describe} />
          )}
          {goingPublic && isTunneled ? (
            <Alert variant="warning" dismissible={false}>
              Anyone on the internet will be able to call every tool this tunnel
              exposes, with no sign-in. Only make it public if the upstream
              server is safe to expose.
            </Alert>
          ) : null}
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>{hint}</SettingsSection.FooterHint>
          {canWrite && !off ? (
            <SettingsSection.FooterActions>
              <FooterSaveButton
                pending={pending}
                disabled={!dirty || pending}
                onClick={() => void save()}
              />
            </SettingsSection.FooterActions>
          ) : null}
        </SettingsSection.Footer>
      </SettingsSection.Panel>
      {guard.dialogs}
      <ConfirmDialog
        open={confirmingPublic}
        onOpenChange={(open) => {
          if (!allowingPublic) setConfirmingPublic(open);
        }}
        title="Allow anonymous access to this tunnel?"
        description="Anyone on the internet will be able to call every tool this tunnel exposes, with no sign-in."
        confirmLabel="Make public"
        pendingLabel="Making public…"
        isPending={allowingPublic}
        confirmDisabled={confirmPhrase.trim() !== PUBLIC_ACCESS_CONFIRM_PHRASE}
        onConfirm={() => void allowPublicThenSave()}
      >
        <Text small muted className="block">
          Type{" "}
          <span className="font-mono font-medium">
            {PUBLIC_ACCESS_CONFIRM_PHRASE}
          </span>{" "}
          to confirm.
        </Text>
        <Input
          value={confirmPhrase}
          onChange={(value) => setConfirmPhrase(value)}
          placeholder={PUBLIC_ACCESS_CONFIRM_PHRASE}
          disabled={allowingPublic}
          aria-label={`Type ${PUBLIC_ACCESS_CONFIRM_PHRASE} to confirm`}
        />
      </ConfirmDialog>
    </SettingsSection>
  );
}

const PUBLIC_ACCESS_CONFIRM_PHRASE = "ALLOW PUBLIC ACCESS";

function OptionRadios({
  label,
  options,
  value,
  current,
  disabled,
  blocked = () => false,
  describe = (option) => option.description,
  onChange,
}: {
  label: string;
  options: Option[];
  value: McpServerVisibility;
  current: McpServerVisibility;
  disabled: boolean;
  blocked?: (option: Option) => boolean;
  describe?: (option: Option) => React.ReactNode;
  onChange: (value: McpServerVisibility) => void;
}): JSX.Element {
  return (
    <RadioGroup
      aria-label={label}
      value={value}
      onValueChange={(next) => {
        const option = options.find((candidate) => candidate.value === next);
        if (option && !blocked(option)) onChange(option.value);
      }}
      disabled={disabled}
      className="gap-0 divide-y"
    >
      {options.map((option) => {
        const isBlocked = blocked(option);
        // Availability and Visibility share option values on one page, so
        // the group's label keeps each radio's id unique.
        const id = `mcp-${label.toLowerCase()}-${option.value}`;
        return (
          <div
            key={option.value}
            className="flex items-start gap-3 py-3 first:pt-0 last:pb-0"
          >
            <RadioGroupItem
              id={id}
              value={option.value}
              disabled={isBlocked}
              className="mt-0.5"
            />
            <span className="min-w-0 space-y-0.5">
              <label
                htmlFor={id}
                className={cn(
                  "block text-sm font-medium",
                  isBlocked
                    ? "cursor-not-allowed opacity-60"
                    : "cursor-pointer",
                )}
              >
                {option.label}
                {option.value === current ? (
                  <span className="text-muted-foreground font-normal">
                    {" "}
                    · Current
                  </span>
                ) : null}
              </label>
              <Text as="span" muted small className="block max-w-2xl">
                {describe(option)}
              </Text>
            </span>
          </div>
        );
      })}
    </RadioGroup>
  );
}

function CurrentOption({
  option,
  describe = (shown) => shown.description,
}: {
  option: Option | undefined;
  describe?: (option: Option) => React.ReactNode;
}): JSX.Element {
  return (
    <div className="space-y-0.5">
      <Text small className="font-medium">
        {option?.label ?? "Unknown"}
      </Text>
      {option ? (
        <Text muted small className="block max-w-2xl">
          {describe(option)}
        </Text>
      ) : null}
    </div>
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
