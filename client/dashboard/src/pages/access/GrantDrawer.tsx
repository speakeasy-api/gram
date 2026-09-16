import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { Role } from "@gram/client/models/components/role.js";
import { ResolveChallengeFormResolutionType } from "@gram/client/models/components/resolvechallengeform.js";
import {
  invalidateAllChallenges,
  useChallenges,
} from "@gram/client/react-query/challenges.js";
import { useResolveChallengeMutation } from "@gram/client/react-query/resolveChallenge.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Check,
  ChevronRight,
  Loader2,
  Plus,
  Users,
} from "lucide-react";
import { useState } from "react";
import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { invalidateAllChallengeBuckets } from "@gram/client/react-query/challengeBuckets.js";
import {
  canAssignChallengeRole,
  principalDisplayName,
} from "./challengeHelpers";
import { rolesCoveringChallengeScopes } from "./roleSuggestions";
import { visiblePermissionCount } from "./roleDialogState";

type Step = "choose" | "select-role" | "confirm";

interface GrantDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  challenge: ChallengeBucket | null;
  challengeIds?: string[];
  onCreateNew: () => void;
  onResolved?: () => void;
}

export function GrantDrawer({
  open,
  onOpenChange,
  challenge,
  challengeIds: challengeIdsProp,
  onCreateNew,
  onResolved,
}: GrantDrawerProps): JSX.Element | null {
  const [step, setStep] = useState<Step>("choose");
  const [selectedRole, setSelectedRole] = useState<Role | null>(null);
  const [roleAssignmentConfirmed, setRoleAssignmentConfirmed] = useState(false);
  const queryClient = useQueryClient();
  const { data: rolesData } = useRoles();
  const allRoles = rolesData?.roles ?? [];
  const canAssignRole = challenge ? canAssignChallengeRole(challenge) : false;
  const challengeIds = challengeIdsProp ?? (challenge ? [challenge.id] : []);
  const {
    data: challengeData,
    isPending: areChallengesPending,
    isError: challengesFailed,
  } = useChallenges(
    open && canAssignRole && challengeIds.length > 0
      ? { ids: challengeIds }
      : undefined,
    undefined,
    { enabled: open && canAssignRole && challengeIds.length > 0 },
  );
  // Challenge resolution adds one role without replacing the member's current
  // roles. System-role assignment stays on the full member-management flow;
  // this focused shortcut only offers custom roles whose grants cover every
  // complete selector captured by the bucket's challenges.
  const hasAllChallengeDetails =
    challengeData?.challenges.length === challengeIds.length;
  const roles =
    challenge && canAssignRole && hasAllChallengeDetails
      ? rolesCoveringChallengeScopes(
          allRoles.filter((role) => !role.isSystem),
          challengeData.challenges,
        )
      : [];

  const hasMatchingRoles = roles.length > 0;

  const resolveChallenge = useResolveChallengeMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllChallenges(queryClient),
        invalidateAllChallengeBuckets(queryClient),
      ]);
    },
  });

  const handleClose = () => {
    onOpenChange(false);
    setTimeout(() => {
      setStep("choose");
      setSelectedRole(null);
      setRoleAssignmentConfirmed(false);
    }, 300);
  };

  const handleCreateNew = () => {
    handleClose();
    setTimeout(onCreateNew, 350);
  };

  const handlePickRole = (role: Role) => {
    setSelectedRole(role);
    setRoleAssignmentConfirmed(false);
    setStep("confirm");
  };

  const handleSave = () => {
    if (!challenge || !selectedRole) return;
    const ids = challengeIds;
    resolveChallenge.mutate(
      {
        request: {
          resolveChallengeForm: {
            challengeIds: ids,
            principalUrn: challenge.principalUrn,
            scope: challenge.scope,
            resolutionType: ResolveChallengeFormResolutionType.RoleAssigned,
            roleSlug: selectedRole.slug,
            roleAssignmentConfirmed,
            resourceKind: challenge.resourceKind,
            resourceId: challenge.resourceId,
          },
        },
      },
      {
        onSuccess: () => {
          onResolved?.();
          handleClose();
        },
      },
    );
  };

  const handleDismiss = () => {
    if (!challenge) return;
    const ids = challengeIds;
    resolveChallenge.mutate(
      {
        request: {
          resolveChallengeForm: {
            challengeIds: ids,
            principalUrn: challenge.principalUrn,
            scope: challenge.scope,
            resolutionType: ResolveChallengeFormResolutionType.Dismissed,
            resourceKind: challenge.resourceKind,
            resourceId: challenge.resourceId,
          },
        },
      },
      {
        onSuccess: () => {
          onResolved?.();
          handleClose();
        },
      },
    );
  };

  if (!challenge) return null;

  const principalDisplay = principalDisplayName(
    challenge.userEmail,
    challenge.principalUrn,
  );

  const stepTitle = {
    choose: "Grant Access",
    "select-role": "Select a Role",
    confirm: "Confirm Assignment",
  }[step];

  const stepOffset = {
    choose: "translate-x-0",
    "select-role": "-translate-x-full",
    confirm: "-translate-x-[200%]",
  }[step];

  return (
    <Sheet open={open} onOpenChange={handleClose}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-md"
      >
        <SheetHeader>
          <SheetTitle>{stepTitle}</SheetTitle>
          <SheetDescription>
            {step === "choose" && (
              <>
                Grant{" "}
                <code className="bg-muted px-1 font-mono text-xs">
                  {challenge.scope}
                </code>{" "}
                access to <strong>{principalDisplay}</strong>
              </>
            )}
            {step === "select-role" && "Choose a role to assign this user to."}
            {step === "confirm" && selectedRole && (
              <>
                Assign <strong>{principalDisplay}</strong> to the{" "}
                <strong>{selectedRole.name}</strong> role.
              </>
            )}
          </SheetDescription>
        </SheetHeader>

        <div className="relative flex-1 overflow-hidden">
          <div
            className={cn(
              "flex h-full transition-transform duration-300 ease-in-out",
              stepOffset,
            )}
          >
            {/* Step 1: Choose action */}
            <div className="w-full shrink-0 space-y-3 overflow-y-auto px-4">
              {!canAssignRole ? (
                <div className="space-y-4">
                  <Text
                    variant="body"
                    className="text-muted-foreground text-sm"
                  >
                    Roles can only be assigned to one active organization user.
                    This challenge can be dismissed, but access for this
                    identity must be managed through its own settings.
                  </Text>
                  <Button
                    variant="secondary"
                    className="w-full"
                    onClick={handleDismiss}
                    disabled={resolveChallenge.isPending}
                  >
                    <Button.Text>
                      {resolveChallenge.isPending
                        ? "Dismissing…"
                        : "Dismiss challenge"}
                    </Button.Text>
                  </Button>
                </div>
              ) : areChallengesPending ? (
                <div className="border-border flex w-full items-center gap-3 border p-4 text-left">
                  <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
                  <Text
                    variant="body"
                    className="text-muted-foreground text-sm"
                  >
                    Checking roles against the denied access…
                  </Text>
                </div>
              ) : challengesFailed || !hasAllChallengeDetails ? (
                <div className="border-border space-y-2 border p-4 text-left">
                  <Text variant="body" className="font-medium">
                    Challenge details are unavailable
                  </Text>
                  <Text
                    variant="body"
                    className="text-muted-foreground text-sm"
                  >
                    Refresh before assigning a role. The challenge can still be
                    dismissed.
                  </Text>
                  <Button
                    variant="secondary"
                    className="w-full"
                    onClick={handleDismiss}
                    disabled={resolveChallenge.isPending}
                  >
                    <Button.Text>Dismiss challenge</Button.Text>
                  </Button>
                </div>
              ) : hasMatchingRoles ? (
                <button
                  type="button"
                  onClick={() => setStep("select-role")}
                  className="border-border hover:bg-muted/50 flex w-full items-center gap-3 border p-4 text-left transition-colors"
                >
                  <div className="bg-muted flex h-10 w-10 items-center justify-center">
                    <Users className="h-5 w-5" />
                  </div>
                  <div className="flex-1">
                    <Text variant="body" className="font-medium">
                      Add to existing role
                    </Text>
                    <Text
                      variant="body"
                      className="text-muted-foreground text-sm"
                    >
                      Assign to a role that already includes the required
                      permissions.
                    </Text>
                  </div>
                  <ChevronRight className="text-muted-foreground h-5 w-5 shrink-0" />
                </button>
              ) : (
                <SimpleTooltip
                  tooltip={`No roles have the ${challenge.scope} scope`}
                >
                  <div className="border-border flex w-full cursor-not-allowed items-center gap-3 border p-4 text-left opacity-50">
                    <div className="bg-muted flex h-10 w-10 items-center justify-center">
                      <Users className="h-5 w-5" />
                    </div>
                    <div className="flex-1">
                      <Text variant="body" className="font-medium">
                        Add to existing role
                      </Text>
                      <Text
                        variant="body"
                        className="text-muted-foreground text-sm"
                      >
                        No roles include the required permissions.
                      </Text>
                    </div>
                  </div>
                </SimpleTooltip>
              )}

              {canAssignRole && (
                <button
                  type="button"
                  onClick={handleCreateNew}
                  className="border-border hover:bg-muted/50 flex w-full items-center gap-3 border p-4 text-left transition-colors"
                >
                  <div className="bg-muted flex h-10 w-10 items-center justify-center">
                    <Plus className="h-5 w-5" />
                  </div>
                  <div className="flex-1">
                    <Text variant="body" className="font-medium">
                      Create new role
                    </Text>
                    <Text
                      variant="body"
                      className="text-muted-foreground text-sm"
                    >
                      Define a new role with the exact permissions needed.
                    </Text>
                  </div>
                  <ChevronRight className="text-muted-foreground h-5 w-5 shrink-0" />
                </button>
              )}
            </div>

            {/* Step 2: Role list */}
            <div className="w-full shrink-0 overflow-y-auto px-4">
              <button
                type="button"
                onClick={() => setStep("choose")}
                className="text-muted-foreground hover:text-foreground mb-3 flex items-center gap-1 text-sm transition-colors"
              >
                <ArrowLeft className="h-4 w-4" />
                Back
              </button>

              <div className="border-border divide-border divide-y border">
                {roles.map((role) => (
                  <button
                    key={role.id}
                    type="button"
                    onClick={() => handlePickRole(role)}
                    className="hover:bg-muted/50 flex w-full items-center justify-between px-4 py-3 text-left transition-colors"
                  >
                    <div>
                      <div className="flex items-center gap-2">
                        <Text variant="body" className="font-medium">
                          {role.name}
                        </Text>
                      </div>
                      <Text
                        variant="body"
                        className="text-muted-foreground text-sm"
                      >
                        {visiblePermissionCount(role.grants)} permissions
                        &middot; {role.memberCount} members
                      </Text>
                    </div>
                    <ChevronRight className="text-muted-foreground h-4 w-4 shrink-0" />
                  </button>
                ))}
              </div>
            </div>

            {/* Step 3: Confirm assignment */}
            <div className="w-full shrink-0 overflow-y-auto px-4">
              <button
                type="button"
                onClick={() => setStep("select-role")}
                className="text-muted-foreground hover:text-foreground mb-3 flex items-center gap-1 text-sm transition-colors"
              >
                <ArrowLeft className="h-4 w-4" />
                Back
              </button>

              {selectedRole && (
                <div className="space-y-4">
                  <div className="border-border border p-4">
                    <div className="space-y-3">
                      <div className="flex items-center justify-between">
                        <Text
                          variant="body"
                          className="text-muted-foreground text-sm"
                        >
                          Identity
                        </Text>
                        <Text variant="body" className="text-sm font-medium">
                          {principalDisplay}
                        </Text>
                      </div>
                      <div className="flex items-center justify-between">
                        <Text
                          variant="body"
                          className="text-muted-foreground text-sm"
                        >
                          Role
                        </Text>
                        <Text variant="body" className="text-sm font-medium">
                          {selectedRole.name}
                        </Text>
                      </div>
                      <div className="flex items-center justify-between">
                        <Text
                          variant="body"
                          className="text-muted-foreground text-sm"
                        >
                          Scope
                        </Text>
                        <code className="bg-muted px-1.5 py-0.5 font-mono text-xs">
                          {challenge.scope}
                        </code>
                      </div>
                      <div className="space-y-2">
                        <Text
                          variant="body"
                          className="text-muted-foreground text-sm"
                        >
                          All role permissions
                        </Text>
                        <ul className="border-border divide-border max-h-40 divide-y overflow-y-auto border">
                          {[...selectedRole.grants]
                            .sort((a, b) => a.scope.localeCompare(b.scope))
                            .map((grant, index) => (
                              <li
                                key={`${grant.scope}-${index}`}
                                className="space-y-1 px-3 py-2"
                              >
                                <code className="font-mono text-xs">
                                  {grant.scope}
                                </code>
                                <Text
                                  variant="body"
                                  className="text-muted-foreground break-all text-xs"
                                >
                                  {grant.selectors && grant.selectors.length > 0
                                    ? grant.selectors
                                        .map((selector) =>
                                          Object.entries(selector)
                                            .filter(
                                              ([, value]) =>
                                                value !== undefined,
                                            )
                                            .map(
                                              ([key, value]) =>
                                                `${key}=${value}`,
                                            )
                                            .join(", "),
                                        )
                                        .join("; ")
                                    : "All matching resources"}
                                </Text>
                              </li>
                            ))}
                        </ul>
                      </div>
                    </div>
                  </div>

                  <label className="border-border flex cursor-pointer items-start gap-3 border p-3">
                    <Checkbox
                      checked={roleAssignmentConfirmed}
                      onCheckedChange={(checked) =>
                        setRoleAssignmentConfirmed(checked === true)
                      }
                      aria-label="Confirm all role permissions"
                      className="mt-0.5"
                    />
                    <Text variant="body" className="text-sm">
                      I reviewed the complete role. Assigning it grants all
                      permissions above, not only {challenge.scope}.
                    </Text>
                  </label>

                  <Button
                    className="w-full"
                    onClick={handleSave}
                    disabled={
                      resolveChallenge.isPending || !roleAssignmentConfirmed
                    }
                  >
                    <Button.LeftIcon>
                      <Check className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>
                      {resolveChallenge.isPending
                        ? "Assigning…"
                        : `Assign to ${selectedRole.name}`}
                    </Button.Text>
                  </Button>
                </div>
              )}
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
