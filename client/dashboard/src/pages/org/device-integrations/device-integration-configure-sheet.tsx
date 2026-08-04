import { RequireScope } from "@/components/require-scope";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import type { DeviceIntegrationProviderField } from "@gram/client/models/components/deviceintegrationproviderfield.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import {
  CheckCircle2,
  ChevronRight,
  Loader2,
  PauseCircle,
  PlugZap,
  Trash2,
  XCircle,
} from "lucide-react";
import { useState } from "react";
import { providerUI } from "./provider-ui";
import type { DeviceIntegrationConfigForm } from "./use-device-integration-config";

export function ConnectionStatusBadge({
  enabled,
  configured,
}: {
  enabled: boolean;
  configured: boolean;
}): JSX.Element {
  if (!configured) {
    return (
      <SimpleTooltip tooltip="No credentials saved yet. Use Connect to set up this provider.">
        <Badge variant="neutral" className="shrink-0">
          <Badge.Text>Not connected</Badge.Text>
        </Badge>
      </SimpleTooltip>
    );
  }

  if (enabled) {
    return (
      <SimpleTooltip tooltip="The provider connection is enabled. Individual schedules can still be paused.">
        <Badge variant="success" background className="shrink-0">
          <Badge.LeftIcon>
            <CheckCircle2 className="h-3.5 w-3.5" />
          </Badge.LeftIcon>
          <Badge.Text>Connected</Badge.Text>
        </Badge>
      </SimpleTooltip>
    );
  }

  return (
    <SimpleTooltip tooltip="The provider connection is paused. No schedules will sync until it is resumed.">
      <Badge variant="neutral" background className="shrink-0">
        <Badge.LeftIcon>
          <PauseCircle className="h-3.5 w-3.5" />
        </Badge.LeftIcon>
        <Badge.Text>Paused</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

// Credential form in a right-hand side sheet, rendered entirely from the
// provider descriptor's field spec — new vendors need zero frontend work.
export function DeviceIntegrationConfigureSheet({
  provider,
  form,
  open,
  onOpenChange,
}: {
  provider: DeviceIntegrationProvider;
  form: DeviceIntegrationConfigForm;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const handleDelete = () => {
    if (!form.isConfigured) return;
    if (
      !window.confirm(`Delete the ${provider.displayName} device integration?`)
    ) {
      return;
    }
    form.remove();
  };

  const handleOpenChange = (nextOpen: boolean) => {
    // Closing discards the draft: a canceled session's typed secrets and
    // half-edited settings must not reappear on the next open.
    if (!nextOpen) form.resetDraft();
    onOpenChange(nextOpen);
  };

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent side="right" className="overflow-y-auto sm:max-w-md">
        <SheetHeader>
          <SheetTitle>
            {form.isConfigured
              ? `Configure ${provider.displayName}`
              : `Connect ${provider.displayName}`}
          </SheetTitle>
          <SheetDescription>
            Credentials are stored encrypted and are never shown again after
            saving. New connections start paused: save, test the connection,
            then enable it.
          </SheetDescription>
        </SheetHeader>

        <Stack gap={4} className="px-4">
          <SetupSteps steps={providerUI(provider).setupSteps} />
          {provider.fields
            .filter((field) => field.required)
            .map((field) => (
              <FieldInput
                key={field.key}
                provider={provider.id}
                field={field}
                form={form}
              />
            ))}

          <AdvancedFields
            provider={provider.id}
            fields={provider.fields.filter((field) => !field.required)}
            form={form}
          />

          {form.isConfigured ? (
            <TestConnectionRow form={form} />
          ) : (
            <Text variant="body" className="text-muted-foreground text-xs">
              Save the connection first to enable the connection test.
            </Text>
          )}
        </Stack>

        <SheetFooter className="flex-row items-center border-t">
          {form.isConfigured ? (
            <RequireScope scope="org:admin" level="component">
              <Button
                variant="destructive-secondary"
                onClick={handleDelete}
                disabled={form.isMutating}
              >
                <Button.LeftIcon>
                  <Trash2 className="size-3.5" />
                </Button.LeftIcon>
                <Button.Text>Delete</Button.Text>
              </Button>
            </RequireScope>
          ) : null}
          <div className="ml-auto flex items-center gap-2">
            <Button variant="secondary" onClick={() => handleOpenChange(false)}>
              <Button.Text>Cancel</Button.Text>
            </Button>
            <RequireScope scope="org:admin" level="component">
              <Button
                variant="primary"
                onClick={form.save}
                disabled={!form.canSave}
              >
                <Button.Text>Save</Button.Text>
              </Button>
            </RequireScope>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function SetupSteps({ steps }: { steps?: string[] }) {
  if (!steps || steps.length === 0) return null;
  return (
    <ol className="text-muted-foreground list-decimal space-y-1 pl-4 text-xs">
      {steps.map((step) => (
        <li key={step}>{step}</li>
      ))}
    </ol>
  );
}

function FieldInput({
  provider,
  field,
  form,
}: {
  provider: string;
  field: DeviceIntegrationProviderField;
  form: DeviceIntegrationConfigForm;
}) {
  const fieldId = `${provider}-connection-${field.key}`;
  const value = field.secret
    ? (form.credentials[field.key] ?? "")
    : (form.settings[field.key] ?? "");
  const setValue = field.secret ? form.setCredential : form.setSetting;

  return (
    <Stack gap={2}>
      <Label htmlFor={fieldId}>{field.label}</Label>
      <Input
        id={fieldId}
        placeholder={placeholderFor(field, form.hasSavedCredentials)}
        value={value}
        onChange={(next) => setValue(field.key, next)}
        type={field.secret ? "password" : "text"}
        disabled={form.isLoading || form.isMutating}
      />
    </Stack>
  );
}

// Optional settings live behind an "Advanced" disclosure so the default view is
// just the required fields (e.g. Region + API Key for Drata — the connection id
// is created automatically, so most customers never touch it). Rendered from
// the descriptor's `required` flag, so every provider's optional fields tuck
// away with zero per-provider frontend work. The section auto-expands when an
// optional field already holds a value, so editing a config that uses one never
// hides it.
function AdvancedFields({
  provider,
  fields,
  form,
}: {
  provider: string;
  fields: DeviceIntegrationProviderField[];
  form: DeviceIntegrationConfigForm;
}) {
  const hasValue = fields.some(
    (field) => (form.settings[field.key] ?? "") !== "",
  );
  // null = follow the data (open iff a value exists); a boolean is the user's
  // explicit choice, which then wins. Derived during render — no effect, so no
  // stale-collapsed flash when the config loads.
  const [override, setOverride] = useState<boolean | null>(null);
  const open = override ?? hasValue;

  if (fields.length === 0) return null;

  return (
    <Collapsible open={open} onOpenChange={setOverride}>
      <CollapsibleTrigger asChild>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs font-medium"
        >
          <ChevronRight
            className={cn("size-3.5 transition-transform", open && "rotate-90")}
          />
          Advanced
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <Stack gap={4} className="pt-4">
          {fields.map((field) => (
            <FieldInput
              key={field.key}
              provider={provider}
              field={field}
              form={form}
            />
          ))}
        </Stack>
      </CollapsibleContent>
    </Collapsible>
  );
}

function placeholderFor(
  field: DeviceIntegrationProviderField,
  hasSavedCredentials: boolean,
): string {
  if (field.secret) {
    return hasSavedCredentials ? "•••••• (saved)" : field.label;
  }
  if (field.kind === "url") {
    return "https://…";
  }
  return field.label;
}

function TestConnectionRow({ form }: { form: DeviceIntegrationConfigForm }) {
  // The test probes the SAVED configuration, so a dirty draft would test the
  // old values while looking like it tested the new ones. Gate the button
  // and say why, mirroring the pre-save state's guidance.
  const dirty = form.hasUnsavedChanges;
  return (
    <Stack gap={2}>
      <Stack direction="horizontal" align="center" gap={2}>
        <Button
          variant="secondary"
          size="sm"
          onClick={form.testConnection}
          disabled={form.isTesting || form.isMutating || dirty}
        >
          <Button.LeftIcon>
            {form.isTesting ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <PlugZap className="size-3.5" />
            )}
          </Button.LeftIcon>
          <Button.Text>Test connection</Button.Text>
        </Button>
        <TestResultBadge form={form} />
      </Stack>
      <Text variant="body" className="text-muted-foreground text-xs">
        {dirty
          ? "Unsaved changes — save first. The test always runs against the saved credentials."
          : "Runs a real request against the vendor using the saved credentials."}
      </Text>
    </Stack>
  );
}

function TestResultBadge({ form }: { form: DeviceIntegrationConfigForm }) {
  if (!form.testResult) return null;
  if (form.testResult.ok) {
    return (
      <Badge variant="success" background className="shrink-0">
        <Badge.LeftIcon>
          <CheckCircle2 className="h-3.5 w-3.5" />
        </Badge.LeftIcon>
        <Badge.Text>Connection OK</Badge.Text>
      </Badge>
    );
  }
  return (
    <SimpleTooltip tooltip={form.testResult.message ?? "Connection failed"}>
      <Badge variant="destructive" background className="shrink-0">
        <Badge.LeftIcon>
          <XCircle className="h-3.5 w-3.5" />
        </Badge.LeftIcon>
        <Badge.Text>Failed</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}
