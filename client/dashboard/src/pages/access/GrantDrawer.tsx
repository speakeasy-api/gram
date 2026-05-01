import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { Role } from "@gram/client/models/components/role.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { ArrowLeft, Check, ChevronRight, Plus, Users } from "lucide-react";
import { useState } from "react";
import type { AuthzChallenge } from "./ChallengesTab";

type Step = "choose" | "select-role" | "confirm";

interface GrantDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  challenge: AuthzChallenge | null;
  onCreateNew: () => void;
}

export function GrantDrawer({
  open,
  onOpenChange,
  challenge,
  onCreateNew,
}: GrantDrawerProps) {
  const [step, setStep] = useState<Step>("choose");
  const [selectedRole, setSelectedRole] = useState<Role | null>(null);
  const { data: rolesData } = useRoles();
  const allRoles = rolesData?.roles ?? [];
  const roles = challenge
    ? allRoles.filter((r) => r.grants.some((g) => g.scope === challenge.scope))
    : [];

  const hasMatchingRoles = roles.length > 0;

  const handleClose = () => {
    onOpenChange(false);
    setTimeout(() => {
      setStep("choose");
      setSelectedRole(null);
    }, 300);
  };

  const handleCreateNew = () => {
    handleClose();
    setTimeout(onCreateNew, 350);
  };

  const handlePickRole = (role: Role) => {
    setSelectedRole(role);
    setStep("confirm");
  };

  const handleSave = () => {
    // TODO: call API to assign principal to role
    handleClose();
  };

  if (!challenge) return null;

  const principalDisplay = challenge.userEmail ?? challenge.principalUrn;

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
                <code className="bg-muted rounded px-1 font-mono text-xs">
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
              {hasMatchingRoles ? (
                <button
                  type="button"
                  onClick={() => setStep("select-role")}
                  className="border-border hover:bg-muted/50 flex w-full items-center gap-3 rounded-lg border p-4 text-left transition-colors"
                >
                  <div className="bg-muted flex h-10 w-10 items-center justify-center rounded-lg">
                    <Users className="h-5 w-5" />
                  </div>
                  <div className="flex-1">
                    <Type variant="body" className="font-medium">
                      Add to existing role
                    </Type>
                    <Type
                      variant="body"
                      className="text-muted-foreground text-sm"
                    >
                      Assign to a role that already includes the required
                      permissions.
                    </Type>
                  </div>
                  <ChevronRight className="text-muted-foreground h-5 w-5 shrink-0" />
                </button>
              ) : (
                <SimpleTooltip
                  tooltip={`No roles have the ${challenge.scope} scope`}
                >
                  <div className="border-border flex w-full cursor-not-allowed items-center gap-3 rounded-lg border p-4 text-left opacity-50">
                    <div className="bg-muted flex h-10 w-10 items-center justify-center rounded-lg">
                      <Users className="h-5 w-5" />
                    </div>
                    <div className="flex-1">
                      <Type variant="body" className="font-medium">
                        Add to existing role
                      </Type>
                      <Type
                        variant="body"
                        className="text-muted-foreground text-sm"
                      >
                        No roles include the required permissions.
                      </Type>
                    </div>
                  </div>
                </SimpleTooltip>
              )}

              <button
                type="button"
                onClick={handleCreateNew}
                className="border-border hover:bg-muted/50 flex w-full items-center gap-3 rounded-lg border p-4 text-left transition-colors"
              >
                <div className="bg-muted flex h-10 w-10 items-center justify-center rounded-lg">
                  <Plus className="h-5 w-5" />
                </div>
                <div className="flex-1">
                  <Type variant="body" className="font-medium">
                    Create new role
                  </Type>
                  <Type
                    variant="body"
                    className="text-muted-foreground text-sm"
                  >
                    Define a new role with the exact permissions needed.
                  </Type>
                </div>
                <ChevronRight className="text-muted-foreground h-5 w-5 shrink-0" />
              </button>
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

              <div className="border-border divide-border divide-y rounded-md border">
                {roles.map((role) => (
                  <button
                    key={role.id}
                    type="button"
                    onClick={() => handlePickRole(role)}
                    className="hover:bg-muted/50 flex w-full items-center justify-between px-4 py-3 text-left transition-colors"
                  >
                    <div>
                      <div className="flex items-center gap-2">
                        <Type variant="body" className="font-medium">
                          {role.name}
                        </Type>
                        {role.isSystem && (
                          <Badge variant="neutral">
                            <Badge.Text>System</Badge.Text>
                          </Badge>
                        )}
                      </div>
                      <Type
                        variant="body"
                        className="text-muted-foreground text-sm"
                      >
                        {role.grants.length} permissions &middot;{" "}
                        {role.memberCount} members
                      </Type>
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
                  <div className="border-border rounded-md border p-4">
                    <div className="space-y-3">
                      <div className="flex items-center justify-between">
                        <Type
                          variant="body"
                          className="text-muted-foreground text-sm"
                        >
                          Identity
                        </Type>
                        <Type variant="body" className="text-sm font-medium">
                          {principalDisplay}
                        </Type>
                      </div>
                      <div className="flex items-center justify-between">
                        <Type
                          variant="body"
                          className="text-muted-foreground text-sm"
                        >
                          Role
                        </Type>
                        <div className="flex items-center gap-2">
                          <Type variant="body" className="text-sm font-medium">
                            {selectedRole.name}
                          </Type>
                          {selectedRole.isSystem && (
                            <Badge variant="neutral">
                              <Badge.Text>System</Badge.Text>
                            </Badge>
                          )}
                        </div>
                      </div>
                      <div className="flex items-center justify-between">
                        <Type
                          variant="body"
                          className="text-muted-foreground text-sm"
                        >
                          Scope
                        </Type>
                        <code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
                          {challenge.scope}
                        </code>
                      </div>
                      <div className="flex items-center justify-between">
                        <Type
                          variant="body"
                          className="text-muted-foreground text-sm"
                        >
                          Permissions
                        </Type>
                        <Type variant="body" className="text-sm">
                          {selectedRole.grants.length}
                        </Type>
                      </div>
                    </div>
                  </div>

                  <Button className="w-full" onClick={handleSave}>
                    <Button.LeftIcon>
                      <Check className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Assign to {selectedRole.name}</Button.Text>
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
