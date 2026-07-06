import { Badge } from "@speakeasy-api/moonshine";
import { SimpleTooltip } from "./ui/tooltip";

// The backend `account_type` telemetry dimension resolves to team | personal.
// Only `personal` is surfaced as a badge — team is the expected default, so
// anything that isn't personal (team, unclassified, undefined) is implied to
// be team and renders nothing.
type AccountTypeBadgeProps = {
  /**
   * The resolved account type. Accepts the raw backend value (which may be
   * undefined or an unrecognized string for unclassified sessions). Only a
   * `personal` value renders a badge; everything else renders nothing, so
   * callers can pass row data verbatim. Typed as a plain string so row models
   * can be passed directly — the runtime guard narrows.
   */
  accountType: string | undefined | null;
  /** When true, omit the tooltip wrapper (e.g. inside a row that already has one). */
  noTooltip?: boolean;
  className?: string;
};

export const PERSONAL_TOOLTIP =
  "Personal account — usage from an individual AI account (e.g. Claude Max) signed in on a company device, not your enterprise plan.";

export const TEAM_TOOLTIP =
  "Team account — usage from a shared AI plan provisioned by your organization's enterprise agreement.";

export function AccountTypeBadge({
  accountType,
  noTooltip = false,
  className,
}: AccountTypeBadgeProps): JSX.Element | null {
  // Only personal usage is flagged — it's the thing an admin is scanning for
  // (the "personal account on a company device" / cost-arbitrage story). Team
  // is the implied default and shows no badge to avoid noise across long tables.
  if (accountType !== "personal") return null;

  const pill = (
    <Badge
      size="sm"
      variant="warning"
      background
      className={className}
      data-account-type="personal"
    >
      <Badge.Text>Personal</Badge.Text>
    </Badge>
  );

  if (noTooltip) return pill;

  return <SimpleTooltip tooltip={PERSONAL_TOOLTIP}>{pill}</SimpleTooltip>;
}
