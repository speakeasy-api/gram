import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Label } from "@/components/ui/Label";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useRegisterWorkloadIssuerMutation } from "@gram/client/react-query/registerWorkloadIssuer.js";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import type { WorkloadIdentityPolicy } from "@gram/client/models/components/workloadidentitypolicy.js";
import type { SetupContext, SetupStep } from "./setupInstructions";

/** Reuses the custom issuer form; registering trust never creates an agent or admission. */
export function WorkloadIssuerSelector({
  issuers,
  selectedId,
  loading,
  failed,
  onRetry,
  onSelect,
  onCreated,
  issuerStep,
}: {
  issuers: WorkloadIssuer[];
  selectedId: string;
  loading: boolean;
  failed: boolean;
  onRetry: () => void;
  onSelect: (issuer: WorkloadIssuer) => void;
  onCreated: (policy: WorkloadIdentityPolicy, issuer: WorkloadIssuer) => void;
  issuerStep: SetupStep;
}): JSX.Element {
  const project = useProject();
  const { hasScope } = useRBAC();
  const canCreate = hasScope("workload:write");
  const [open, setOpen] = useState(false);
  const [values, setValues] = useState<Record<string, string>>({});
  const [projectScoped, setProjectScoped] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const register = useRegisterWorkloadIssuerMutation();
  const context: SetupContext = {
    values,
    setValue: (key, value) =>
      setValues((current) => ({ ...current, [key]: value })),
    agentName: "",
    computed: {},
    issuerPicker: null,
    ruleConflict: null,
  };
  const create = async () => {
    if (
      busy ||
      !canCreate ||
      !issuerStep.complete?.(context) ||
      (projectScoped && (!project.id || !project.slug))
    )
      return;
    setBusy(true);
    setError(null);
    try {
      const policy = await register.mutateAsync({
        request: {
          ...(projectScoped ? { gramProject: project.slug } : {}),
          registerWorkloadIssuerForm: {
            name: values.name!.trim(),
            issuer: values.issuer!.trim(),
            jwksUri: values.jwks_uri!.trim(),
            description: `Federated from ${values.issuer!.trim()}`,
            projectScoped,
            tags: [],
          },
        },
      });
      const issuer = policy.issuers.find(
        (item) =>
          item.name === values.name!.trim() &&
          item.projectId === (projectScoped ? project.id : ""),
      );
      if (!issuer)
        throw new Error(
          "The issuer was registered but could not be selected. Reload the workload issuers before trying again.",
        );
      onCreated(policy, issuer);
      setOpen(false);
      setValues({});
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "Could not register the workload issuer.",
      );
    } finally {
      setBusy(false);
    }
  };
  if (loading) return <p role="status">Loading workload issuers…</p>;
  if (failed)
    return (
      <div role="alert">
        <p>Could not load workload issuers.</p>
        <Button variant="secondary" onClick={onRetry}>
          Retry workload issuers
        </Button>
      </div>
    );
  const selected = issuers.find((issuer) => issuer.id === selectedId);
  return (
    <div className="space-y-3">
      {issuers.length > 0 && (
        <>
          <Label htmlFor="workload-issuer-choice">Workload issuer</Label>
          <select
            id="workload-issuer-choice"
            className="border-input bg-background block h-9 w-full border px-2"
            value={selectedId}
            onChange={(event) => {
              const issuer = issuers.find(
                (item) => item.id === event.target.value,
              );
              if (issuer) onSelect(issuer);
            }}
          >
            <option value="" disabled>
              Select a workload issuer
            </option>
            {issuers.map((issuer) => (
              <option key={issuer.id} value={issuer.id}>
                {issuer.name} —{" "}
                {issuer.projectId ? "Current project" : "Organization"}
              </option>
            ))}
          </select>
        </>
      )}
      {selected && (
        <dl className="text-muted-foreground space-y-1 break-all text-sm">
          <dt>External JWT issuer</dt>
          <dd>{selected.issuer}</dd>
          <dt>Signing keys (JWKS)</dt>
          <dd>{selected.jwksUri}</dd>
        </dl>
      )}
      <p className="text-muted-foreground text-sm">
        Reuse issuer trust across agents. Assign this agent’s subject separately
        below; inference permission belongs to the agent, not the issuer.
      </p>
      <Button
        variant="secondary"
        disabled={!canCreate}
        onClick={() => {
          setValues({});
          setError(null);
          setProjectScoped(!!project.id);
          setOpen(true);
        }}
      >
        Create new workload issuer
      </Button>
      {!canCreate && (
        <p className="text-sm">
          Creating workload issuers requires workload:write.
        </p>
      )}
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!busy) setOpen(next);
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Create workload issuer</Dialog.Title>
            <Dialog.Description>
              Register an external JWT issuer and its signing keys once. No
              agent, subject assignment, or inference grant is created here.
            </Dialog.Description>
          </Dialog.Header>
          <fieldset disabled={busy} className="space-y-4">
            {issuerStep.body(context)}
            <Label htmlFor="workload-issuer-tier">Available to</Label>
            <select
              id="workload-issuer-tier"
              className="border-input bg-background block h-9 w-full border px-2"
              value={projectScoped ? "project" : "organization"}
              onChange={(event) =>
                setProjectScoped(event.target.value === "project")
              }
            >
              {project.id && <option value="project">Current project</option>}
              <option value="organization">Organization</option>
            </select>
          </fieldset>
          {error && (
            <p role="alert" className="text-destructive text-sm">
              {error}
            </p>
          )}
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={
                busy ||
                !canCreate ||
                !issuerStep.complete?.(context) ||
                (projectScoped && (!project.id || !project.slug))
              }
              onClick={() => void create()}
            >
              {busy ? "Creating issuer…" : "Create workload issuer"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}
