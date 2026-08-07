import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { Checkbox } from "@/components/ui/Checkbox";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import {
  ALLOW_RULE_POLICY_REQUIRED,
  shadowMCPInventoryActions,
} from "./shadowMCPInventoryActionItems";
import type { ShadowMCPPolicyDisposition } from "./shadowMCPInventoryStatus";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { useEffect, useState } from "react";

export type ShadowMCPPolicy = Pick<
  RiskPolicy,
  | "audienceType"
  | "audiencePrincipalUrns"
  | "id"
  | "name"
  | "shadowMcpDisposition"
>;

export type InventoryActionMode =
  | "review"
  | "add"
  | "edit"
  | "delete"
  | "block"
  | "unblock";
export type ReviewDecision = "allow" | "deny";
export type ActiveInventoryAction = {
  mode: InventoryActionMode;
  server: ShadowMCPInventoryServer;
};

function humanizePrincipalURN(principalURN: string) {
  if (principalURN === "user:all") {
    return "Everyone";
  }

  const segments = principalURN.split(":").filter(Boolean);
  const label = segments[segments.length - 1] ?? principalURN;
  return label
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function memberDisplayName(member: AccessMember) {
  if (member.name && member.name !== member.email) {
    return `${member.name} (${member.email})`;
  }
  return member.email;
}

function policyAudienceLabel(
  policy: ShadowMCPPolicy,
  roles: Role[],
  members: AccessMember[],
) {
  if (policy.audienceType === "everyone") {
    return "Everyone";
  }

  const principalLabels = policy.audiencePrincipalUrns.map((principalURN) => {
    if (principalURN.startsWith("user:")) {
      const member = members.find((item) => item.principalUrn === principalURN);
      return member
        ? memberDisplayName(member)
        : humanizePrincipalURN(principalURN);
    }

    const role = roles.find((item) => item.principalUrn === principalURN);
    return role?.name ?? humanizePrincipalURN(principalURN);
  });
  if (principalLabels.length <= 2) {
    return principalLabels.join(", ");
  }

  return `${principalLabels.slice(0, 2).join(", ")} + ${principalLabels.length - 2} more`;
}

function actionSheetTitle(mode: InventoryActionMode) {
  switch (mode) {
    case "review":
      return "Review Request";
    case "add":
      return "Add Allow Rule";
    case "edit":
      return "Edit Rule";
    case "delete":
      return "Delete Rule";
    case "block":
      return "Block Server";
    case "unblock":
      return "Unblock Server";
  }
}

function actionSheetDescription(mode: InventoryActionMode) {
  switch (mode) {
    case "review":
      return "Resolve the pending Shadow MCP request for this server.";
    case "add":
      return "Allow this Shadow MCP server for selected policies.";
    case "edit":
      return "Change which policies allow this Shadow MCP server.";
    case "delete":
      return "Remove the allow decision for this Shadow MCP server.";
    case "block":
      return "Add this server to the policy's blocked list. Everyone in the project loses access.";
    case "unblock":
      return "Remove this server from the policy's blocked list. Everyone in the project regains access.";
  }
}

function actionSheetSubmitLabel(
  mode: InventoryActionMode,
  decision: ReviewDecision,
) {
  switch (mode) {
    case "review":
      return decision === "allow" ? "Approve Request" : "Deny Request";
    case "delete":
      return "Delete Rule";
    case "edit":
      return "Save Changes";
    case "block":
      return "Block Server";
    case "unblock":
      return "Unblock Server";
    case "add":
      return "Add Allow Rule";
  }
}

function initialPolicyIDsForAction(
  action: ActiveInventoryAction,
  shadowMCPPolicies: ShadowMCPPolicy[],
) {
  const shadowMCPPolicyIDs = shadowMCPPolicies.map((policy) => policy.id);
  if (action.server.allowedPolicyIds.length > 0) {
    return action.server.allowedPolicyIds.filter((policyID) =>
      shadowMCPPolicyIDs.includes(policyID),
    );
  }
  if (
    action.mode === "review" &&
    action.server.latestRequest &&
    shadowMCPPolicyIDs.includes(action.server.latestRequest.policyId)
  ) {
    return [action.server.latestRequest.policyId];
  }
  return shadowMCPPolicyIDs;
}

export function ShadowMCPInventoryActionMenu({
  canManageAllowRules,
  disabled,
  disposition = null,
  onOpenAction,
  server,
}: {
  canManageAllowRules: boolean;
  disabled: boolean;
  disposition?: ShadowMCPPolicyDisposition | null;
  onOpenAction: (
    mode: InventoryActionMode,
    server: ShadowMCPInventoryServer,
  ) => void;
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  const actions = shadowMCPInventoryActions(server, {
    canManageAllowRules,
    disabled,
    disposition,
    onOpenAction,
  });

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={`Open actions for ${server.serverName || server.urlHost}`}
          disabled={disabled}
          onClick={(event) => event.stopPropagation()}
          size="xs"
          variant="tertiary"
        >
          <Button.Icon>
            <Icon name="ellipsis" />
          </Button.Icon>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        onClick={(event) => event.stopPropagation()}
      >
        {actions.map((action, index) => (
          <DropdownMenuItem
            disabled={action.disabled}
            key={index}
            onSelect={(event) => {
              event.stopPropagation();
              action.onClick();
            }}
          >
            {action.description ? (
              <span className="flex min-w-0 flex-col">
                <span>{action.label}</span>
                <span className="text-muted-foreground text-xs">
                  {action.description}
                </span>
              </span>
            ) : (
              action.label
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function PolicySelection({
  disabled,
  emptyMessage,
  members,
  onSelectionChange,
  policies,
  roles,
  selectedPolicyIDs,
}: {
  disabled: boolean;
  emptyMessage: string;
  members: AccessMember[];
  onSelectionChange: (policyIDs: string[]) => void;
  policies: ShadowMCPPolicy[];
  roles: Role[];
  selectedPolicyIDs: string[];
}) {
  const selectedPolicyIDSet = new Set(selectedPolicyIDs);

  return (
    <section className="border-border space-y-3 border p-3">
      <Text variant="small" className="font-medium">
        Policies
      </Text>
      <div className="space-y-2">
        {policies.length === 0 && (
          <Text muted small>
            {emptyMessage}
          </Text>
        )}
        {policies.map((policy) => {
          const checked = selectedPolicyIDSet.has(policy.id);
          return (
            <label
              key={policy.id}
              className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 px-3 py-2.5 transition-colors"
            >
              <Checkbox
                checked={checked}
                disabled={disabled}
                onCheckedChange={(nextChecked) => {
                  if (nextChecked) {
                    onSelectionChange([...selectedPolicyIDs, policy.id]);
                    return;
                  }
                  onSelectionChange(
                    selectedPolicyIDs.filter(
                      (policyID) => policyID !== policy.id,
                    ),
                  );
                }}
              />
              <span className="min-w-0 flex-1">
                <Text variant="small" className="truncate font-medium">
                  {policy.name}
                </Text>
                <Text muted small>
                  Policy applies to{" "}
                  {policyAudienceLabel(policy, roles, members)}
                </Text>
              </span>
            </label>
          );
        })}
      </div>
    </section>
  );
}

export function ShadowMCPInventoryActionSheet({
  action,
  disposition = null,
  isSubmitting,
  members,
  onOpenChange,
  onSubmit,
  open,
  policyUnavailableMessage = ALLOW_RULE_POLICY_REQUIRED,
  roles,
  shadowMCPPolicies,
}: {
  action: ActiveInventoryAction | null;
  disposition?: ShadowMCPPolicyDisposition | null;
  isSubmitting: boolean;
  members: AccessMember[];
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    action: ActiveInventoryAction;
    decision: ReviewDecision;
    policyIDs: string[];
  }) => Promise<void>;
  open: boolean;
  policyUnavailableMessage?: string;
  roles: Role[];
  shadowMCPPolicies: ShadowMCPPolicy[];
}): JSX.Element | null {
  const [decision, setDecision] = useState<ReviewDecision>("allow");
  const [selectedPolicyIDs, setSelectedPolicyIDs] = useState<string[]>([]);

  useEffect(() => {
    if (!action || !open) {
      setDecision("allow");
      setSelectedPolicyIDs([]);
      return;
    }
    setDecision("allow");
    setSelectedPolicyIDs(initialPolicyIDsForAction(action, shadowMCPPolicies));
  }, [action, shadowMCPPolicies, open]);

  if (!action) return null;

  const server = action.server;
  const isBlocklistAction =
    action.mode === "block" || action.mode === "unblock";
  // Under allow_all, approving a request unblocks the server project-wide, so
  // there is no policy selection to make.
  const isAllowAllReview =
    action.mode === "review" && disposition === "allow_all";
  const canChoosePolicies =
    !isBlocklistAction &&
    !isAllowAllReview &&
    action.mode !== "delete" &&
    (action.mode !== "review" || decision === "allow");
  const needsPolicySelection = canChoosePolicies;
  const canSubmit =
    !isSubmitting &&
    (isBlocklistAction ||
      isAllowAllReview ||
      action.mode === "delete" ||
      (action.mode === "review" && decision === "deny") ||
      selectedPolicyIDs.length > 0);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{actionSheetTitle(action.mode)}</SheetTitle>
          <SheetDescription>
            {actionSheetDescription(action.mode)}
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4">
          <section className="border-border border px-4 py-3">
            <Text variant="small" className="font-medium">
              {server.serverName || server.urlHost}
            </Text>
            <Text muted small className="mt-1 break-all">
              {server.canonicalServerUrl}
            </Text>
            {server.latestRequest && action.mode === "review" && (
              <div className="mt-4 grid grid-cols-2 gap-4">
                <div className="min-w-0">
                  <Text muted small>
                    Requester
                  </Text>
                  <Text variant="body" className="mt-1 truncate text-sm">
                    {server.latestRequest.requesterEmail}
                  </Text>
                </div>
                <div>
                  <Text muted small>
                    Requested
                  </Text>
                  <Text variant="body" className="mt-1 text-sm">
                    {formatShortDate(server.latestRequest.requestedAt)}
                  </Text>
                </div>
              </div>
            )}
          </section>

          {action.mode === "review" && (
            <RadioGroup
              value={decision}
              onValueChange={(value) => setDecision(value as ReviewDecision)}
              className="border-border grid grid-cols-2 gap-4 border p-3"
            >
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 border border-transparent px-3 py-2.5 transition-colors",
                  decision === "allow" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="allow" className="mt-1.5" />
                <span>
                  <Badge variant="success">
                    <Badge.Text>Approve</Badge.Text>
                  </Badge>
                  <Text muted small>
                    {isAllowAllReview
                      ? "Unblock the server for everyone in the project."
                      : "Add an allow decision."}
                  </Text>
                </span>
              </label>
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 border border-transparent px-3 py-2.5 transition-colors",
                  decision === "deny" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="deny" className="mt-1.5" />
                <span>
                  <Badge variant="destructive">
                    <Badge.Text>Deny</Badge.Text>
                  </Badge>
                  <Text muted small>
                    Resolve the request.
                  </Text>
                </span>
              </label>
            </RadioGroup>
          )}

          {needsPolicySelection && (
            <PolicySelection
              disabled={isSubmitting}
              emptyMessage={policyUnavailableMessage}
              members={members}
              onSelectionChange={setSelectedPolicyIDs}
              policies={shadowMCPPolicies}
              roles={roles}
              selectedPolicyIDs={selectedPolicyIDs}
            />
          )}

          {action.mode === "delete" && (
            <Text muted small>
              This removes the current allow decision for the URL.
            </Text>
          )}

          {isBlocklistAction && (
            <Text muted small>
              {action.mode === "block"
                ? "The block applies to everyone in the project immediately."
                : "The server becomes available to everyone in the project immediately."}
            </Text>
          )}
        </div>

        <SheetFooter>
          <Button
            className="w-full"
            disabled={!canSubmit}
            onClick={() => {
              void onSubmit({ action, decision, policyIDs: selectedPolicyIDs });
            }}
            variant={
              action.mode === "delete" || action.mode === "block"
                ? "destructive-primary"
                : "primary"
            }
          >
            <Button.LeftIcon>
              {isSubmitting && (
                <Icon name="loader-circle" className="animate-spin" />
              )}
            </Button.LeftIcon>
            <Button.Text>
              {actionSheetSubmitLabel(action.mode, decision)}
            </Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
