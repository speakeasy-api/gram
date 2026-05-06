import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { match } from "ts-pattern";
import { ExternalLink, ShieldCheck, ShieldOff, UserRound } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  useClearCurrentUser,
  useSetCurrentUser,
  useUpdateUser,
  useUsers,
} from "@/hooks/use-devidp";
import type { Mode, User, WorkosCurrentUser } from "@/lib/devidp";

const WORKOS_USERS_DASHBOARD_URL =
  "https://dashboard.workos.com/environment_01J5C09A9KMAHSZ0T9WBK3TXHJ/users";

interface WorkosUserLookup {
  id?: string;
  email?: string;
  first_name?: string;
  last_name?: string;
  profile_picture_url?: string;
}

interface WorkosShadow extends WorkosUserLookup {
  workos_sub: string;
  shadow_id?: string;
  shadow_admin: boolean;
}

async function fetchWorkos(suffix: string): Promise<unknown> {
  const res = await fetch(`/devidp/workos/${suffix}`);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

interface Props {
  mode: Mode;
  user: User | null;
  workos: WorkosCurrentUser | null;
}

/**
 * Top-of-dashboard card that surfaces who Gram thinks the current user is and
 * exposes the two destructive-ish toggles a developer reaches for most: admin
 * on/off and switching to another user.
 *
 * Mode handling diverges around identity sources:
 *  - local-speakeasy: `user` is the source of truth (local row), id is a uuid.
 *  - workos: `workos.workos_sub` is set, but admin status and the "real"
 *    backing user id live on the local shadow record fetched from
 *    `/devidp/workos/currentUser`. We re-look up the WorkOS profile to get a
 *    pretty display name when available.
 *  - oauth2 / oauth2-1: not a "current user" mode; render an explanation.
 */
export function CurrentUserCard({ mode, user, workos }: Props) {
  if (mode === "oauth2" || mode === "oauth2-1") {
    return <OAuthIssuerEmptyState mode={mode} />;
  }
  if (mode === "workos") {
    return <WorkosUserCard workos={workos} />;
  }
  return <LocalUserCard user={user} mode={mode} />;
}

function LocalUserCard({
  user,
  mode,
}: {
  user: User | null;
  mode: Exclude<Mode, "workos">;
}) {
  const usersQ = useUsers();
  const set = useSetCurrentUser();
  const clear = useClearCurrentUser();
  const update = useUpdateUser();

  const users = usersQ.data?.items ?? [];
  const [picked, setPicked] = useState("");

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <UserRound className="size-4 text-muted-foreground" />
          Current user
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        {user ? (
          <UserSummary
            displayName={user.display_name}
            secondary={user.email}
            idLabel="id"
            id={user.id}
            isAdmin={user.admin}
            onToggleAdmin={() =>
              update.mutate({ id: user.id, admin: !user.admin })
            }
            adminLoading={update.isPending}
            onClear={() => clear.mutate({ mode })}
            clearLoading={clear.isPending}
          />
        ) : (
          <EmptyState mode={mode} />
        )}

        <Divider />

        <div className="space-y-2">
          <Label className="text-xs text-muted-foreground uppercase tracking-wide">
            {user ? "Switch to another user" : "Pick a user"}
          </Label>
          <div className="flex gap-2">
            <Select value={picked} onValueChange={(v) => setPicked(v ?? "")}>
              <SelectTrigger className="flex-1">
                <SelectValue placeholder="Select user…">
                  {(value: string | null) => {
                    const u = value ? users.find((u) => u.id === value) : null;
                    return u ? `${u.display_name} (${u.email})` : value;
                  }}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {users.map((u) => (
                  <SelectItem key={u.id} value={u.id}>
                    {u.display_name} ({u.email})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              type="button"
              disabled={!picked || set.isPending}
              onClick={() =>
                set.mutate(
                  { mode, user_id: picked },
                  { onSuccess: () => setPicked("") },
                )
              }
            >
              {set.isPending ? "Switching…" : user ? "Switch" : "Set"}
            </Button>
          </div>
          {set.error && (
            <div className="text-xs text-destructive">
              {(set.error as Error).message}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function WorkosUserCard({ workos }: { workos: WorkosCurrentUser | null }) {
  const set = useSetCurrentUser();
  const clear = useClearCurrentUser();
  const update = useUpdateUser();
  const qc = useQueryClient();
  const [input, setInput] = useState("");

  const sub = workos?.workos_sub;

  // The /rpc/devIdp.getCurrentUser response only persists workos_sub; first /
  // last name and email come back empty unless the WorkOS lookup was already
  // populated. Re-resolve via the proxy here.
  const lookupQ = useQuery<WorkosUserLookup>({
    queryKey: ["workos", "current-lookup", sub],
    queryFn: () =>
      fetchWorkos(
        `users/${encodeURIComponent(sub!)}`,
      ) as Promise<WorkosUserLookup>,
    enabled: !!sub,
  });

  // Shadow record gives us shadow_id and shadow_admin so we can toggle admin
  // and link the WorkOS user to its local row (organisations live there).
  const shadowQ = useQuery<WorkosShadow>({
    queryKey: ["workos", "shadow", sub],
    queryFn: () => fetchWorkos("currentUser") as Promise<WorkosShadow>,
    enabled: !!sub,
  });

  const preview = useQuery({
    queryKey: ["workos", "preview", input],
    queryFn: () => fetchWorkos(`users/${encodeURIComponent(input)}`),
    enabled: false,
  });

  const fullName = [
    lookupQ.data?.first_name ?? workos?.first_name,
    lookupQ.data?.last_name ?? workos?.last_name,
  ]
    .filter(Boolean)
    .join(" ");
  const email = lookupQ.data?.email ?? workos?.email ?? "";
  const shadowId = shadowQ.data?.shadow_id;
  const isAdmin = shadowQ.data?.shadow_admin ?? false;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <UserRound className="size-4 text-muted-foreground" />
          Current user
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        {workos ? (
          <UserSummary
            displayName={fullName || email || "Unknown user"}
            secondary={fullName ? email : undefined}
            idLabel="workos_sub"
            id={workos.workos_sub}
            isAdmin={isAdmin}
            onToggleAdmin={
              shadowId
                ? () =>
                    update.mutate(
                      { id: shadowId, admin: !isAdmin },
                      {
                        onSuccess: () =>
                          qc.invalidateQueries({
                            queryKey: ["workos", "shadow", sub],
                          }),
                      },
                    )
                : undefined
            }
            adminLoading={update.isPending}
            onClear={() => clear.mutate({ mode: "workos" })}
            clearLoading={clear.isPending}
            adminUnavailable={!shadowId}
          />
        ) : (
          <EmptyState mode="workos" />
        )}

        <Divider />

        <div className="space-y-2">
          <div className="flex items-baseline justify-between gap-2">
            <Label
              htmlFor="workos-sub-input"
              className="text-xs text-muted-foreground uppercase tracking-wide"
            >
              {workos ? "Switch to another WorkOS user" : "Pick a WorkOS user"}
            </Label>
            <a
              href={WORKOS_USERS_DASHBOARD_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-[var(--retro-orange)] underline underline-offset-2"
            >
              Browse in WorkOS
              <ExternalLink className="size-3" />
            </a>
          </div>
          <div className="flex gap-2">
            <Input
              id="workos-sub-input"
              className="flex-1 font-mono"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="user_01H… or alice@example.com"
            />
            <Button
              type="button"
              variant="outline"
              disabled={!input || preview.isFetching}
              onClick={() => preview.refetch()}
            >
              {preview.isFetching ? "…" : "Preview"}
            </Button>
            <Button
              type="button"
              disabled={!input || set.isPending}
              onClick={() =>
                set.mutate(
                  { mode: "workos", workos_sub: input },
                  { onSuccess: () => setInput("") },
                )
              }
            >
              {set.isPending ? "Saving…" : workos ? "Switch" : "Set"}
            </Button>
          </div>
          {preview.error && (
            <div className="text-xs text-destructive">
              {(preview.error as Error).message}
            </div>
          )}
          {preview.data !== undefined && (
            <pre className="text-xs bg-muted rounded-md p-3 overflow-x-auto max-h-48">
              <code>{JSON.stringify(preview.data, null, 2)}</code>
            </pre>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function UserSummary({
  displayName,
  secondary,
  idLabel,
  id,
  isAdmin,
  onToggleAdmin,
  adminLoading,
  onClear,
  clearLoading,
  adminUnavailable,
}: {
  displayName: string;
  secondary?: string;
  idLabel: string;
  id: string;
  isAdmin: boolean;
  onToggleAdmin?: () => void;
  adminLoading: boolean;
  onClear: () => void;
  clearLoading: boolean;
  adminUnavailable?: boolean;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0 space-y-1">
        <div className="text-base font-semibold leading-tight truncate">
          {displayName}
        </div>
        {secondary && (
          <div className="text-sm text-muted-foreground truncate">
            {secondary}
          </div>
        )}
        <div className="text-xs text-muted-foreground">
          <span className="font-mono uppercase tracking-wider mr-1">
            {idLabel}
          </span>
          <code className="font-mono">{id}</code>
        </div>

        <div className="flex items-center gap-2 pt-2">
          <AdminBadge isAdmin={isAdmin} />
          {onToggleAdmin && (
            <Button
              type="button"
              variant="outline"
              size="xs"
              disabled={adminLoading}
              onClick={onToggleAdmin}
            >
              {adminLoading ? "…" : isAdmin ? "Revoke admin" : "Make admin"}
            </Button>
          )}
          {adminUnavailable && (
            <span className="text-[11px] text-muted-foreground italic">
              Shadow record unavailable
            </span>
          )}
        </div>
      </div>

      <Button
        type="button"
        variant="ghost"
        size="xs"
        disabled={clearLoading}
        onClick={onClear}
      >
        {clearLoading ? "Clearing…" : "Clear"}
      </Button>
    </div>
  );
}

function AdminBadge({ isAdmin }: { isAdmin: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-[10px] font-mono uppercase tracking-wider border",
        isAdmin
          ? "bg-[var(--retro-green)]/15 border-[var(--retro-green)]/40 text-foreground"
          : "bg-muted/40 border-border text-muted-foreground",
      )}
    >
      {isAdmin ? (
        <ShieldCheck className="size-3" />
      ) : (
        <ShieldOff className="size-3" />
      )}
      {isAdmin ? "admin" : "not admin"}
    </span>
  );
}

function EmptyState({ mode }: { mode: Mode }) {
  return (
    <div className="text-sm text-muted-foreground italic">
      {match(mode)
        .with("workos", () => "No WorkOS user selected yet.")
        .otherwise(() => "No current user selected yet for this mode.")}
    </div>
  );
}

function OAuthIssuerEmptyState({ mode }: { mode: "oauth2" | "oauth2-1" }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <UserRound className="size-4 text-muted-foreground" />
          Current user
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-sm text-muted-foreground">
          Gram is currently using <code className="font-mono">{mode}</code> as
          an MCP OAuth issuer. There is no concept of a "current user" in this
          mode — switch to <code className="font-mono">local-speakeasy</code>{" "}
          or <code className="font-mono">workos</code> to manage user identity.
        </div>
      </CardContent>
    </Card>
  );
}

function Divider() {
  return <div className="border-t border-border" />;
}
