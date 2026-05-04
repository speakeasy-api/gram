import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useCurrentUser, useSetCurrentUser } from "@/hooks/use-devidp";

async function fetchWorkos(suffix: string): Promise<unknown> {
  const res = await fetch(`/devidp/workos/${suffix}`);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

export function WorkosModePane() {
  const cur = useCurrentUser("workos");
  const set = useSetCurrentUser();
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
            <Label htmlFor="workos-sub">Set workos_sub or email</Label>
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
