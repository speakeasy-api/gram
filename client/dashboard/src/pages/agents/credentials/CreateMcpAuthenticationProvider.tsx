import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { DEFAULT_USER_SESSION_DURATION_HOURS } from "@/lib/externalMcpUserSessions";
import { useRef, useState, type JSX } from "react";
import { TextField } from "./SetupFields";

/**
 * Creates an MCP authentication provider that can give tokens to agents, in
 * the agent's project, for an organization that has none yet. It starts with
 * no servers: each is assigned to it afterwards, where the agent's servers
 * are listed.
 */
export function CreateMcpAuthenticationProvider({
  onCreated,
}: {
  /** Selects the new provider once it can be found. */
  onCreated: (id: string) => Promise<void>;
}): JSX.Element {
  const project = useProject();
  const sdk = useSdkClient();
  const { hasScope } = useRBAC();
  const canCreate = hasScope("project:write", project.id);
  const [slug, setSlug] = useState("agents");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // If creating succeeds but finding it fails, a retry finds it again rather
  // than creating a second one.
  const createdID = useRef<string | undefined>(undefined);

  const create = async () => {
    if (busy || !canCreate || !project.slug || !slug.trim()) return;
    setBusy(true);
    setError(null);
    try {
      if (!createdID.current) {
        const provider = await sdk.userSessionIssuers.create({
          gramProject: project.slug,
          createUserSessionIssuerForm: {
            slug: slug.trim(),
            authnChallengeMode: "interactive",
            sessionDurationHours: DEFAULT_USER_SESSION_DURATION_HOURS,
            authorizationServerMode: "shared",
          },
        });
        createdID.current = provider.id;
      }
      await onCreated(createdID.current);
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "Couldn't create the MCP authentication provider. Try again.",
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <Stack gap={3} className="border-border border p-4">
      <Stack gap={1}>
        <Text>
          No MCP authentication provider can give tokens to agents yet
        </Text>
        <Text muted small>
          Create one in {project.name}, then assign it to this agent&apos;s
          servers below.
        </Text>
      </Stack>
      {canCreate ? (
        <>
          <fieldset disabled={busy || !!createdID.current}>
            <TextField
              id="mcp-authentication-provider-slug"
              label="Name"
              value={slug}
              onChange={setSlug}
              placeholder="agents"
              help="A unique name within this project."
            />
          </fieldset>
          <div>
            <Button
              variant="secondary"
              disabled={!project.slug || !slug.trim() || busy}
              onClick={() => void create()}
            >
              <Button.Text>
                {busy
                  ? "Creating…"
                  : createdID.current
                    ? "Retry finding it"
                    : "Create MCP authentication provider"}
              </Button.Text>
            </Button>
          </div>
        </>
      ) : (
        <Text muted small>
          Creating one needs permission to edit {project.name}. Ask a project
          administrator.
        </Text>
      )}
      {error && (
        <Text small role="alert" className="text-destructive block">
          {error}
        </Text>
      )}
    </Stack>
  );
}
