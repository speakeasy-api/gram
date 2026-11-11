import { useState, type JSX } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { GcpIamCredential } from "@gram/admin-client/models/components/gcpiamcredential";

import { Button } from "@/components/ui/button";
import { DialogFooter } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

import {
  EMPTY_FORM,
  GCP_AUTH_METHODS,
  formFromCredential,
  formProblem,
  requestFromForm,
  type CredentialFormState,
  type GcpAuthMethod,
} from "./credentialForm";
import {
  createGcpIamPlatformCredential,
  invalidatePlatformCredentialQueries,
  updateGcpIamPlatformCredential,
} from "./platformCredentialsClient";

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <label className="grid gap-1 text-sm">
      <span className="font-medium">{label}</span>
      {children}
      {hint && <span className="text-muted-foreground text-xs">{hint}</span>}
    </label>
  );
}

// Creates a credential when `credential` is absent, otherwise replaces it.
export function CredentialEditor({
  credential,
  onSaved,
  onCancel,
  onPendingChange,
}: {
  credential?: GcpIamCredential;
  onSaved: (saved: GcpIamCredential) => void | Promise<void>;
  onCancel: () => void;
  onPendingChange?: (pending: boolean) => void;
}): JSX.Element {
  const cache = useQueryClient();
  const [form, setForm] = useState<CredentialFormState>(() =>
    credential ? formFromCredential(credential) : EMPTY_FORM,
  );
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const problem = formProblem(form);
  const method = GCP_AUTH_METHODS.find((m) => m.value === form.method);

  const set = (patch: Partial<CredentialFormState>) =>
    setForm((current) => ({ ...current, ...patch }));

  const submit = async () => {
    if (pending || problem) return;
    setPending(true);
    onPendingChange?.(true);
    setError("");
    try {
      const request = requestFromForm(form);
      const saved = credential
        ? await updateGcpIamPlatformCredential({
            ...request,
            id: credential.id,
          })
        : await createGcpIamPlatformCredential(request);
      await invalidatePlatformCredentialQueries(cache);
      toast.success(credential ? "Credential saved" : "Credential created");
      await onSaved(saved);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Request failed");
    } finally {
      setPending(false);
      onPendingChange?.(false);
    }
  };

  return (
    <form
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <Field label="Name">
        <Input
          required
          value={form.name}
          onChange={(event) => set({ name: event.target.value })}
        />
      </Field>
      <Field label="Authentication" hint={method?.description}>
        <Select
          value={form.method}
          onValueChange={(value) => set({ method: value as GcpAuthMethod })}
        >
          <SelectTrigger aria-label="Authentication">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {GCP_AUTH_METHODS.map((m) => (
              <SelectItem key={m.value} value={m.value}>
                {m.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      {form.method === "wif" && (
        <>
          <Field label="WIF pool ID">
            <Input
              className="font-mono"
              value={form.wifPoolId}
              onChange={(event) => set({ wifPoolId: event.target.value })}
            />
          </Field>
          <Field label="WIF provider ID">
            <Input
              className="font-mono"
              value={form.wifProviderId}
              onChange={(event) => set({ wifProviderId: event.target.value })}
            />
          </Field>
          <Field label="GCP project number">
            <Input
              className="font-mono"
              inputMode="numeric"
              value={form.wifProjectNumber}
              onChange={(event) =>
                set({ wifProjectNumber: event.target.value })
              }
            />
          </Field>
        </>
      )}
      {form.method !== "ambient" && (
        <Field
          label={
            form.method === "wif"
              ? "Service account to impersonate (optional)"
              : "Service account to impersonate"
          }
          hint="For example signer@project-id.iam.gserviceaccount.com."
        >
          <Input
            className="font-mono"
            value={form.impersonateServiceAccount}
            onChange={(event) =>
              set({ impersonateServiceAccount: event.target.value })
            }
          />
        </Field>
      )}
      {credential && (
        <p className="text-muted-foreground text-sm">
          Saving replaces the whole configuration. The server refuses the change
          while managed signing keys still sign through this credential, and
          refuses an identity change while organization encryption keys or
          issuer keys still use it.
        </p>
      )}
      {problem && !error && (
        <p className="text-muted-foreground text-sm">{problem}</p>
      )}
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          disabled={pending}
          onClick={onCancel}
        >
          Cancel
        </Button>
        <Button type="submit" disabled={pending || problem !== undefined}>
          {pending
            ? "Saving…"
            : credential
              ? "Save credential"
              : "Create credential"}
        </Button>
      </DialogFooter>
    </form>
  );
}
