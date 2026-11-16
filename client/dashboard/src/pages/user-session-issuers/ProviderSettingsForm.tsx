import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { FieldError } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { AllowedClientsDialog } from "@/pages/mcp/x/tabs/settings/sections/authentication/AllowedClientsDialog";
import { AllowedClientsSummary } from "@/pages/mcp/x/tabs/settings/sections/authentication/CimdAdmissionModeField";
import { CimdCustomClientsField } from "@/pages/mcp/x/tabs/settings/sections/authentication/CimdCustomClientsField";
import {
  CLIENT_ACCESS_OPTIONS,
  DURATION_UNIT_HOURS,
  DURATION_UNIT_OPTIONS,
  splitIntoUnit,
  type DurationUnit,
} from "@/pages/mcp/x/tabs/settings/sections/authentication/sessionSettings";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { UpdateUserSessionIssuerFormClientIdMetadataAdmissionMode as WritableMode } from "@gram/client/models/components/updateusersessionissuerform.js";
import { useUpdateOrganizationUserSessionIssuerMutation } from "@gram/client/react-query/updateOrganizationUserSessionIssuer.js";
import { useUpdateUserSessionIssuerMutation } from "@gram/client/react-query/updateUserSessionIssuer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useState } from "react";
import { toast } from "sonner";
import { invalidateIssuers } from "./invalidateIssuers";

/**
 * How a provider's settings can be changed from the page showing them: an
 * organization-wide one through the organization, a project's own from that
 * project, and any other project's not at all.
 */
type EditPath =
  | { kind: "organization" }
  | { kind: "project" }
  | { kind: "elsewhere"; projectName: string };

function editPathFor(
  issuer: UserSessionIssuer,
  editableProjectId: string | undefined,
): EditPath {
  if (issuer.projectId === "") return { kind: "organization" };
  if (issuer.projectId === editableProjectId) return { kind: "project" };
  return {
    kind: "elsewhere",
    projectName: issuer.projectName ?? "its project",
  };
}

const SAVE_SCOPE: Record<"organization" | "project", Scope> = {
  organization: "org:admin",
  project: "project:write",
};

/**
 * A provider's session settings as one form: change any of them, then save
 * them together. They change how every MCP server using the provider signs
 * people in, so nothing applies until Save.
 */
