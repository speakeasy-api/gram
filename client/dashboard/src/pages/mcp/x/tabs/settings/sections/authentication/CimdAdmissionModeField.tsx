import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Icon } from "@/components/ui/Icon";
import { Label } from "@/components/ui/Label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { UpdateUserSessionIssuerFormClientIdMetadataAdmissionMode as WritableMode } from "@gram/client/models/components/updateusersessionissuerform.js";
import { useCimdClientPresets } from "@gram/client/react-query/cimdClientPresets.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { useUpdateUserSessionIssuerMutation } from "@gram/client/react-query/updateUserSessionIssuer.js";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useId, useState, type MouseEvent } from "react";
import { toast } from "sonner";

// The three modes an operator can actually choose, and between them every
// mode an issuer can be in: "open" is what an issuer carries unless someone
// changes it. The read side of the API can also return "reporting", a legacy
// value that admits exactly what "open" admits. It is not writable and is
// deliberately not offered as a fourth option, so an issuer still storing it
// renders with nothing selected.
const MODE_OPTIONS: {
  value: WritableMode;
  title: string;
  description: string;
}[] = [
  {
    value: WritableMode.Presets,
    title: "Known clients (recommended)",
    // Deliberately not "custom URLs you add below": the custom URL list only
    // renders in the modes that consult it, so "below" would point at
    // nothing for an issuer currently on Open or Disabled.
    description:
      "Allow well-known MCP clients verified by Gram, plus any custom URLs configured on this issuer.",
  },
  {
    value: WritableMode.Open,
    title: "Open",
    description:
      "Allow any spec-valid CIMD client. Documents are accepted from any origin on the internet.",
  },
  {
    value: WritableMode.Disabled,
    title: "Disabled",
    description:
      "Reject all CIMD clients. Gram stops advertising CIMD support for this issuer, so clients fall back to dynamic registration.",
  },
];

