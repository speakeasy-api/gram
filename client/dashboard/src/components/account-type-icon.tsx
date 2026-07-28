import { Icon } from "@speakeasy-api/moonshine";

import { cn } from "@/lib/utils";

import { PERSONAL_TOOLTIP, TEAM_TOOLTIP } from "./account-type-badge";
import { SimpleTooltip } from "./ui/tooltip";

// Encodes the personal/team distinction in the person glyph itself: a single
// person for personal accounts, a group for team accounts. This reuses the
// person-icon slot that already sits next to an account owner in session and
// tool-log rows, so the distinction is legible at a glance across the app.
//
// Unclassified sessions (empty/undefined account type) keep the single-person
// icon and get no tooltip — there's nothing meaningful to explain yet.
type AccountTypeIconProps = {
  /**
   * The resolved account type. Accepts the raw backend value (`team`,
   * `personal`, or an unclassified empty/undefined string), so callers can pass
   * row data verbatim — the runtime guard narrows.
   */
  accountType: string | undefined | null;
  /** When true, omit the tooltip wrapper (e.g. inside a row that already has one). */
  noTooltip?: boolean;
  /** Classes applied to the icon. Defaults match the surrounding metadata icons. */
  className?: string;
};

type AccountTypeGlyph = {
  iconName: "user" | "users";
  label: string;
  tooltip: string | null;
  // Personal usage is the thing an admin scans for, so it's tinted with the
  // warning color and shown at full strength; team/unclassified stay muted like
  // the surrounding metadata icons.
  iconClassName: string;
};

function glyphForAccountType(
  accountType: string | undefined | null,
): AccountTypeGlyph {
  switch (accountType) {
    case "personal":
      return {
        iconName: "user",
        label: "Personal account",
        tooltip: PERSONAL_TOOLTIP,
        iconClassName: "text-warning",
      };
    case "team":
      return {
        iconName: "users",
        label: "Team account",
        tooltip: TEAM_TOOLTIP,
        iconClassName: "opacity-60",
      };
    case null:
    case undefined:
    default:
      return {
        iconName: "user",
        label: "Account owner",
        tooltip: null,
        iconClassName: "opacity-60",
      };
  }
}

export function AccountTypeIcon({
  accountType,
  noTooltip = false,
  className,
}: AccountTypeIconProps): JSX.Element {
  const { iconName, label, tooltip, iconClassName } =
    glyphForAccountType(accountType);

  const icon = (
    <span className="inline-flex items-center" aria-label={label}>
      <Icon
        name={iconName}
        className={cn("size-4", iconClassName, className)}
      />
    </span>
  );

  if (noTooltip || !tooltip) return icon;

  return <SimpleTooltip tooltip={tooltip}>{icon}</SimpleTooltip>;
}
