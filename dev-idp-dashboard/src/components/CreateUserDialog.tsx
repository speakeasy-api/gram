import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { useCreateMembership, useCreateUser } from "@/hooks/use-devidp";
import type { Organization, User } from "@/lib/devidp";

export function CreateUserDialog({
  users: _users,
  orgs,
  onClose,
}: {
  users: User[];
  orgs: Organization[];
  onClose: () => void;
}) {
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [orgId, setOrgId] = useState<string>("");
  const createUser = useCreateUser();
  const createMembership = useCreateMembership();

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={async (e) => {
            e.preventDefault();
            const user = await createUser.mutateAsync({
              email,
              display_name: displayName,
            });
            if (orgId) {
              await createMembership.mutateAsync({
                user_id: user.id,
                organization_id: orgId,
              });
            }
            onClose();
          }}
        >
          <DialogHeader>
            <DialogTitle>Create user</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="user-email">Email</Label>
              <Input
                id="user-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoFocus
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="user-name">Display name</Label>
              <Input
                id="user-name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                required
              />
            </div>
            {orgs.length > 0 && (
              <div className="flex flex-col gap-1.5">
                <Label>Initial membership (optional)</Label>
                <Select value={orgId} onValueChange={(v) => setOrgId(v ?? "")}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="None">
                      {(value: string | null) => {
                        const o = value
                          ? orgs.find((o) => o.id === value)
                          : null;
                        return o ? o.name : value;
                      }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {orgs.map((o) => (
                      <SelectItem key={o.id} value={o.id}>
                        {o.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            {createUser.error && (
              <div className="text-xs text-destructive">
                {(createUser.error as Error).message}
              </div>
            )}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createUser.isPending || createMembership.isPending}
            >
              {createUser.isPending ? "Creating…" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
