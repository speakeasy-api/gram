// Policy display components — AGE-2704.
//
// Presentational pieces (ActionBadge) built on the pure helpers in
// `policy-display.ts`. Kept components-only so React Fast Refresh works
// (the `react(only-export-components)` lint rule); share helpers/constants via
// `policy-display.ts`, never from here.

import { Badge, type BadgeProps } from "@speakeasy-api/moonshine";
import { type PolicyAction } from "./policy-data";

const ACTION_BADGE_CONFIG: Record<
  PolicyAction,
  { label: string; variant: NonNullable<BadgeProps["variant"]> }
> = {
  flag: { label: "Flag", variant: "neutral" },
  block: { label: "Block", variant: "destructive" },
};

export function ActionBadge({ action }: { action: PolicyAction }): JSX.Element {
  const config = ACTION_BADGE_CONFIG[action] ?? ACTION_BADGE_CONFIG.flag;
  return (
    <Badge variant={config.variant}>
      <Badge.Text>{config.label}</Badge.Text>
    </Badge>
  );
}
