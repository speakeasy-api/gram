import { useRef, useState, type JSX } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { GlobalRemoteSessionIssuer } from "@gram/admin-client/models/components/globalremotesessionissuer";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  adminDeleteGlobalIssuer,
  adminRefreshGlobalIssuerMetadata,
} from "@/lib/gramAdminClient";
import { invalidateIssuerQueries } from "./issuerQueries";
export function IssuerActions({
  record,
  showRefresh = true,
  disabled = false,
  onDeleted,
}: {
  record: GlobalRemoteSessionIssuer;
  showRefresh?: boolean;
  disabled?: boolean;
  onDeleted?: () => Promise<void>;
}): JSX.Element {
  const cache = useQueryClient();
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const busy = useRef(false);
  const [error, setError] = useState("");
  const [refreshWarnings, setRefreshWarnings] = useState<string[]>([]);
  const run = async (remove: boolean) => {
    if (busy.current || disabled) return;
    busy.current = true;
    setPending(true);
    setError("");
    try {
      if (remove) {
        await adminDeleteGlobalIssuer({ id: record.issuer.id });
        await invalidateIssuerQueries(cache, record.issuer.id);
        toast.success("Issuer deleted");
        setOpen(false);
        await onDeleted?.();
      } else {
        const result = await adminRefreshGlobalIssuerMetadata({
          id: record.issuer.id,
        });
        setRefreshWarnings(result.discoveryWarnings);
        await invalidateIssuerQueries(cache);
        toast.success("Issuer metadata refreshed");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Request failed");
    } finally {
      busy.current = false;
      setPending(false);
    }
  };
  return (
    <div className="grid gap-2">
      <div className="flex flex-wrap gap-2">
        {showRefresh && (
          <Button
            variant="outline"
            size="sm"
            disabled={pending || disabled}
            onClick={() => void run(false)}
          >
            Refresh metadata
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          disabled={pending || disabled}
          onClick={() => {
            setError("");
            setOpen(true);
          }}
        >
          Delete issuer
        </Button>
      </div>
      {!open && error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      {refreshWarnings.map((warning, i) => (
        <p key={i} className="text-muted-foreground text-sm">
          {warning}
        </p>
      ))}
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!pending) setOpen(value);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              Delete {record.issuer.name || record.issuer.issuer}?
            </DialogTitle>
            <DialogDescription>
              {record.globalClientCount} platform clients and{" "}
              {record.tenantClientCount} tenant-owned clients currently
              reference this issuer. Tenant clients can only be removed by their
              owning organizations. Counts are advisory; the server refuses
              deletion while dependencies exist.
            </DialogDescription>
          </DialogHeader>
          {error && (
            <p role="alert" className="text-destructive text-sm">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button
              variant="ghost"
              disabled={pending || disabled}
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={pending || disabled}
              onClick={() => void run(true)}
            >
              {pending ? "Deleting…" : "Delete issuer"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
