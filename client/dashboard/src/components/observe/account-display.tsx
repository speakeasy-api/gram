import { AccountTypeBadge } from "@/components/account-type-badge";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import {
  type DisplayAccount,
  providerLabel,
} from "@/components/observe/account-display-utils";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/Badge";

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
    <div className={cn("flex items-center gap-2", className)}>
      {/* The provider's own mark, so a row is recognisable before it is read:
          the email is the part that differs between two accounts, and the
          provider is the part that says which product they are on. */}
      <AgentProviderIcon
        source={account.provider}
        className="text-muted-foreground size-4 shrink-0"
      />
      <div className="mr-auto min-w-0">
        <p className="truncate text-sm">{account.email || "(no email)"}</p>
        <p className="text-muted-foreground text-xs">
          {providerLabel(account.provider)}
        </p>
      </div>
      <AccountTypePill accountType={account.accountType} />
    </div>
  );
}