export function CimdAdmissionModeField({
  userSessionIssuer,
  onDraftModeChange,
}: {
  userSessionIssuer: UserSessionIssuer;
  /**
   * Publishes each unsaved selection so a sibling field can render against
   * it. The custom-URL list belongs to the modes that consult it, and an
   * operator moving an issuer onto "Known clients" needs to stage those URLs
   * before the switch takes effect, not after.
   */
  onDraftModeChange?: (mode: WritableMode) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const fieldId = useId();
  const effectiveMode = userSessionIssuer.clientIdMetadataAdmissionMode;

  // "reporting" cannot be written back, so there is no option to select for
  // it and the group renders with nothing chosen. Only a row stored before
  // "open" became the written default can still read back as it, and saving
  // any mode leaves that state for good.
  const unconfigured = effectiveMode === "reporting";

  const [draftMode, setDraftMode] = useState<WritableMode | null>(null);

  // Resync on the mode value, NOT the issuer object: every save in this
  // panel invalidates the issuer query, so keying on the object would
  // discard an unsaved selection whenever a sibling field saves. The issuer
  // id is included so a reused instance never carries a draft across
  // issuers that happen to share a mode.
  useEffect(() => {
    setDraftMode(null);
  }, [effectiveMode, userSessionIssuer.id]);

  const selectedMode = draftMode ?? (unconfigured ? null : effectiveMode);
  const dirty = draftMode !== null && draftMode !== effectiveMode;

  const update = useUpdateUserSessionIssuerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllUserSessionIssuer(queryClient, { refetchType: "all" }),
        // Also invalidate MCP server and remote session client queries so the
        // sidebar readiness bar refreshes (AGE-3279).
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Client admission policy updated");
    },
    // No error toast: the failure is already rendered inline as a
    // FieldError below, and double-reporting it reads as two failures.
  });

  const save = (mode: WritableMode) => {
    update.mutate({
      request: {
        updateUserSessionIssuerForm: {
          id: userSessionIssuer.id,
          clientIdMetadataAdmissionMode: mode,
        },
      },
    });
  };

  const handleSave = () => {
    if (!draftMode) return;
    save(draftMode);
  };

  return (
    <Field data-invalid={update.isError ? true : undefined}>
      {/* No htmlFor: a group label must not target one option, or clicking
          the heading silently arms that choice. Names the group instead. */}
      <FieldLabel id={`${fieldId}-label`}>CIMD Client Admission</FieldLabel>

      <RadioGroup
        aria-labelledby={`${fieldId}-label`}
        value={selectedMode ?? ""}
        onValueChange={(next) => {
          setDraftMode(next as WritableMode);
          onDraftModeChange?.(next as WritableMode);
        }}
        className="space-y-2.5"
      >
        {MODE_OPTIONS.map((option) => (
          <ModeOptionCard
            key={option.value}
            id={`${fieldId}-${option.value}`}
            option={option}
            selected={selectedMode === option.value}
          />
        ))}
      </RadioGroup>

      {selectedMode === WritableMode.Open && (
        <Alert variant="warning" dismissible={false}>
          Open admission accepts client metadata documents from any origin on
          the internet. Any valid CIMD client can reach the consent screen, so
          users must review it before approving an authorization flow.
        </Alert>
      )}

      <FieldDescription>
        Which MCP clients may authenticate using a Client ID Metadata Document
        (CIMD). This does not restrict clients that lack CIMD support: they
        register through Dynamic Client Registration (DCR), which stays enabled
        and open to any client whatever you choose here.
      </FieldDescription>

      <div className="flex">
        {/* Render-function form: RequireScope's loading branch applies only
            pointer-events-none, so a keyboard user could still fire the
            mutation while grants are in flight. */}
        <RequireScope scope="project:write" level="component">
          {({ disabled }) => (
            <Button
              variant="primary"
              size="md"
              disabled={disabled || !dirty || update.isPending}
              onClick={handleSave}
            >
              {update.isPending && (
                <Button.LeftIcon>
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>Save</Button.Text>
            </Button>
          )}
        </RequireScope>
      </div>

      {update.isError && <FieldError>{update.error.message}</FieldError>}
    </Field>
  );
}

function ModeOptionCard({
  id,
  option,
  selected,
}: {
  id: string;
  option: (typeof MODE_OPTIONS)[number];
  selected: boolean;
}) {
  // Radix renders the item as a <button role="radio">, which takes no
  // accessible name from a wrapping <label> — name it explicitly.
  const labelId = `${id}-label`;
  const descriptionId = `${id}-description`;

  // The whole card reads as the target, so the whole card selects. Clicks
  // that started on a button are ignored, or opening the presets popover
  // would silently arm this mode too. Mouse affordance only: the radio group
  // already handles keyboard selection, and the radio stays the accessible
  // control.
  const selectFromCard = (event: MouseEvent<HTMLDivElement>) => {
    if (event.target instanceof Element && event.target.closest("button")) {
      return;
    }
    document.getElementById(id)?.click();
  };

  return (
    <div
      onClick={selectFromCard}
      className={cn(
        "grid cursor-pointer grid-cols-[auto_1fr] items-center gap-x-3 rounded-lg border p-3.5 transition-colors",
        selected ? "border-foreground bg-muted/40" : "border-border",
      )}
    >
      <RadioGroupItem
        value={option.value}
        id={id}
        aria-labelledby={labelId}
        aria-describedby={descriptionId}
      />
      <Label
        id={labelId}
        htmlFor={id}
        className="cursor-pointer text-sm font-medium"
      >
        {option.title}
      </Label>
      <div
        id={descriptionId}
        className="text-muted-foreground col-start-2 mt-1.5 text-xs"
      >
        {option.description}
        {option.value === WritableMode.Presets && <KnownClientsPopover />}
      </div>
    </div>
  );
}

// A click-triggered Popover rather than a Tooltip: this is the only place a
// user can find out what "Known clients" covers, and the URLs need to be
// readable, selectable, and reachable on touch devices.
function KnownClientsPopover() {
  const { data, isLoading, isError } = useCimdClientPresets();
  const enabled = (data?.items ?? []).filter((preset) => preset.enabled);

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground ml-1 inline-flex cursor-pointer items-center gap-1 underline underline-offset-2"
        >
          <Icon name="info" className="size-3" />
          What's included?
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-96">
        <Text variant="body" className="font-medium">
          Clients verified by Gram
        </Text>
        <Text muted small className="mt-1">
          Gram maintains this list; newly verified vendors may be added over
          time. Some entries match a family of URLs rather than one exact
          address.
        </Text>
        <KnownClientsList
          isLoading={isLoading}
          isError={isError}
          presets={enabled}
        />
      </PopoverContent>
    </Popover>
  );
}

function KnownClientsList({
  isLoading,
  isError,
  presets,
}: {
  isLoading: boolean;
  isError: boolean;
  presets: { clientIdMetadataUri: string; displayName: string }[];
}) {
  if (isLoading) {
    return (
      <Text muted small className="mt-3 block">
        Loading verified clients…
      </Text>
    );
  }

  if (isError) {
    return (
      <Text muted small className="mt-3 block">
        Could not load the verified client list.
      </Text>
    );
  }

  if (presets.length === 0) {
    return (
      <Text muted small className="mt-3 block">
        No verified clients are currently enabled.
      </Text>
    );
  }

  return (
    <ul className="mt-3 max-h-64 space-y-2 overflow-y-auto">
      {presets.map((preset) => (
        <li key={preset.clientIdMetadataUri}>
          <Text small className="block font-medium">
            {preset.displayName}
          </Text>
          <Text muted className="block font-mono text-xs break-all">
            {preset.clientIdMetadataUri}
          </Text>
        </li>
      ))}
    </ul>
  );
}
