import { type ShadowMCPPolicyState } from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { Type } from "../ui/type";
import { Icon } from "@/components/ui/icon";
import type { IconName } from "@/components/ui/icon/names";

function policyStatusText(state: ShadowMCPPolicyState): {
  label: string;
  icon: IconName;
  description: string;
} {
  switch (state) {
    case "blocking":
      return {
        label: "Blocking",
        icon: "shield-check",
        description:
          "Block policy is enabled. Servers without allow rules are not allowed.",
      };
    case "flagging":
      return {
        label: "Flagging",
        icon: "shield-alert",
        description:
          "Flagging policy is enabled. Servers without allow rules are only flagged.",
      };
    case "none":
      return {
        label: "No Policy",
        icon: "shield-off",
        description:
          "No policy is enabled. All Shadow MCP servers are allowed.",
      };
    case "unavailable":
      return {
        label: "Unavailable",
        icon: "shield-off",
        description: "",
      };
  }
}

export function ShadowMCPPolicyStatus({
  policyState,
}: {
  policyState: ShadowMCPPolicyState;
}): React.ReactNode {
  if (policyState === "unavailable") {
    return null;
  }

  const { label, icon, description } = policyStatusText(policyState);
  return (
    <div className="border-border bg-muted/30 flex items-start gap-2 rounded-md border px-4 py-3">
      <Icon
        className="text-muted-foreground mt-1 h-4 w-4 shrink-0"
        name={icon}
      />
      <div className="min-w-0 flex-1">
        <Type variant="body">{label}</Type>
        <Type variant="small">{description}</Type>
      </div>
    </div>
  );
}
