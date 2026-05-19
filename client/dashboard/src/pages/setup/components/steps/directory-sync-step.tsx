import { useState, useEffect } from "react";
import { Users, Check, ChevronDown, ChevronUp, Loader2 } from "lucide-react";
import { StepContainer } from "../step-container";
import { MOCK_DIRECTORY_USERS } from "../../mock-data";
import type { DirectoryUser } from "../../types";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

interface DirectorySyncStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function DirectorySyncStep({
  onComplete,
  onBack,
}: DirectorySyncStepProps) {
  const [syncing, setSyncing] = useState(true);
  const [users, setUsers] = useState<DirectoryUser[]>([]);
  const [showAllMembers, setShowAllMembers] = useState(false);

  useEffect(() => {
    const timer = setTimeout(() => {
      setUsers(MOCK_DIRECTORY_USERS);
      setSyncing(false);
    }, 2000);
    return () => clearTimeout(timer);
  }, []);

  const admins = users.filter((u) => u.role === "admin");
  const members = users.filter((u) => u.role === "member");
  const displayedMembers = showAllMembers ? members : members.slice(0, 3);

  const toggleUserRole = (userId: string) => {
    setUsers((prev) =>
      prev.map((u) =>
        u.id === userId
          ? { ...u, role: u.role === "admin" ? "member" : "admin" }
          : u,
      ),
    );
  };

  if (syncing) {
    return (
      <StepContainer
        icon={
          <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
            <Users className="text-foreground h-6 w-6" />
          </div>
        }
        title="Syncing directory"
        description="Fetching users and groups from your identity provider..."
        onContinue={() => {}}
        showBack
        onBack={onBack}
        canContinue={false}
      >
        <div className="flex flex-col items-center justify-center py-16">
          <Loader2 className="text-muted-foreground mb-4 h-8 w-8 animate-spin" />
          <p className="text-muted-foreground text-sm">
            This may take a moment
          </p>
        </div>
      </StepContainer>
    );
  }

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Users className="text-foreground h-6 w-6" />
        </div>
      }
      title="Confirm directory sync"
      description="Review the users synced from your identity provider. Toggle roles as needed - admins can manage policies and settings."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <div className="space-y-6">
        {/* Summary stats */}
        <div className="grid grid-cols-2 gap-4">
          <div className="border-border bg-card rounded-lg border p-4">
            <p className="text-foreground text-2xl font-semibold">
              {admins.length}
            </p>
            <p className="text-muted-foreground text-sm">Administrators</p>
          </div>
          <div className="border-border bg-card rounded-lg border p-4">
            <p className="text-foreground text-2xl font-semibold">
              {members.length}
            </p>
            <p className="text-muted-foreground text-sm">Members</p>
          </div>
        </div>

        {/* Admins */}
        <div>
          <label className="text-muted-foreground text-sm font-medium tracking-wide uppercase">
            Administrators
          </label>
          <div className="mt-2 space-y-2">
            {admins.map((user) => (
              <UserRow
                key={user.id}
                user={user}
                onToggleRole={() => toggleUserRole(user.id)}
              />
            ))}
          </div>
        </div>

        {/* Members */}
        <div>
          <label className="text-muted-foreground text-sm font-medium tracking-wide uppercase">
            Members
          </label>
          <div className="mt-2 space-y-2">
            {displayedMembers.map((user) => (
              <UserRow
                key={user.id}
                user={user}
                onToggleRole={() => toggleUserRole(user.id)}
              />
            ))}
          </div>
          {members.length > 3 && (
            <button
              onClick={() => setShowAllMembers(!showAllMembers)}
              className="text-muted-foreground hover:text-foreground mt-3 flex items-center gap-1 text-sm transition-colors"
            >
              {showAllMembers ? (
                <>
                  <ChevronUp className="h-4 w-4" />
                  Show less
                </>
              ) : (
                <>
                  <ChevronDown className="h-4 w-4" />
                  Show {members.length - 3} more
                </>
              )}
            </button>
          )}
        </div>

        {/* Sync complete notice */}
        <div className="bg-success/5 border-success/20 rounded-lg border p-4">
          <div className="flex items-start gap-3">
            <div className="bg-success/10 mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center rounded">
              <Check className="text-success h-4 w-4" />
            </div>
            <div>
              <p className="text-foreground text-sm font-medium">
                Directory sync complete
              </p>
              <p className="text-muted-foreground mt-1 text-sm">
                {users.length} users imported. Roles will sync automatically
                going forward.
              </p>
            </div>
          </div>
        </div>
      </div>
    </StepContainer>
  );
}

function UserRow({
  user,
  onToggleRole,
}: {
  user: DirectoryUser;
  onToggleRole: () => void;
}) {
  return (
    <div className="border-border bg-card flex items-center gap-3 rounded-lg border p-3">
      <div className="bg-secondary flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full">
        <span className="text-foreground text-sm font-medium">
          {user.name
            .split(" ")
            .map((n) => n[0])
            .join("")}
        </span>
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-foreground truncate text-sm font-medium">
          {user.name}
        </p>
        <p className="text-muted-foreground truncate text-xs">{user.email}</p>
      </div>
      <button onClick={onToggleRole}>
        <Badge
          variant={user.role === "admin" ? "default" : "secondary"}
          className={cn(
            "cursor-pointer transition-colors",
            user.role === "admin" &&
              "bg-foreground text-background hover:bg-foreground/90",
          )}
        >
          {user.role === "admin" ? "Admin" : "Member"}
        </Badge>
      </button>
    </div>
  );
}
