import { useState, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  errorMessage,
  gramAdminFetch,
  gramAdminMutation,
} from "@/lib/gramAdminApi";

type SelfServeStatus = {
  disabled: boolean;
  updated_by?: string;
  updated_at?: string;
};
const statusKey = ["admin", "self-serve"];

export function SelfServePage(): JSX.Element {
  const client = useQueryClient();
  const status = useQuery({
    queryKey: statusKey,
    queryFn: () => gramAdminFetch<SelfServeStatus>("/admin/selfServe"),
  });
  const [confirming, setConfirming] = useState(false);
  const mutation = useMutation({
    mutationFn: (disabled: boolean) =>
      gramAdminMutation<SelfServeStatus>("/admin/selfServe.set", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ disabled }),
      }),
    onSuccess: (next) => {
      setConfirming(false);
      client.setQueryData(statusKey, next);
    },
  });
  const disabled = status.data?.disabled ?? false;

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold">Self-serve signup</h1>
        <p className="text-muted-foreground">
          Turns self-serve off across all organizations. While off, signup,
          assistants auto-provisioning and Platform MCP workspace creation
          refuse to create organizations, and no self-serve trial is armed on
          login. Existing organizations and staff-created organizations are
          unaffected.
        </p>
      </div>
      {status.isPending && <p>Loading status…</p>}
      {status.isError && (
        <div role="alert">
          {errorMessage(status.error)}{" "}
          <Button onClick={() => void status.refetch()}>Retry</Button>
        </div>
      )}
      {mutation.isError && !confirming && (
        <p role="alert">{errorMessage(mutation.error)}</p>
      )}
      {status.data && (
        <div className="space-y-4 rounded-lg border p-4">
          <p className="text-lg font-medium" data-testid="self-serve-state">
            Self-serve is {disabled ? "off" : "on"}
          </p>
          {status.data.updated_by && status.data.updated_at && (
            <p className="text-sm text-muted-foreground">
              Last changed by {status.data.updated_by} at{" "}
              {new Date(status.data.updated_at).toLocaleString()}
            </p>
          )}
          <Button
            variant={disabled ? "default" : "destructive"}
            disabled={mutation.isPending}
            onClick={() => {
              mutation.reset();
              setConfirming(true);
            }}
          >
            {disabled ? "Turn self-serve on" : "Turn self-serve off"}
          </Button>
        </div>
      )}
      <Dialog open={confirming} onOpenChange={setConfirming}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {disabled ? "Turn self-serve on?" : "Turn self-serve off?"}
            </DialogTitle>
            <DialogDescription>
              {disabled
                ? "New users can create organizations and start trials again."
                : "New users across all organizations will be refused when they try to create an organization or start a trial. This takes effect immediately."}
            </DialogDescription>
          </DialogHeader>
          {mutation.isError && (
            <p role="alert">{errorMessage(mutation.error)}</p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirming(false)}>
              Cancel
            </Button>
            <Button
              variant={disabled ? "default" : "destructive"}
              disabled={mutation.isPending}
              onClick={() => mutation.mutate(!disabled)}
            >
              {disabled ? "Turn on" : "Turn off"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
