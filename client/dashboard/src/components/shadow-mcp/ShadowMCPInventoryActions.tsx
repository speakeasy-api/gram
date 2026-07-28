import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { Checkbox } from "@/components/ui/checkbox";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import {
  ALLOW_RULE_POLICY_REQUIRED,
  shadowMCPInventoryActions,
} from "./shadowMCPInventoryActionItems";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import { useEffect, useState } from "react";

export type ShadowMCPPolicy = Pick<
  RiskPolicy,
  "audienceType" | "audiencePrincipalUrns" | "id" | "name"
>;

export type InventoryActionMode = "review" | "add" | "edit" | "delete";
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
  }
}

function actionSheetSubmitLabel(
  mode: InventoryActionMode,
  decision: ReviewDecision,
) {
  if (mode === "review") {
    return decision === "allow" ? "Approve Request" : "Deny Request";
  }
  if (mode === "delete") {
    return "Delete Rule";
  }
  if (mode === "edit") {
    return "Save Changes";
  }
  return "Add Allow Rule";
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
  onOpenAction,
  server,
}: {
  canManageAllowRules: boolean;
  disabled: boolean;
  onOpenAction: (
    mode: InventoryActionMode,
    server: ShadowMCPInventoryServer,
  ) => void;
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  const actions = shadowMCPInventoryActions(server, {
    canManageAllowRules,
    disabled,
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
    <section className="border-border space-y-3 rounded-md border p-3">
      <Type variant="small" className="font-medium">
        Policies
      </Type>
      <div className="space-y-2">
        {policies.length === 0 && (
          <Type muted small>
            {emptyMessage}
          </Type>
        )}
        {policies.map((policy) => {
          const checked = selectedPolicyIDSet.has(policy.id);
          return (
            <label
              key={policy.id}
              className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 rounded-sm px-3 py-2.5 transition-colors"
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
                <Type variant="small" className="truncate font-medium">
                  {policy.name}
                </Type>
                <Type muted small>
                  Policy applies to{" "}
                  {policyAudienceLabel(policy, roles, members)}
                </Type>
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
  const canChoosePolicies =
    action.mode !== "delete" &&
    (action.mode !== "review" || decision === "allow");
  const needsPolicySelection = canChoosePolicies;
  const canSubmit =
    !isSubmitting &&
    (action.mode === "delete" ||
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
          <section className="border-border rounded-md border px-4 py-3">
            <Type variant="small" className="font-medium">
              {server.serverName || server.urlHost}
            </Type>
            <Type muted small className="mt-1 break-all">
              {server.canonicalServerUrl}
            </Type>
            {server.latestRequest && action.mode === "review" && (
              <div className="mt-4 grid grid-cols-2 gap-4">
                <div className="min-w-0">
                  <Type muted small>
                    Requester
                  </Type>
                  <Type variant="body" className="mt-1 truncate text-sm">
                    {server.latestRequest.requesterEmail}
                  </Type>
                </div>
                <div>
                  <Type muted small>
                    Requested
                  </Type>
                  <Type variant="body" className="mt-1 text-sm">
                    {formatShortDate(server.latestRequest.requestedAt)}
                  </Type>
                </div>
              </div>
            )}
          </section>

          {action.mode === "review" && (
            <RadioGroup
              value={decision}
              onValueChange={(value) => setDecision(value as ReviewDecision)}
              className="border-border grid grid-cols-2 gap-4 rounded-md border p-3"
            >
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 rounded-sm border border-transparent px-3 py-2.5 transition-colors",
                  decision === "allow" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="allow" className="mt-1.5" />
                <span>
                  <Badge variant="success">
                    <Badge.Text>Approve</Badge.Text>
                  </Badge>
                  <Type muted small>
                    Add an allow decision.
                  </Type>
                </span>
              </label>
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 rounded-sm border border-transparent px-3 py-2.5 transition-colors",
                  decision === "deny" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="deny" className="mt-1.5" />
                <span>
                  <Badge variant="destructive">
                    <Badge.Text>Deny</Badge.Text>
                  </Badge>
                  <Type muted small>
                    Resolve the request.
                  </Type>
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
            <Type muted small>
              This removes the current allow decision for the URL.
            </Type>
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
              action.mode === "delete" ? "destructive-primary" : "primary"
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
