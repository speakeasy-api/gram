import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { FieldError } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  DURATION_UNIT_HOURS,
  DURATION_UNIT_OPTIONS,
  splitIntoUnit,
  type DurationUnit,
} from "@/pages/mcp/x/tabs/settings/sections/authentication/sessionSettings";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useEffect, useId, useState } from "react";
import {
  SAVE_SCOPE,
  editPathFor,
  settingsFooterHint,
  useSaveProviderSettings,
} from "./providerSettings";

/** How long a sign-in through this provider lasts, saved on its own. */
export function SessionLengthSection({
  issuer,
  editableProjectId,
}: {
  issuer: UserSessionIssuer;
  /** The project whose own providers this page may change, if any. */
  editableProjectId?: string;
}): JSX.Element {
  const fieldId = useId();
  const path = editPathFor(issuer, editableProjectId);
  const readOnly = path.kind === "elsewhere";
  const saved = splitIntoUnit(issuer.sessionDurationHours);
  const [number, setNumber] = useState(saved.number);
  const [unit, setUnit] = useState<DurationUnit>(saved.unit);

  // Start over from what is saved whenever it changes underneath the form.
  useEffect(() => {
    const split = splitIntoUnit(issuer.sessionDurationHours);
    setNumber(split.number);
    setUnit(split.unit);
  }, [issuer.id, issuer.sessionDurationHours]);

  const draftHours = number * DURATION_UNIT_HOURS[unit];
  const dirty = !readOnly && draftHours !== issuer.sessionDurationHours;
  const valid = draftHours > 0;
  const update = useSaveProviderSettings(issuer, path);

  return (
    <SettingsSection id="session-length">
      <SettingsSection.Header>
        <SettingsSection.Title>Session length</SettingsSection.Title>
        <SettingsSection.Description>
          Longest a sign-in lasts before people sign in again.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              id={`${fieldId}-duration`}
              aria-label="Session length"
              type="number"
              min="1"
              value={String(number)}
              onChange={(raw: string) => {
                const parsed = parseInt(raw, 10);
                setNumber(Number.isFinite(parsed) && parsed >= 0 ? parsed : 0);
              }}
              aria-invalid={!valid ? true : undefined}
              className="w-[90px]"
              disabled={readOnly}
            />
            <Select
              value={unit}
              onValueChange={(value) => setUnit(value as DurationUnit)}
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
          {update.error && <FieldError>{update.error.message}</FieldError>}
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {settingsFooterHint(path)}
          </SettingsSection.FooterHint>
          {path.kind !== "elsewhere" && (
            <SettingsSection.FooterActions>
              {dirty && (
                <Button
                  variant="tertiary"
                  size="md"
                  onClick={() => {
                    setNumber(saved.number);
                    setUnit(saved.unit);
                  }}
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
                    onClick={() =>
                      update.save({ sessionDurationHours: draftHours })
                    }
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
