import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  useClearCurrentUser,
  useCurrentUser,
  useSetCurrentUser,
} from "@/hooks/use-devidp";

const WORKOS_USERS_DASHBOARD_URL =
  "https://dashboard.workos.com/environment_01J5C09A9KMAHSZ0T9WBK3TXHJ/users";

async function fetchWorkos(suffix: string): Promise<unknown> {
  const res = await fetch(`/devidp/workos/${suffix}`);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

export function WorkosModePane() {
  const cur = useCurrentUser("workos");
  const set = useSetCurrentUser();
  const clear = useClearCurrentUser();
  const [input, setInput] = useState("");

  const preview = useQuery({
    queryKey: ["workos", "preview", input],
    queryFn: () => fetchWorkos(`users/${encodeURIComponent(input)}`),
    enabled: false,
  });

  const current = cur.data?.workos;

  return (
    <div className="space-y-6">
      <Card size="sm">
        <CardHeader>
          <CardTitle>Current user (WorkOS)</CardTitle>
          {current && (
            <CardAction>
              <Button
                type="button"
                variant="ghost"
                size="xs"
                disabled={clear.isPending}
                onClick={() => clear.mutate({ mode: "workos" })}
              >
                {clear.isPending ? "Clearing…" : "Clear"}
              </Button>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="space-y-4">
          {cur.isLoading ? (
            <div className="text-sm text-muted-foreground">Loading…</div>
          ) : current ? (
            <div className="text-sm space-y-1">
              <div className="font-medium">
                {[current.first_name, current.last_name]
                  .filter(Boolean)
                  .join(" ") ||
                  current.email ||
                  current.workos_sub}
              </div>
              {current.email && (
                <div className="text-muted-foreground">{current.email}</div>
              )}
              <div className="text-xs text-muted-foreground">
                workos_sub: <code>{current.workos_sub}</code>
              </div>
            </div>
          ) : (
            <div className="text-sm text-muted-foreground italic">
              No currentUser set yet.
            </div>
          )}

          <div className="border-t border-border pt-4 space-y-2">
            <div className="flex items-baseline justify-between gap-3">
              <Label htmlFor="workos-sub">Set workos_sub or email</Label>
              <a
                href={WORKOS_USERS_DASHBOARD_URL}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-[var(--retro-orange)] underline underline-offset-2"
              >
                Browse users in WorkOS
                <ExternalLink className="size-3" />
              </a>
            </div>
            <div className="flex gap-2">
              <Input
                id="workos-sub"
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
                {set.isPending ? "Saving…" : "Save"}
              </Button>
            </div>
            {preview.error && (
              <div className="text-xs text-destructive">
                {(preview.error as Error).message}
              </div>
            )}
            {preview.data !== undefined && (
              <pre className="text-xs bg-muted rounded-2xl p-3 overflow-x-auto max-h-60 mt-2">
                <code>{JSON.stringify(preview.data, null, 2)}</code>
              </pre>
            )}
          </div>
        </CardContent>
      </Card>

      <p className="text-xs text-muted-foreground">
        WorkOS data is fetched live from the upstream WorkOS REST API via the
        dev-idp's <code>/workos/*</code> passthrough. Local users and orgs from{" "}
        <code>/rpc/users.list</code> do not apply to this mode.
      </p>
    </div>
  );
}
