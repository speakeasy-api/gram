import { useState } from "react";
import { match } from "ts-pattern";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import {
  useCurrentUser,
  useSetCurrentUser,
  useUsers,
} from "@/hooks/use-devidp";
import type { Mode } from "@/lib/devidp";
import { DiscoveryPane } from "@/components/providers/DiscoveryPane";

type LocalMode = Exclude<Mode, "workos">;

export function LocalModePane({ mode }: { mode: LocalMode }) {
  const cur = useCurrentUser(mode);
  const usersQ = useUsers();
  const set = useSetCurrentUser();
  const [picked, setPicked] = useState<string>("");

  const users = usersQ.data?.items ?? [];
  const current = cur.data;

  return (
    <div className="space-y-6">
      <Card size="sm">
        <CardHeader>
          <CardTitle>Current user</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {cur.isLoading ? (
            <div className="text-sm text-muted-foreground">Loading…</div>
          ) : current?.user ? (
            <div className="text-sm space-y-1">
              <div className="font-medium">{current.user.display_name}</div>
              <div className="text-muted-foreground">{current.user.email}</div>
              <div className="text-xs text-muted-foreground">
                id: <code>{current.user.id}</code>
              </div>
            </div>
          ) : (
            <div className="text-sm text-muted-foreground italic">
              No currentUser set yet for this mode.
            </div>
          )}

          <div className="border-t border-border pt-4 space-y-2">
            <Label>Switch to user</Label>
            <div className="flex gap-2">
              <Select value={picked} onValueChange={(v) => setPicked(v ?? "")}>
                <SelectTrigger className="w-full flex-1">
                  <SelectValue placeholder="Select user…" />
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
                {set.isPending ? "Switching…" : "Switch"}
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

      {match(mode)
        .with("mock-speakeasy", () => <MockSpeakeasyHints />)
        .with("oauth2-1", () => <DiscoveryPane prefix="/oauth2-1" />)
        .with("oauth2", () => <DiscoveryPane prefix="/oauth2" />)
        .exhaustive()}
    </div>
  );
}

function MockSpeakeasyHints() {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>Bootstrap</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        <p className="text-muted-foreground">
          If <code>currentUser</code> is unset, hit the login URL once to
          trigger the default-user bootstrap from your git committer:
        </p>
        <pre className="text-xs bg-muted rounded-2xl p-3 overflow-x-auto">
          <code>{`/mock-speakeasy/v1/speakeasy_provider/login?return_url=http://x`}</code>
        </pre>
      </CardContent>
    </Card>
  );
}
