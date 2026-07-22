import { RequireScope } from "@/components/require-scope";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { Badge, Button, Stack } from "@speakeasy-api/moonshine";
import { CheckCircle2, PauseCircle, Trash2 } from "lucide-react";
import type { AIIntegrationProvider } from "./ai-integration-providers";
import type { AIIntegrationConfigForm } from "./use-ai-integration-config-form";

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
      <SimpleTooltip tooltip="The provider connection is enabled. Individual streams can still be paused.">
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
    <SimpleTooltip tooltip="The provider connection is paused. No streams will poll until it is resumed.">
      <Badge variant="neutral" background className="shrink-0">
        <Badge.LeftIcon>
          <PauseCircle className="h-3.5 w-3.5" />
        </Badge.LeftIcon>
        <Badge.Text>Paused</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

// Credential form in a right-hand side sheet.
export function ConfigureSheet({
  provider,
  form,
  open,
  onOpenChange,
}: {
  provider: AIIntegrationProvider;
  form: AIIntegrationConfigForm;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const apiKeyFieldId = `${provider.provider}-connection-api-key`;
  const orgIdFieldId = `${provider.provider}-connection-org-id`;
  const billingModeFieldId = `${provider.provider}-connection-billing-mode`;

  const handleDelete = () => {
    if (!form.isConfigured) return;
    if (!window.confirm(`Delete the ${provider.name} AI integration?`)) return;
    form.remove();
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="overflow-y-auto sm:max-w-md">
        <SheetHeader>
          <SheetTitle>
            {form.isConfigured
              ? `Configure ${provider.name}`
              : `Connect ${provider.name}`}
          </SheetTitle>
          <SheetDescription>{provider.onboardingDescription}</SheetDescription>
        </SheetHeader>

        <Stack gap={4} className="px-4">
          <Stack gap={2}>
            <Label htmlFor={apiKeyFieldId}>{provider.apiKeyLabel}</Label>
            <Input
              id={apiKeyFieldId}
              placeholder={
                form.hasSavedKey ? "•••••• (saved)" : provider.apiKeyPlaceholder
              }
              value={form.apiKey}
              onChange={form.setApiKey}
              type="password"
              disabled={form.isLoading || form.isMutating}
            />
            {provider.helpText ? (
              <Type variant="body" className="text-muted-foreground text-xs">
                {provider.helpText}
              </Type>
            ) : null}
          </Stack>

          {provider.requiresOrganizationId ? (
            <Stack gap={2}>
              <Label htmlFor={orgIdFieldId}>
                {provider.organizationIdLabel ?? "Organization ID"}
              </Label>
              <Input
                id={orgIdFieldId}
                placeholder={provider.organizationIdPlaceholder}
                value={form.organizationId}
                onChange={form.setOrganizationId}
                disabled={form.isLoading || form.isMutating}
              />
            </Stack>
          ) : null}

          <Stack gap={2}>
            <Label htmlFor={billingModeFieldId}>Billing mode</Label>
            <Select
              value={form.billingMode || "unknown"}
              onValueChange={(value) => form.setBillingMode(value)}
              disabled={form.isLoading || form.isMutating}
            >
              <SelectTrigger id={billingModeFieldId}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="unknown">Unknown</SelectItem>
                <SelectItem value="metered">Metered (pay-per-token)</SelectItem>
                <SelectItem value="flat_rate">
                  Flat rate (subscription seats)
                </SelectItem>
              </SelectContent>
            </Select>
            <Type variant="body" className="text-muted-foreground text-xs">
              Dashboard cost is estimated from token usage at API rates. Only
              "Metered" accounts are billed per token, so their cost is shown as
              real spend; subscription plans show it as an estimate.
            </Type>
          </Stack>
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
            <Button variant="secondary" onClick={() => onOpenChange(false)}>
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
