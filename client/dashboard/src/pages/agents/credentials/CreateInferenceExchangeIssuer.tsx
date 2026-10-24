import { useRef, useState } from "react";
import { Button } from "@/components/ui/Button";
import { useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { TextField } from "./SetupFields";

/** Creates only a Speakeasy authorization server, not an external issuer or MCP server. */
export function CreateInferenceExchangeIssuer({
  onCreated,
  onBusy,
}: {
  onCreated: (id: string) => Promise<void>;
  onBusy: (busy: boolean) => void;
}): JSX.Element {
  const project = useProject();
  const sdk = useSdkClient();
  const { hasScope } = useRBAC();
  const canCreate = hasScope("project:write", project.id);
  const [slug, setSlug] = useState("inference");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [complete, setComplete] = useState(false);
  // If creation succeeds but discovery fails, retry discovery, not the write.
  const createdID = useRef<string | undefined>(undefined);
  const create = async () => {
    if (busy || !canCreate || !project.id || !project.slug || !slug.trim())
      return;
    setBusy(true);
    onBusy(true);
    setError(null);
    try {
      if (!createdID.current) {
        const issuer = await sdk.userSessionIssuers.create({
          gramProject: project.slug,
          createUserSessionIssuerForm: {
            slug: slug.trim(),
            authnChallengeMode: "chain",
            sessionDurationHours: 1,
            authorizationServerMode: "shared",
          },
        });
        createdID.current = issuer.id;
      }
      // Discovery is the only refresh needed here. Organization issuer queries
      // require org:read, which project-only administrators need not hold.
      await onCreated(createdID.current);
      setComplete(true);
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "Could not create the shared authorization server. Try again.",
      );
    } finally {
      setBusy(false);
      onBusy(false);
    }
  };
  if (complete)
    return (
      <p className="text-sm">
        Shared Speakeasy authorization server created and selected. No MCP
        server was created.
      </p>
    );
  return (
    <div className="space-y-3 border p-3">
      <h3 className="font-medium">
        Create a shared Speakeasy authorization server
      </h3>
      <p className="text-sm">
        Create one in {project.name} for workload token exchange, with a maximum
        session duration of one hour.
      </p>
      {!canCreate && (
        <p className="text-sm">
          Creating an exchange authorization server requires project:write for{" "}
          {project.name}. Ask a project administrator to create one, or use an
          existing project or organization server above. Workload trust still
          requires workload:write, and inference requires the owner’s explicit
          inference:execute permission.
        </p>
      )}
      <fieldset disabled={!canCreate || busy || !!createdID.current}>
        <TextField
          id="inference-exchange-slug"
          label="Speakeasy authorization server slug"
          value={slug}
          onChange={setSlug}
          placeholder="inference"
          help="A unique name within this project."
        />
      </fieldset>
      <Button
        variant="secondary"
        disabled={
          !canCreate || !project.id || !project.slug || !slug.trim() || busy
        }
        onClick={() => void create()}
      >
        {busy
          ? "Preparing exchange server…"
          : createdID.current
            ? "Retry loading exchange server"
            : "Create shared authorization server"}
      </Button>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
    </div>
  );
}
