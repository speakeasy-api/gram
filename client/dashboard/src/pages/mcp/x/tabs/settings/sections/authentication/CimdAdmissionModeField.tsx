import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { FieldError } from "@/components/ui/Field";
import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Text } from "@/components/ui/Text";
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
import { useEffect, useId, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { AllowedClientsDialog } from "./AllowedClientsDialog";
import { AuthRow, ExplainerDialog, RowSave } from "./AuthRow";

// The three WRITABLE modes. "open" is what an issuer carries unless someone
// changes it, so it is what a newly created or never-configured issuer shows
// as selected. The read side of the API can also return "reporting", a
// legacy value that admits exactly what "open" admits. It is not writable
// and is deliberately not offered as a fourth option, so an issuer still
// storing it renders with nothing selected.
const MODE_OPTIONS: {
  value: WritableMode;
  label: string;
  /** Shown only while this option is the selection. */
  explanation: string;
}[] = [
  {
    value: WritableMode.Presets,
    label: "Verified clients",
    // Deliberately not "the URLs you add below": the custom URL list only
    // renders in the modes that consult it, so "below" would point at
    // nothing for an issuer currently on Any or Off.
    explanation:
      "Clients Speakeasy has checked, plus any document URLs you allow yourself.",
  },
  {
    value: WritableMode.Open,
    label: "Any client",
    explanation:
      "Any client with a valid document, hosted anywhere on the internet. Nobody vets it first — each user decides at the consent screen.",
  },
  {
    value: WritableMode.Disabled,
    label: "Off",
    explanation:
      "No client connects this way. Speakeasy stops advertising it, so clients register themselves instead.",
  },
];

export function CimdAdmissionModeField({
  userSessionIssuer,
  onDraftModeChange,
  children,
}: {
  userSessionIssuer: UserSessionIssuer;
  /**
   * Publishes each unsaved selection so a sibling field can render against
   * it. The custom-URL list belongs to the modes that consult it, and an
   * operator moving an issuer onto "Verified apps only" needs to stage those
   * URLs before the switch takes effect, not after.
   */
  onDraftModeChange?: (mode: WritableMode) => void;
  /** The custom-URL list, rendered only in the modes that consult it. */
  children?: ReactNode;
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

  const explanation = MODE_OPTIONS.find(
    (option) => option.value === selectedMode,
  )?.explanation;

  // The allowlist only means anything in the mode that consults it, and it
  // follows the SELECTION rather than the saved value: an operator moving
  // onto "Verified clients" needs to stage those URLs before the switch
  // takes effect, not after.
  const admitsCustomUrls = selectedMode === WritableMode.Presets;

  return (
    <AuthRow
      label="Client access"
      hint={
        <>
          Which MCP clients may identify themselves by URL.
          <ExplainerDialog title="Client ID Metadata Documents (CIMD)">
            <Text muted small className="block">
              A client can host a small public file — its name, logo and where
              it sends users after sign-in — and hand Speakeasy that URL instead
              of registering first. This setting decides whose files Speakeasy
              accepts.
            </Text>
            <Text muted small className="block">
              Clients that publish no such file register themselves
              automatically (Dynamic Client Registration). That path stays open
              to every client no matter what you pick here.
            </Text>
          </ExplainerDialog>
        </>
      }
    >
      <RadioGroup
        aria-label="Client access"
        value={selectedMode ?? ""}
        onValueChange={(next) => {
          setDraftMode(next as WritableMode);
          onDraftModeChange?.(next as WritableMode);
        }}
        className="flex flex-wrap items-center gap-x-6 gap-y-2"
      >
        {MODE_OPTIONS.map((option) => (
          <div key={option.value} className="flex items-center gap-2">
            <RadioGroupItem
              value={option.value}
              id={`${fieldId}-${option.value}`}
            />
            <Label
              htmlFor={`${fieldId}-${option.value}`}
              className="cursor-pointer text-sm"
            >
              {option.label}
            </Label>
          </div>
        ))}
      </RadioGroup>

      {explanation && (
        <Text muted small className="block">
          {explanation}
        </Text>
      )}

      {/* A link on the explanation line, not a button stacked above Save:
          two buttons in one row read as a choice between them, when only
          one of them writes the setting. */}
      {admitsCustomUrls && (
        <AllowedClientsDialog
          trigger={
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground cursor-pointer text-sm underline underline-offset-2"
            >
              <AllowedClientsSummary />
            </button>
          }
        >
          {children}
        </AllowedClientsDialog>
      )}

      {update.isError && <FieldError>{update.error.message}</FieldError>}

      <RowSave visible={dirty}>
        {/* Render-function form: RequireScope's loading branch applies only
            pointer-events-none, so a keyboard user could still fire the
            mutation while grants are in flight. */}
        <RequireScope scope="project:write" level="component">
          {({ disabled }) => (
            <Button
              variant="primary"
              size="md"
              disabled={disabled || update.isPending}
              onClick={handleSave}
            >
              {update.isPending && (
                <Button.LeftIcon>
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>{update.isPending ? "Saving" : "Save"}</Button.Text>
            </Button>
          )}
        </RequireScope>
      </RowSave>
    </AuthRow>
  );
}

// The button's own label, so the counts an operator is about to manage are
// visible before the modal opens.
function AllowedClientsSummary() {
  const { data, isLoading, isError } = useCimdClientPresets();
  const verified = (data?.items ?? []).filter((preset) => preset.enabled);

  if (isLoading || isError) return <>Manage allowed clients</>;

  return <>Manage allowed clients ({verified.length} verified)</>;
}