export function ProviderSettingsForm({
  issuer,
  editableProjectId,
}: {
  issuer: UserSessionIssuer;
  /** The project whose own providers this page may change, if any. */
  editableProjectId?: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const fieldId = useId();
  const path = editPathFor(issuer, editableProjectId);
  const readOnly = path.kind === "elsewhere";

  const saved = splitIntoUnit(issuer.sessionDurationHours);
  const savedMode = issuer.clientIdMetadataAdmissionMode;
  const [durationNumber, setDurationNumber] = useState(saved.number);
  const [durationUnit, setDurationUnit] = useState<DurationUnit>(saved.unit);
  // "reporting" is a legacy stored value that cannot be written back, so it
  // selects nothing until someone picks a mode.
  const [mode, setMode] = useState<WritableMode | null>(
    savedMode === "reporting" ? null : (savedMode as WritableMode),
  );

  // Start over from what is saved whenever it changes underneath the form.
  useEffect(() => {
    const split = splitIntoUnit(issuer.sessionDurationHours);
    setDurationNumber(split.number);
    setDurationUnit(split.unit);
    setMode(savedMode === "reporting" ? null : (savedMode as WritableMode));
  }, [issuer.id, issuer.sessionDurationHours, savedMode]);

  const draftHours = durationNumber * DURATION_UNIT_HOURS[durationUnit];
  const durationChanged = draftHours !== issuer.sessionDurationHours;
  const modeChanged = mode !== null && mode !== savedMode;
  const dirty = !readOnly && (durationChanged || modeChanged);
  const valid = draftHours > 0;

  const onSuccess = async (): Promise<void> => {
    await invalidateIssuers(queryClient);
    toast.success("Settings saved");
  };
  const organizationUpdate = useUpdateOrganizationUserSessionIssuerMutation({
    onSuccess,
  });
  const projectUpdate = useUpdateUserSessionIssuerMutation({ onSuccess });
  const update =
    path.kind === "organization" ? organizationUpdate : projectUpdate;

  const save = (): void => {
    const changes = {
      id: issuer.id,
      ...(durationChanged ? { sessionDurationHours: draftHours } : {}),
      ...(modeChanged && mode ? { clientIdMetadataAdmissionMode: mode } : {}),
    };
    if (path.kind === "organization") {
      organizationUpdate.mutate({
        request: { updateOrganizationUserSessionIssuerForm: changes },
      });
    } else if (path.kind === "project") {
      projectUpdate.mutate({
        request: { updateUserSessionIssuerForm: changes },
      });
    }
  };

  const discard = (): void => {
    setDurationNumber(saved.number);
    setDurationUnit(saved.unit);
    setMode(savedMode === "reporting" ? null : (savedMode as WritableMode));
  };

  const explanation = CLIENT_ACCESS_OPTIONS.find(
    (option) => option.value === mode,
  )?.explanation;
  // The allowed-clients list belongs to the mode that consults it, and only
  // a project's own provider keeps one here.
  const showAllowedClients =
    path.kind === "project" && mode === WritableMode.Presets;

  let hint: string;
  if (path.kind === "elsewhere") {
    hint = `Change these in ${path.projectName}'s MCP Sessions configuration.`;
  } else if (path.kind === "organization") {
    hint = "Applies to every MCP server that uses it, in every project.";
  } else {
    hint = "Applies to every MCP server that uses it.";
  }

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Settings</SettingsSection.Title>
        <SettingsSection.Description>
          How long people stay signed in, and which MCP clients may connect.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body className="space-y-6">
          <div className="grid gap-2 sm:grid-cols-[220px_1fr] sm:gap-6">
            <div>
              <Label htmlFor={`${fieldId}-duration`}>Session length</Label>
              <Text muted small className="block">
                Longest a sign-in lasts before people sign in again.
              </Text>
            </div>
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  id={`${fieldId}-duration`}
                  type="number"
                  min="1"
                  value={String(durationNumber)}
                  onChange={(raw: string) => {
                    const parsed = parseInt(raw, 10);
                    setDurationNumber(
                      Number.isFinite(parsed) && parsed >= 0 ? parsed : 0,
                    );
                  }}
                  aria-invalid={!valid ? true : undefined}
                  className="w-[90px]"
                  disabled={readOnly}
                />
                <Select
                  value={durationUnit}
                  onValueChange={(value) =>
                    setDurationUnit(value as DurationUnit)
                  }
                  disabled={readOnly}
                >
                  <SelectTrigger
                    className="w-[110px]"
                    aria-label="Session length unit"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {DURATION_UNIT_OPTIONS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {!valid && (
                <FieldError>Enter a duration of at least one hour.</FieldError>
              )}
            </div>
          </div>

          <div className="grid gap-2 sm:grid-cols-[220px_1fr] sm:gap-6">
            <div>
              <Label id={`${fieldId}-access`}>Client access</Label>
              <Text muted small className="block">
                Which MCP clients may identify themselves by URL. Clients
                without one register themselves either way.
              </Text>
            </div>
            <div className="space-y-2">
              <RadioGroup
                aria-labelledby={`${fieldId}-access`}
                value={mode ?? ""}
                onValueChange={(next) => setMode(next as WritableMode)}
                className="flex flex-wrap items-center gap-x-6 gap-y-2"
                disabled={readOnly}
              >
                {CLIENT_ACCESS_OPTIONS.map((option) => (
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
              {showAllowedClients && (
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
                  <CimdCustomClientsField userSessionIssuer={issuer} />
                </AllowedClientsDialog>
              )}
            </div>
          </div>

          {update.isError && <FieldError>{update.error.message}</FieldError>}
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>{hint}</SettingsSection.FooterHint>
          {path.kind !== "elsewhere" && (
            <SettingsSection.FooterActions>
              {dirty && (
                <Button
                  variant="tertiary"
                  size="md"
                  onClick={discard}
                  disabled={update.isPending}
                >
                  <Button.Text>Discard</Button.Text>
                </Button>
              )}
              <RequireScope scope={SAVE_SCOPE[path.kind]} level="component">
                {({ disabled }) => (
                  <FooterSaveButton
                    pending={update.isPending}
                    disabled={disabled || !dirty || !valid || update.isPending}
                    onClick={save}
                  />
                )}
              </RequireScope>
            </SettingsSection.FooterActions>
          )}
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
