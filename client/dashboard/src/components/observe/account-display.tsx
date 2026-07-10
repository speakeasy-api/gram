import { AccountTypeBadge } from "@/components/account-type-badge";
import {
  type DisplayAccount,
  providerLabel,
} from "@/components/observe/account-display-utils";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/moonshine";
import { Type } from "@/components/ui/type";

// The per-account type marker. Personal reuses the shared amber badge; team is
// shown explicitly (this is the detailed view, so every account is labeled).
function AccountTypePill({
  accountType,
}: {
  accountType: string;
}): JSX.Element | null {
  if (accountType === "personal") {
    return <AccountTypeBadge accountType="personal" noTooltip />;
  }
  if (accountType === "team") {
    return (
      <Badge size="sm" variant="neutral" background>
        <Badge.Text>Team</Badge.Text>
      </Badge>
    );
  }
  return null;
}

// One account row: email + provider on the left, type pill on the right. Shared
// by the employees-list popover and the employee detail accounts card.
export function AccountRow({
  account,
  className,
}: {
  account: DisplayAccount;
  className?: string;
}): JSX.Element {
  return (
    <div className={cn("flex items-center justify-between gap-2", className)}>
      <div className="min-w-0">
        <Type small className="truncate">
          {account.email || "(no email)"}
        </Type>
        <Type muted small className="text-xs">
          {providerLabel(account.provider)}
        </Type>
      </div>
      <AccountTypePill accountType={account.accountType} />
    </div>
  );
}
