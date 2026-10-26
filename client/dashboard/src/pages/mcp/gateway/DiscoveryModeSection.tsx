import { RequireScope } from "@/components/require-scope";
import { useState } from "react";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import {
  SettingsSection,
  FooterSaveButton,
} from "@/components/detail/settings-section";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { ReleaseStageBadge } from "@/components/release-stage-badge";

export type DiscoveryMode = "progressive" | "direct" | "code_mode";

const discoveryModes: readonly string[] = [
  "progressive",
  "direct",
  "code_mode",
] satisfies DiscoveryMode[];

function isDiscoveryMode(value: unknown): value is DiscoveryMode {
  return typeof value === "string" && discoveryModes.includes(value);
}

/**
 * The Tool discovery settings section a gateway's default mode is chosen in:
 * the project gateway's Settings and the Organization Gateway page. The
 * caller decides whether Code Mode can be chosen and saves the mode.
 */
export function DiscoveryModeSection({
  savedMode,
  canWrite,
  writeScope,
  codeModeSelectable,
  codeModeNotEnabled,
  codeModeUnavailableLabel,
  codeModeFallbackReason,
  isPending,
  error,
  onSave,
}: {
  /** The saved default, which may be a mode this dashboard does not know. */
  savedMode: string;
  canWrite: boolean;
  /** The scope that may save, for the save button's permission feedback. */
  writeScope: { scope: Scope; resourceId?: string };
  codeModeSelectable: boolean;
  /** Only a successful entitlement read says the organization lacks Code Mode. */
  codeModeNotEnabled: boolean;
  /** The Code Mode option's label while it cannot be chosen for another reason. */
  codeModeUnavailableLabel: string;
  /** Why a saved Code Mode default serves Progressive, as one sentence. */
  codeModeFallbackReason: string;
  isPending: boolean;
  error: Error | null;
  onSave: (mode: DiscoveryMode) => void;
}): JSX.Element {
  const [mode, setMode] = useState(savedMode);
  const supportsEditing = isDiscoveryMode(savedMode);
  const knownDraft = isDiscoveryMode(mode);
  return (
    <SettingsSection id="discovery">
      <SettingsSection.Header>
        <SettingsSection.Title>
          Tool discovery <ReleaseStageBadge stage="preview" />
        </SettingsSection.Title>
        <SettingsSection.Description>
          Progressive discovers tools as needed. Direct lists every available
          tool and works with clients that cannot discover tools progressively.
          Code Mode exposes one execute tool for discovering and calling tools
          in Python.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field data-invalid={error ? true : undefined} className="max-w-md">
            <FieldLabel htmlFor="gateway-discovery-mode">
              Default mode
            </FieldLabel>
            <Select
              value={supportsEditing && knownDraft ? mode : ""}
              onValueChange={(value) => {
                if (
                  canWrite &&
                  supportsEditing &&
                  !isPending &&
                  isDiscoveryMode(value) &&
                  (value !== "code_mode" || codeModeSelectable)
                )
                  setMode(value);
              }}
              disabled={!canWrite || !supportsEditing || isPending}
            >
              <SelectTrigger id="gateway-discovery-mode">
                <SelectValue placeholder="Unknown mode" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="progressive">Progressive</SelectItem>
                <SelectItem value="direct">Direct</SelectItem>
                {codeModeNotEnabled ? (
                  // A disabled item ignores the pointer, so the wrapper
                  // receives the hover that opens the tooltip.
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div>
                        <SelectItem value="code_mode" disabled>
                          Code Mode (not enabled)
                        </SelectItem>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent side="right">
                      Code Mode is not enabled for your organization. Contact
                      Speakeasy to turn it on.
                    </TooltipContent>
                  </Tooltip>
                ) : (
                  <SelectItem value="code_mode" disabled={!codeModeSelectable}>
                    {codeModeSelectable
                      ? "Code Mode"
                      : codeModeUnavailableLabel}
                  </SelectItem>
                )}
              </SelectContent>
            </Select>
            {savedMode === "code_mode" && !codeModeSelectable && (
              <p className="text-muted-foreground text-sm">
                {codeModeFallbackReason} Connections currently use Progressive
                discovery; the saved default remains Code Mode.
              </p>
            )}
            {error && <FieldError>{error.message}</FieldError>}
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {supportsEditing
              ? "Applies to new connections that use the gateway default. Existing connections keep their mode until they reconnect."
              : "Refresh the page before changing this gateway’s unsupported discovery mode."}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope
              scope={writeScope.scope}
              resourceId={writeScope.resourceId}
              level="component"
            >
              <FooterSaveButton
                pending={isPending}
                disabled={
                  !canWrite ||
                  !supportsEditing ||
                  !knownDraft ||
                  mode === savedMode ||
                  isPending ||
                  (mode === "code_mode" && !codeModeSelectable)
                }
                onClick={() => {
                  if (
                    !canWrite ||
                    !supportsEditing ||
                    isPending ||
                    !isDiscoveryMode(mode) ||
                    (mode === "code_mode" && !codeModeSelectable)
                  )
                    return;
                  onSave(mode);
                }}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
