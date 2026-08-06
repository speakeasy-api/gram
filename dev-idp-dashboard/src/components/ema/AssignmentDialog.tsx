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
import type { EmaApp, EmaAppAssignment, EmaResource, User } from "@/lib/devidp";
import {
  useCreateEmaAssignment,
  useDeleteEmaAssignment,
  useUpdateEmaAssignment,
} from "@/hooks/use-ema";

/**
 * Create or edit an assignment — the route drawn on the canvas.
 *
 * Editing only offers scopes: the (app, user, resource) triple is the
 * identity of the row, so changing it means a different assignment. Delete
 * and re-assign, which is also what revoking access actually is.
 */
export function AssignmentDialog({
  assignment,
  apps,
  users,
  resources,
  onClose,
}: {
  assignment?: EmaAppAssignment;
  apps: EmaApp[];
  users: User[];
  resources: EmaResource[];
  onClose: () => void;
}) {
  const editing = assignment !== undefined;
  const [appID, setAppID] = useState(assignment?.app_id ?? "");
  const [userID, setUserID] = useState(assignment?.user_id ?? "");
  const [resourceID, setResourceID] = useState(assignment?.resource_id ?? "");
  const [scopes, setScopes] = useState(assignment?.granted_scopes ?? "");

  const create = useCreateEmaAssignment();
  const update = useUpdateEmaAssignment();
  const remove = useDeleteEmaAssignment();
  const error = create.error ?? update.error ?? remove.error;
  const pending = create.isPending || update.isPending || remove.isPending;

  const submit = () => {
    if (editing) {
      update.mutate(
        { id: assignment.id, granted_scopes: scopes },
        { onSuccess: onClose },
      );
    } else {
      create.mutate(
        {
          app_id: appID,
          user_id: userID,
          resource_id: resourceID,
          granted_scopes: scopes || undefined,
        },
        { onSuccess: onClose },
      );
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {editing ? "Edit assignment" : "Assign an app"}
            </DialogTitle>
          </DialogHeader>

          <div className="flex flex-col gap-3">
            <Picker
              id="assign-app"
              label="App"
              value={appID}
              onChange={setAppID}
              disabled={editing}
              options={apps.map((a) => ({ value: a.id, label: a.client_id }))}
            />
            <Picker
              id="assign-user"
              label="User"
              value={userID}
              onChange={setUserID}
              disabled={editing}
              options={users.map((u) => ({ value: u.id, label: u.email }))}
            />
            <Picker
              id="assign-resource"
              label="Resource"
              value={resourceID}
              onChange={setResourceID}
              disabled={editing}
              options={resources.map((r) => ({ value: r.id, label: r.slug }))}
            />
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="assign-scopes">Granted scopes</Label>
              <Input
                id="assign-scopes"
                value={scopes}
                onChange={(e) => setScopes(e.target.value)}
                placeholder="chat.read chat.history"
              />
              <p className="text-xs text-muted-foreground">
                A mint request is narrowed to these. Asking only for scopes that
                are not granted is refused outright.
              </p>
            </div>
            {error && (
              <div className="text-xs text-destructive">
                {(error as Error).message}
              </div>
            )}
          </div>

          <DialogFooter className="justify-between">
            {editing ? (
              <Button
                type="button"
                variant="destructive"
                onClick={() =>
                  remove.mutate({ id: assignment.id }, { onSuccess: onClose })
                }
              >
                Revoke
              </Button>
            ) : (
              <span />
            )}
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={
                  pending || (!editing && (!appID || !userID || !resourceID))
                }
              >
                {editing ? "Save" : "Assign"}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Picker({
  id,
  label,
  value,
  onChange,
  options,
  disabled,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: Array<{ value: string; label: string }>;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <select
        id={id}
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className="h-9 rounded-md border border-border bg-background px-2 text-sm disabled:opacity-60"
      >
        <option value="">Select…</option>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </div>
  );
}
