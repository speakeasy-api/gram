import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
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
import { useUpdateUserSessionIssuerMutation } from "@gram/client/react-query/updateUserSessionIssuer.js";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { toast } from "sonner";

// The three modes an operator can actually choose. The read side of the API
// can also return "reporting" — the default for an issuer that has never
// been configured — but it is not writable and is deliberately not shown as
// a fourth option: it is as permissive as "open" while it is on, so naming
// it as a choice would put a reassuring label on the least restrictive
// state. An unconfigured issuer therefore renders with nothing selected,
// and the confirmation dialog on the first save is where the recording
// state and its irreversibility are explained. See
// server/internal/usersessions/cimd/admission/mode.go.
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
}: {
  userSessionIssuer: UserSessionIssuer;
}): JSX.Element {
  const queryClient = useQueryClient();
  const fieldId = useId();
  const effectiveMode = userSessionIssuer.clientIdMetadataAdmissionMode;

  // "reporting" is only ever returned for an issuer whose mode was never
  // set, and it cannot be written back — so choosing anything here is a
  // one-way door out of the unconfigured state.
  const unconfigured = effectiveMode === "reporting";

  const [draftMode, setDraftMode] = useState<WritableMode | null>(null);
  // Holds the mode the confirmation dialog is asking about. Captured when
  // the dialog opens so a background refetch that clears the draft cannot
  // empty the dialog's copy or disarm its confirm button mid-read.
  const [pendingMode, setPendingMode] = useState<WritableMode | null>(null);

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
    if (unconfigured) {
      setPendingMode(draftMode);
      return;
    }
    save(draftMode);
  };

  return (
    <Field data-invalid={update.isError ? true : undefined}>
      {/* No htmlFor: a group label must not target one option, or clicking
          the heading silently arms that choice. Names the group instead. */}
      <FieldLabel id={`${fieldId}-label`}>Client Admission</FieldLabel>

      <RadioGroup
        aria-labelledby={`${fieldId}-label`}
        value={selectedMode ?? ""}
        onValueChange={(next) => setDraftMode(next as WritableMode)}
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
          the internet. The consent screen and redirect origin-binding become
          the only guardrails on who can start an authorization flow.
        </Alert>
      )}

      <FieldDescription>
        Which MCP clients may authenticate using a Client ID Metadata Document
        (CIMD).
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

      {pendingMode && (
        <ConfirmFirstPolicyDialog
          mode={pendingMode}
          onCancel={() => setPendingMode(null)}
          onConfirm={(mode) => {
            setPendingMode(null);
            save(mode);
          }}
        />
      )}
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

  return (
    <div
      className={cn(
        "grid grid-cols-[auto_1fr] items-center gap-x-3 rounded-lg border p-3.5 transition-colors",
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

function ConfirmFirstPolicyDialog({
  mode,
  onCancel,
  onConfirm,
}: {
  mode: WritableMode;
  onCancel: () => void;
  onConfirm: (mode: WritableMode) => void;
}) {
  const title = MODE_OPTIONS.find((option) => option.value === mode)?.title;

  return (
    <Dialog open onOpenChange={onCancel}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Set client admission policy</Dialog.Title>
          <Dialog.Description>
            This issuer will use <strong>{title}</strong> from now on.
          </Dialog.Description>
        </Dialog.Header>

        <Alert variant="warning" dismissible={false}>
          Gram is currently recording admission decisions without enforcing
          them. Setting a policy is permanent: you can switch between Known
          clients, Open, and Disabled at any time afterward, but this issuer
          cannot return to recording.
        </Alert>

        <Dialog.Footer>
          <Button variant="secondary" onClick={onCancel}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button variant="primary" onClick={() => onConfirm(mode)}>
            <Button.Text>Set policy</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
