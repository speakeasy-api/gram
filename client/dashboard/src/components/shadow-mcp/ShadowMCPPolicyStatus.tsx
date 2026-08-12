import {
  type ShadowMCPPolicyDisposition,
  type ShadowMCPPolicyState,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { Text } from "@/components/ui/Text";
import { Icon } from "@/components/ui/Icon";
import { type IconName } from "@/components/ui/Icon/names";

function policyStatusText(
  state: ShadowMCPPolicyState,
  disposition: ShadowMCPPolicyDisposition | null,
): {
  label: string;
  icon: IconName;
  description: string;
} {
  switch (state) {
    case "blocking":
      if (disposition === "allow_all") {
        return {
          label: "Allowing by default",
          icon: "shield-check",
          description:
            "Allow-all policy is enabled. Servers are available unless a block rule denies them.",
        };
      }
      return {
        label: "Blocking",
        icon: "shield-check",
        description:
          "Block policy is enabled. Servers without allow rules are not allowed.",
      };
    case "warning":
      return {
        label: "Warning",
        icon: "shield-alert",
        description:
          "Warn policy is enabled. Users must acknowledge warnings before continuing.",
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
  disposition = null,
  policyState,
}: {
  disposition?: ShadowMCPPolicyDisposition | null;
  policyState: ShadowMCPPolicyState;
}): React.ReactNode {
  if (policyState === "unavailable") {
    return null;
  }

  const { label, icon, description } = policyStatusText(
    policyState,
    disposition,
  );

  return (
    <div className="border-border bg-muted/30 flex max-w-2xs items-start gap-2 border px-3 py-2">
      <Icon
        className="text-muted-foreground mt-0.5 h-4 w-4 shrink-0"
        name={icon}
      />
      <div className="min-w-0 flex-1">
        <Text variant="small" className="font-medium">
          {label}
        </Text>
        <Text muted className="text-xs">
          {description}
        </Text>
      </div>
    </div>
  );
}
