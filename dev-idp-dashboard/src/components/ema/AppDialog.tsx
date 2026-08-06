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
import type { EmaApp } from "@/lib/devidp";
import {
  useCreateEmaApp,
  useDeleteEmaApp,
  useUpdateEmaApp,
} from "@/hooks/use-ema";

/**
 * Create or edit a requesting app. Which credential is filled in decides how
 * the app authenticates, so the fields say so rather than leaving an operator
 * to infer it from the server's behaviour.
 */
export function AppDialog({
  app,
  onClose,
}: {
  app?: EmaApp;
  onClose: () => void;
}) {
  const editing = app !== undefined;
  const [clientID, setClientID] = useState(app?.client_id ?? "");
  const [name, setName] = useState(app?.name ?? "");
  const [clientSecret, setClientSecret] = useState(app?.client_secret ?? "");
  const [jwks, setJwks] = useState(app?.jwks ?? "");

  const create = useCreateEmaApp();
  const update = useUpdateEmaApp();
  const remove = useDeleteEmaApp();
  const pending = create.isPending || update.isPending || remove.isPending;
  const error = create.error ?? update.error ?? remove.error;

  const submit = () => {
    if (editing) {
      update.mutate(
        {
          id: app.id,
          client_id: clientID,
          name,
          client_secret: clientSecret,
          jwks,
        },
        { onSuccess: onClose },
      );
    } else {
      create.mutate(
        {
          client_id: clientID,
          name: name || undefined,
          client_secret: clientSecret || undefined,
          jwks: jwks || undefined,
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
            <DialogTitle>{editing ? "Edit app" : "Register app"}</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="app-client-id">Client ID</Label>
              <Input
                id="app-client-id"
                value={clientID}
                onChange={(e) => setClientID(e.target.value)}
                placeholder="my-mcp-client"
                autoFocus
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="app-name">Name</Label>
              <Input
                id="app-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="defaults to the client id"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="app-secret">Client secret</Label>
              <Input
                id="app-secret"
                value={clientSecret}
                onChange={(e) => setClientSecret(e.target.value)}
                placeholder="blank = public client"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="app-jwks">JWKS</Label>
              <Input
                id="app-jwks"
                value={jwks}
                onChange={(e) => setJwks(e.target.value)}
                placeholder='{"keys":[…]}'
              />
              <p className="text-xs text-muted-foreground">
                Set this and the app must authenticate with private_key_jwt; its
                secret stops working.
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
                  remove.mutate({ id: app.id }, { onSuccess: onClose })
                }
              >
                Delete
              </Button>
            ) : (
              <span />
            )}
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={pending || !clientID.trim()}>
                {editing ? "Save" : "Register"}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
