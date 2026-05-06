import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { match } from "ts-pattern";
import { ExternalLink, ShieldCheck, UserRound } from "lucide-react";
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
import { Avatar } from "@/components/dashboard/Avatar";
import { InlineCopy } from "@/components/dashboard/InlineCopy";

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
 * Top-of-dashboard hero card. Surfaces the active "current user" with avatar,
 * id, admin toggle, and a switcher.
 *
 * Identity sources diverge by mode:
 *  - local-speakeasy: `user` is the source of truth (local row).
 *  - workos: `workos.workos_sub` identifies the WorkOS user, but admin status
 *    and the backing local id live on the shadow record fetched from
 *    `/devidp/workos/currentUser`.
 *  - oauth2 / oauth2-1: not a "current user" mode — explanatory state.
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
      <CardHeader className="border-b pb-4">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <UserRound className="size-4 text-muted-foreground" />
          Current user
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        {user ? (
          <UserHero
            displayName={user.display_name}
            secondary={user.email}
            idLabel="id"
            id={user.id}
            photoUrl={user.photo_url ?? null}
          />
        ) : (
          <EmptyState mode={mode} />
        )}

        {user && (
          <AdminToggleRow
            isAdmin={user.admin}
            loading={update.isPending}
            onToggle={() =>
              update.mutate({ id: user.id, admin: !user.admin })
            }
          />
        )}

        <div className="border-t pt-4 space-y-2">
          <Label className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground/80">
            {user ? "Switch to another user" : "Pick a user to begin"}
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
            {user && (
              <Button
                type="button"
                variant="ghost"
                disabled={clear.isPending}
                onClick={() => clear.mutate({ mode })}
              >
                {clear.isPending ? "Clearing…" : "Clear"}
              </Button>
            )}
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

  // /rpc/devIdp.getCurrentUser only persists workos_sub; first/last name and
  // email come back empty unless the WorkOS lookup was already populated. Re-
  // resolve via the proxy so we can render a real name and a profile photo.
  const lookupQ = useQuery<WorkosUserLookup>({
    queryKey: ["workos", "current-lookup", sub],
    queryFn: () =>
      fetchWorkos(
        `users/${encodeURIComponent(sub!)}`,
      ) as Promise<WorkosUserLookup>,
    enabled: !!sub,
  });

  // Shadow record gives us shadow_id (the local user row) and shadow_admin so
  // we can toggle admin and link memberships from it.
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
  const photo =
    lookupQ.data?.profile_picture_url ?? workos?.profile_picture_url ?? null;
  const shadowId = shadowQ.data?.shadow_id;
  const isAdmin = shadowQ.data?.shadow_admin ?? false;

  return (
    <Card>
      <CardHeader className="border-b pb-4">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <UserRound className="size-4 text-muted-foreground" />
          Current user
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        {workos ? (
          <UserHero
            displayName={fullName || email || "Unknown user"}
            secondary={fullName ? email : undefined}
            idLabel="workos_sub"
            id={workos.workos_sub}
            photoUrl={photo}
          />
        ) : (
          <EmptyState mode="workos" />
        )}

        {workos && shadowId && (
          <AdminToggleRow
            isAdmin={isAdmin}
            loading={update.isPending}
            onToggle={() =>
              update.mutate(
                { id: shadowId, admin: !isAdmin },
                {
                  onSuccess: () =>
                    qc.invalidateQueries({
                      queryKey: ["workos", "shadow", sub],
                    }),
                },
              )
            }
          />
        )}
        {workos && !shadowId && (
          <div className="rounded-md border border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
            Shadow record unavailable — admin can't be toggled until the user
            has logged in via Gram once.
          </div>
        )}

        <div className="border-t pt-4 space-y-2">
          <div className="flex items-baseline justify-between gap-2">
            <Label
              htmlFor="workos-sub-input"
              className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground/80"
            >
              {workos
                ? "Switch to another WorkOS user"
                : "Pick a WorkOS user to begin"}
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
            {workos && (
              <Button
                type="button"
                variant="ghost"
                disabled={clear.isPending}
                onClick={() => clear.mutate({ mode: "workos" })}
              >
                {clear.isPending ? "Clearing…" : "Clear"}
              </Button>
            )}
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

function UserHero({
  displayName,
  secondary,
  idLabel,
  id,
  photoUrl,
}: {
  displayName: string;
  secondary?: string;
  idLabel: string;
  id: string;
  photoUrl: string | null;
}) {
  return (
    <div className="flex items-center gap-4">
      <Avatar src={photoUrl} name={displayName} size="lg" />
      <div className="min-w-0 flex-1 space-y-1">
        <div className="text-xl font-semibold leading-tight truncate">
          {displayName}
        </div>
        {secondary && (
          <div className="text-sm text-muted-foreground truncate">
            {secondary}
          </div>
        )}
        <div className="-ml-1.5">
          <InlineCopy value={id} label={idLabel} />
        </div>
      </div>
    </div>
  );
}

function AdminToggleRow({
  isAdmin,
  loading,
  onToggle,
}: {
  isAdmin: boolean;
  loading: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      disabled={loading}
      aria-pressed={isAdmin}
      className={cn(
        "group/admin w-full text-left rounded-md border px-3 py-3",
        "flex items-center gap-3 transition-colors",
        "disabled:opacity-60 disabled:cursor-wait",
        isAdmin
          ? "border-[var(--retro-green)]/40 bg-[var(--retro-green)]/10 hover:bg-[var(--retro-green)]/15"
          : "border-border bg-muted/30 hover:bg-muted/60",
      )}
    >
      <ShieldCheck
        className={cn(
          "size-5 shrink-0 transition-colors",
          isAdmin ? "text-[var(--retro-green)]" : "text-muted-foreground",
        )}
        strokeWidth={isAdmin ? 2.5 : 2}
      />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium leading-tight">
          {isAdmin ? "Speakeasy admin" : "Standard user"}
        </div>
        <div className="text-xs text-muted-foreground leading-snug">
          {isAdmin
            ? "Has full Speakeasy admin scope. Click to revoke."
            : "Click to grant Speakeasy admin scope."}
        </div>
      </div>
      <Toggle on={isAdmin} loading={loading} />
    </button>
  );
}

function Toggle({ on, loading }: { on: boolean; loading: boolean }) {
  return (
    <span
      role="presentation"
      className={cn(
        "relative inline-flex h-5 w-9 shrink-0 rounded-full transition-colors",
        on ? "bg-[var(--retro-green)]" : "bg-muted-foreground/30",
        loading && "animate-pulse",
      )}
    >
      <span
        className={cn(
          "absolute top-0.5 size-4 rounded-full bg-card shadow transition-all",
          on ? "left-[18px]" : "left-0.5",
        )}
      />
    </span>
  );
}

function EmptyState({ mode }: { mode: Mode }) {
  return (
    <div className="flex items-center gap-4 py-1">
      <Avatar name="?" size="lg" className="opacity-60" />
      <div className="text-sm text-muted-foreground">
        {match(mode)
          .with(
            "workos",
            () => "No WorkOS user selected yet. Pick one below.",
          )
          .otherwise(() => "No current user selected. Pick one below.")}
      </div>
    </div>
  );
}

function OAuthIssuerEmptyState({ mode }: { mode: "oauth2" | "oauth2-1" }) {
  return (
    <Card>
      <CardHeader className="border-b pb-4">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <UserRound className="size-4 text-muted-foreground" />
          Current user
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-sm text-muted-foreground">
          Gram is currently using <code className="font-mono">{mode}</code> as
          an MCP OAuth issuer. There is no concept of a "current user" in this
          mode — switch to <code className="font-mono">local-speakeasy</code>{" "}
          or <code className="font-mono">workos</code> to manage user
          identity.
        </div>
      </CardContent>
    </Card>
  );
}
