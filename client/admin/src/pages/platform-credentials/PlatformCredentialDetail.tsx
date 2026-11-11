import { useState, type JSX } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { toast } from "sonner";
import type { GcpIamCredential } from "@gram/admin-client/models/components/gcpiamcredential";
import type { VerifyPlatformCredentialResult } from "@gram/admin-client/models/components/verifyplatformcredentialresult";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { CopyValue } from "@/components/CopyValue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

import { CredentialEditor } from "./CredentialEditor";
import { authMethodLabel, authMethodOf } from "./credentialForm";
import {
  deleteGcpIamPlatformCredential,
  gcpIamPlatformCredentialQuery,
  invalidatePlatformCredentialQueries,
  verifyGcpIamPlatformCredential,
} from "./platformCredentialsClient";

const IDENTITY_SOURCE_LABELS: Record<
  NonNullable<VerifyPlatformCredentialResult["identitySource"]>,
  string
> = {
  metadata_server: "Attached identity (metadata server)",
  application_default_credentials: "Application Default Credentials",
  impersonation: "Impersonated service account",
};

function Row({
  label,
  value,
}: {
  label: string;
  value: string | undefined;
}): JSX.Element {
  return (
    <div className="grid grid-cols-[12rem_1fr] items-center gap-4 py-2 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 items-center gap-2">
        {value ? (
          <>
            <span className="truncate font-mono">{value}</span>
            <CopyValue label={label} value={value} />
          </>
        ) : (
          <span className="text-muted-foreground">Not set</span>
        )}
      </dd>
    </div>
  );
}

function Configuration({
  credential,
}: {
  credential: GcpIamCredential;
}): JSX.Element {
  const method = authMethodOf(credential);
  return (
    <section className="grid gap-2">
      <h2 className="text-base font-semibold">Configuration</h2>
      <dl className="divide-y">
        <div className="grid grid-cols-[12rem_1fr] items-center gap-4 py-2 text-sm">
          <dt className="text-muted-foreground">Authentication</dt>
          <dd>
            <Badge variant="outline">{authMethodLabel(method)}</Badge>
          </dd>
        </div>
        {method === "wif" && (
          <>
            <Row label="WIF pool ID" value={credential.wifPoolId} />
            <Row label="WIF provider ID" value={credential.wifProviderId} />
            <Row
              label="GCP project number"
              value={credential.wifProjectNumber}
            />
          </>
        )}
        {method !== "ambient" && (
          <Row
            label="Impersonated service account"
            value={credential.impersonateServiceAccount}
          />
        )}
        <Row label="Credential ID" value={credential.id} />
        <div className="grid grid-cols-[12rem_1fr] gap-4 py-2 text-sm">
          <dt className="text-muted-foreground">Created</dt>
          <dd>{credential.createdAt.toLocaleString()}</dd>
        </div>
        <div className="grid grid-cols-[12rem_1fr] gap-4 py-2 text-sm">
          <dt className="text-muted-foreground">Updated</dt>
          <dd>{credential.updatedAt.toLocaleString()}</dd>
        </div>
      </dl>
    </section>
  );
}

function Verification({ id }: { id: string }): JSX.Element {
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<VerifyPlatformCredentialResult>();
  const [error, setError] = useState("");

  const verify = async () => {
    setPending(true);
    setError("");
    try {
      setResult(await verifyGcpIamPlatformCredential(id));
    } catch (e) {
      setResult(undefined);
      setError(e instanceof Error ? e.message : "Request failed");
    } finally {
      setPending(false);
    }
  };

  return (
    <section className="grid gap-3">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold">Verify</h2>
          <p className="text-muted-foreground text-sm">
            Resolves the identity live and reports the principal it acts as.
            Nothing is saved.
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={pending}
          onClick={() => void verify()}
        >
          {pending ? "Verifying…" : "Verify identity"}
        </Button>
      </div>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      {result && (
        <div role="status" className="grid gap-1 rounded-md border p-3 text-sm">
          <div>
            <Badge variant={result.verified ? "default" : "destructive"}>
              {result.verified ? "Verified" : "Not verified"}
            </Badge>
          </div>
          {result.principal && (
            <p>
              Acts as <span className="font-mono">{result.principal}</span>
            </p>
          )}
          {result.identitySource && (
            <p className="text-muted-foreground">
              {IDENTITY_SOURCE_LABELS[result.identitySource]}
            </p>
          )}
          {result.detail && (
            <p className="text-muted-foreground">{result.detail}</p>
          )}
        </div>
      )}
    </section>
  );
}

export function PlatformCredentialDetail(): JSX.Element {
  const { credentialId } = useParams({
    from: "/platform-credentials/$credentialId",
  });
  const navigate = useNavigate();
  const cache = useQueryClient();
  const query = useQuery(gcpIamPlatformCredentialQuery(credentialId));
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [confirm, confirmDialog] = useConfirmDialog();

  const remove = async (credential: GcpIamCredential) => {
    const confirmed = await confirm({
      title: `Delete ${credential.name}?`,
      description:
        "Anything that signs through this credential stops working. The server refuses while managed signing keys, organization encryption keys or live issuer keys still use it.",
      confirmLabel: "Delete credential",
      destructive: true,
    });
    if (!confirmed) return;

    setDeleting(true);
    setDeleteError("");
    try {
      await deleteGcpIamPlatformCredential(credential.id);
      await invalidatePlatformCredentialQueries(cache, credential.id);
      toast.success("Credential deleted");
      await navigate({ to: "/platform-credentials" });
    } catch (e) {
      setDeleteError(e instanceof Error ? e.message : "Request failed");
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="grid max-w-3xl gap-8">
      {query.isPending && <p role="status">Loading credential…</p>}
      {query.error && (
        <div role="alert">
          {query.error.message}
          <Button variant="ghost" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </div>
      )}
      {query.data && (
        <>
          <header className="flex items-start justify-between gap-4">
            <div>
              <h1 className="text-xl font-semibold">{query.data.name}</h1>
              <p className="text-muted-foreground text-sm">
                Platform GCP IAM credential
              </p>
            </div>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={deleting}
                onClick={() => setEditing(true)}
              >
                Edit
              </Button>
              <Button
                variant="destructive"
                size="sm"
                disabled={deleting}
                onClick={() => void remove(query.data)}
              >
                {deleting ? "Deleting…" : "Delete"}
              </Button>
            </div>
          </header>
          {deleteError && (
            <p role="alert" className="text-destructive text-sm">
              {deleteError}
            </p>
          )}
          <Configuration credential={query.data} />
          {/* Keyed on the save time so an edit clears a result that described
              the identity it replaced. */}
          <Verification
            key={query.data.updatedAt.getTime()}
            id={query.data.id}
          />
          <Dialog
            open={editing}
            onOpenChange={(open) => {
              if (!saving) setEditing(open);
            }}
          >
            <DialogContent onInteractOutside={(e) => e.preventDefault()}>
              <DialogHeader>
                <DialogTitle>Edit {query.data.name}</DialogTitle>
                <DialogDescription>
                  Replace this credential's name and configuration.
                </DialogDescription>
              </DialogHeader>
              {editing && (
                <CredentialEditor
                  credential={query.data}
                  onPendingChange={setSaving}
                  onCancel={() => setEditing(false)}
                  onSaved={() => setEditing(false)}
                />
              )}
            </DialogContent>
          </Dialog>
        </>
      )}
      {confirmDialog}
    </div>
  );
}
