import { AccountTypeBadge } from "@/components/account-type-badge";
import {
  type DisplayAccount,
  providerLabel,
} from "@/components/observe/account-display-utils";
import { Badge } from "@speakeasy-api/moonshine";

// The per-account type marker. Personal reuses the shared amber badge; team is
// shown explicitly (this is the detailed view, so every account is labeled).
export function AccountTypePill({
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
}: {
  account: DisplayAccount;
}): JSX.Element {
  return (
    <div className="flex items-center justify-between gap-2">
      <div className="min-w-0">
        <p className="truncate text-sm">{account.email || "(no email)"}</p>
        <p className="text-muted-foreground text-xs">
          {providerLabel(account.provider)}
        </p>
      </div>
      <AccountTypePill accountType={account.accountType} />
    </div>
  );
}
