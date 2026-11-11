import { useAiIntegrationOrgUnits } from "@gram/client/react-query/aiIntegrationOrgUnits";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import {
  type AIIntegrationProvider,
  orgUnitLabel,
} from "./ai-integration-providers";
import type { AIIntegrationConfigForm } from "./use-ai-integration-config-form";

// Google Workspace fields for providers that act as a delegated admin and
// export one organizational unit (gemini_compliance). The unit list needs the
// saved key and admin, so it appears after the first save.
export function AIIntegrationWorkspaceFields({
  provider,
  form,
  active,
}: {
  provider: AIIntegrationProvider;
  form: AIIntegrationConfigForm;
  // Whether the surrounding sheet or drawer is open; the unit list is only
  // fetched while it is.
  active: boolean;
}): JSX.Element | null {
  const showsOrgUnits = Boolean(provider.selectsOrgUnit && form.hasSavedKey);
  const orgUnits = useAiIntegrationOrgUnits(
    { provider: provider.provider },
    undefined,
    // A refusal names the delegation or privilege to fix; retrying won't.
    { enabled: active && showsOrgUnits, retry: false },
  );
  if (!provider.delegatedUser && !provider.selectsOrgUnit) return null;

  const delegatedUserFieldId = `${provider.provider}-delegated-user`;
  const orgUnitFieldId = `${provider.provider}-org-unit`;
  const disabled = form.isLoading || form.isMutating;

  return (
    <>
      {provider.delegatedUser ? (
        <div className="space-y-2">
          <Label htmlFor={delegatedUserFieldId}>
            {provider.delegatedUser.label}
          </Label>
          <Input
            id={delegatedUserFieldId}
            type="email"
            placeholder={provider.delegatedUser.placeholder}
            value={form.delegatedUser}
            onChange={form.setDelegatedUser}
            noAutofill
            disabled={disabled}
          />
          {provider.delegatedUser.helpText ? (
            <Text variant="body" className="text-muted-foreground text-xs">
              {provider.delegatedUser.helpText}
            </Text>
          ) : null}
        </div>
      ) : null}

      {showsOrgUnits ? (
        <div className="space-y-2">
          <Label htmlFor={orgUnitFieldId}>Organizational unit to export</Label>
          {orgUnits.isError ? (
            <Text variant="body" role="alert" destructive className="text-xs">
              {orgUnits.error.message}
            </Text>
          ) : (
            <Select
              value={form.orgUnitId}
              onValueChange={form.setOrgUnitId}
              disabled={disabled || orgUnits.isLoading}
            >
              <SelectTrigger id={orgUnitFieldId}>
                <SelectValue
                  placeholder={
                    orgUnits.isLoading
                      ? "Loading organizational units…"
                      : "Choose an organizational unit"
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {(orgUnits.data?.orgUnits ?? []).map((unit) => (
                  <SelectItem key={unit.id} value={unit.id}>
                    {orgUnitLabel(unit)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <Text variant="body" className="text-muted-foreground text-xs">
            {form.awaitingOrgUnit
              ? "Syncing starts once a unit is saved. Choose Entire organization to cover everyone."
              : "Changing the unit restarts the import for the new scope."}
          </Text>
        </div>
      ) : null}
    </>
  );
}
