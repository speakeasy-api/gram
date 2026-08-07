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
import type { EmaResource, EmaTrustRule } from "@/lib/devidp";
import {
  useCreateEmaResource,
  useCreateEmaTrustRule,
  useDeleteEmaResource,
  useDeleteEmaTrustRule,
  useUpdateEmaResource,
} from "@/hooks/use-ema";

/**
 * Create or edit a resource, and manage the trust rules attached to it.
 *
 * Trust rules live here rather than on their own page because they are a
 * per-resource property: which issuers this one resource accepts ID-JAGs
 * from, and the scope ceiling it applies. That is the redeem-side half of
 * the policy, and it belongs next to the thing it governs.
 */
export function ResourceDialog({
  resource,
  trustRules,
  localIssuer,
  onClose,
}: {
  resource?: EmaResource;
  trustRules: EmaTrustRule[];
  /** This dev-idp's own oauth2-1 issuer, offered as the obvious default. */
  localIssuer: string;
  onClose: () => void;
}) {
  const editing = resource !== undefined;
  const [slug, setSlug] = useState(resource?.slug ?? "");
  const [name, setName] = useState(resource?.name ?? "");
  const [identifier, setIdentifier] = useState(
    resource?.resource_identifier ?? "",
  );

  const create = useCreateEmaResource();
  const update = useUpdateEmaResource();
  const remove = useDeleteEmaResource();
  const error = create.error ?? update.error ?? remove.error;
  const pending = create.isPending || update.isPending || remove.isPending;

  const mine = trustRules.filter((r) => r.resource_id === resource?.id);

  const submit = () => {
    if (editing) {
      update.mutate(
        { id: resource.id, slug, name, resource_identifier: identifier },
        { onSuccess: onClose },
      );
    } else {
      create.mutate(
        {
          slug,
          name: name || undefined,
          resource_identifier: identifier,
        },
        { onSuccess: onClose },
      );
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <div className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>
              {editing ? "Edit resource" : "Register resource"}
            </DialogTitle>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="res-slug">Slug</Label>
              <Input
                id="res-slug"
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                placeholder="chat"
                autoFocus
                required
              />
              {editing && (
                <p className="text-xs text-muted-foreground break-all">
                  Issuer (what an ID-JAG carries in <code>aud</code>):{" "}
                  {resource.issuer}
                </p>
              )}
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="res-name">Name</Label>
              <Input
                id="res-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="defaults to the slug"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="res-identifier">Resource identifier</Label>
              <Input
                id="res-identifier"
                value={identifier}
                onChange={(e) => setIdentifier(e.target.value)}
                placeholder="https://mcp.chat.example/"
                required
              />
              <p className="text-xs text-muted-foreground">
                The MCP server behind this authorization server. Lands in the
                token's <code>aud</code> — a different URL from the issuer
                above.
              </p>
            </div>
            {error && (
              <div className="text-xs text-destructive">
                {(error as Error).message}
              </div>
            )}
            <DialogFooter className="justify-between">
              {editing ? (
                <Button
                  type="button"
                  variant="destructive"
                  onClick={() =>
                    remove.mutate({ id: resource.id }, { onSuccess: onClose })
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
                <Button
                  type="submit"
                  disabled={pending || !slug.trim() || !identifier.trim()}
                >
                  {editing ? "Save" : "Register"}
                </Button>
              </div>
            </DialogFooter>
          </form>

          {editing && (
            <TrustRules
              resourceId={resource.id}
              rules={mine}
              localIssuer={localIssuer}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function TrustRules({
  resourceId,
  rules,
  localIssuer,
}: {
  resourceId: string;
  rules: EmaTrustRule[];
  localIssuer: string;
}) {
  const [issuer, setIssuer] = useState("");
  const [ceiling, setCeiling] = useState("");
  const create = useCreateEmaTrustRule();
  const remove = useDeleteEmaTrustRule();

  return (
    <section className="flex flex-col gap-2 border-t border-border pt-4">
      <div className="flex flex-col gap-1">
        <h3 className="text-sm font-medium">Trusts</h3>
        <p className="text-xs text-muted-foreground">
          Which issuers this resource accepts ID-JAGs from. Enforced separately
          from assignments, so a grant this dev-idp minted can still be refused
          here. An issuer need not be this dev-idp.
        </p>
      </div>

      <div className="flex flex-col gap-1.5">
        {rules.length === 0 && (
          <p className="text-xs text-muted-foreground italic">
            Nothing trusted — every redemption is refused.
          </p>
        )}
        {rules.map((r) => (
          <div
            key={r.id}
            className="flex items-center justify-between gap-2 rounded-md border border-border px-2 py-1"
          >
            <div className="min-w-0 font-mono text-xs">
              <div className="truncate">{r.trusted_issuer}</div>
              <div className="text-muted-foreground">
                {r.allowed_scopes ? `≤ ${r.allowed_scopes}` : "no ceiling"}
                {r.enabled ? "" : " · disabled"}
              </div>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="xs"
              onClick={() => remove.mutate({ id: r.id })}
            >
              Remove
            </Button>
          </div>
        ))}
      </div>

      <div className="flex items-end gap-2">
        <div className="flex flex-1 flex-col gap-1.5">
          <Label htmlFor="trust-issuer">Issuer</Label>
          <Input
            id="trust-issuer"
            value={issuer}
            onChange={(e) => setIssuer(e.target.value)}
            placeholder={localIssuer}
          />
        </div>
        <div className="flex w-36 flex-col gap-1.5">
          <Label htmlFor="trust-ceiling">Ceiling</Label>
          <Input
            id="trust-ceiling"
            value={ceiling}
            onChange={(e) => setCeiling(e.target.value)}
            placeholder="none"
          />
        </div>
        <Button
          type="button"
          variant="outline"
          disabled={create.isPending}
          onClick={() =>
            create.mutate(
              {
                resource_id: resourceId,
                trusted_issuer: issuer.trim() || localIssuer,
                allowed_scopes: ceiling.trim() || undefined,
              },
              {
                onSuccess: () => {
                  setIssuer("");
                  setCeiling("");
                },
              },
            )
          }
        >
          Trust
        </Button>
      </div>
      {create.error && (
        <div className="text-xs text-destructive">
          {(create.error as Error).message}
        </div>
      )}
    </section>
  );
}
