import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Text } from "@/components/ui/Text";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { UpdateUserSessionIssuerFormClientIdMetadataAdmissionMode as WritableMode } from "@gram/client/models/components/updateusersessionissuerform.js";
import { useCimdClientPresets } from "@gram/client/react-query/cimdClientPresets.js";
import { useId, type ReactNode } from "react";
import { AllowedClientsDialog } from "./AllowedClientsDialog";
import { AuthRow, ExplainerDialog } from "./AuthRow";

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

/**
 * The client access row. The selection and its Save live with the panel's
 * other session settings, in `UserIdentitySessionControls`.
 */
export function CimdAdmissionModeField({
  userSessionIssuer,
  draftMode,
  onDraftModeChange,
  children,
  readOnly = false,
}: {
  userSessionIssuer: UserSessionIssuer;
  /** The unsaved selection, or null while it matches the saved mode. */
  draftMode: WritableMode | null;
  readOnly?: boolean;
  /**
   * Each new selection. The custom-URL list belongs to the modes that consult
   * it, and an operator moving an issuer onto "Verified clients" needs to
   * stage those URLs before the switch takes effect, not after.
   */
  onDraftModeChange: (mode: WritableMode) => void;
  /** The custom-URL list, rendered only in the modes that consult it. */
  children?: ReactNode;
}): JSX.Element {
  const fieldId = useId();
  const effectiveMode = userSessionIssuer.clientIdMetadataAdmissionMode;

  // "reporting" cannot be written back, so there is no option to select for
  // it and the group renders with nothing chosen. Only a row stored before
  // "open" became the written default can still read back as it, and saving
  // any mode leaves that state for good.
  const unconfigured = effectiveMode === "reporting";

  const selectedMode = draftMode ?? (unconfigured ? null : effectiveMode);

  const explanation = MODE_OPTIONS.find(
    (option) => option.value === selectedMode,
  )?.explanation;

  // The allowlist only means anything in the mode that consults it, and it
  // follows the SELECTION rather than the saved value: an operator moving
  // onto "Verified clients" needs to stage those URLs before the switch
  // takes effect, not after.
  const admitsCustomUrls =
    selectedMode === WritableMode.Presets ||
    // "reporting" is not selectable, so selectedMode is null for an issuer
    // still stored that way — but it consults the same list, and hiding the
    // affordance would strand those issuers with no way to edit it.
    (draftMode === null && unconfigured);

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
        aria-describedby={explanation ? `${fieldId}-explanation` : undefined}
        value={selectedMode ?? ""}
        onValueChange={(next) => onDraftModeChange(next as WritableMode)}
        className="flex flex-wrap items-center gap-x-6 gap-y-2"
        disabled={readOnly}
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
        <Text muted small id={`${fieldId}-explanation`} className="block">
          {explanation}
        </Text>
      )}

      {/* A link on the explanation line, not a button stacked above Save:
          two buttons in one row read as a choice between them, when only
          one of them writes the setting. */}
      {admitsCustomUrls && !readOnly && (
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
