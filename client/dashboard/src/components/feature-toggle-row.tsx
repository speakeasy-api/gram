import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";
import type { LucideIcon } from "lucide-react";

/**
 * One org-level product-feature toggle: icon, name, explanation, and an
 * admin-gated switch. Shared by the settings pages that flip product
 * features (data configuration, hook behavior).
 */
export function FeatureToggleRow({
  icon: Icon,
  title,
  description,
  checked,
  onCheckedChange,
  disabled,
  ready,
  ariaLabel,
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  checked: boolean;
  onCheckedChange: (enabled: boolean) => void;
  disabled: boolean;
  /** Render the switch only once the current server values have loaded. */
  ready: boolean;
  ariaLabel: string;
}): JSX.Element {
  return (
    <Stack direction="horizontal" justify="space-between" align="center">
      <Stack gap={1}>
        <Stack direction="horizontal" align="center" gap={2}>
          <Icon className="text-muted-foreground h-4 w-4" />
          <Type variant="body" className="font-medium">
            {title}
          </Type>
        </Stack>
        <Type
          variant="body"
          className="text-muted-foreground mr-8 ml-6 max-w-4xl text-sm"
        >
          {description}
        </Type>
      </Stack>
      {ready && (
        <RequireScope scope="org:admin" level="component">
          <Switch
            checked={checked}
            onCheckedChange={onCheckedChange}
            disabled={disabled}
            aria-label={ariaLabel}
          />
        </RequireScope>
      )}
    </Stack>
  );
}
