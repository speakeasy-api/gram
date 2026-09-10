import { SettingsSection } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type JSX } from "react";
import { toast } from "sonner";

import {
  agentPolicyGrantsFromDraft,
  agentPolicyViewFromGrants,
  diffAgentPolicyGrants,
  invalidateAgentPolicy,
  type AgentPolicyDraft,
} from "./agent-policy-grants";
import { AgentPolicyEditor } from "./AgentPolicyEditor";

export function AgentPolicySection({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const organization = useOrganization();
  const { user } = useSession();
  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Permissions</SettingsSection.Title>
        <SettingsSection.Description>
          The most this agent may ever be delegated. Adding a permission here
          grants nothing on its own — each API key is narrowed again at
          issuance, against the owner's live permissions and your own.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <AgentPolicyContent
          // A draft belongs to one organization, one agent, one signed-in
          // human, and one answer to "may this human write". Any of them
          // changing makes the pending edit meaningless, so it is dropped
          // rather than carried into a context it was not built for.
          key={`${organization.id}:${user.id}:${agent.id}:${agent.permissions.write}`}
          agent={agent}
          organizationId={organization.id}
          userId={user.id}
        />
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function AgentPolicyContent({
  agent,
  organizationId,
  userId,
}: {
  agent: ManagedAgent;
  organizationId: string;
  userId: string;
}) {
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const canWrite = agent.permissions.write;
  const [draft, setDraft] = useState<AgentPolicyDraft | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [refreshFailed, setRefreshFailed] = useState(false);

  const grants = useQuery({
    queryKey: ["agent-policy-grants", organizationId, agent.id],
    queryFn: ({ signal }) =>
      sdk.agents.listPolicyGrants({ agentId: agent.id }, undefined, { signal }),
    retry: false,
    throwOnError: false,
  });

  const view = agentPolicyViewFromGrants(grants.data ?? []);
  // The stored ceiling is the draft until the user edits it, so a background
  // refetch is not silently overwritten by a stale local copy.
  const current = draft ?? view.draft;
  const dirty = draft !== null;

  const save = async () => {
    if (!draft || saving) return;
    // Only the grants the editor could represent are diffed. Preserved ones are
    // never removed and never re-created.
    const { create, remove } = diffAgentPolicyGrants(
      view.editable,
      agentPolicyGrantsFromDraft(draft),
    );
    setSaving(true);
    setError(null);
    let writeError: unknown = null;
    try {
      // Removals first: a narrowed permission would otherwise collide with the
      // grant it replaces, which the server rejects as a duplicate.
      for (const grant of remove) {
        await sdk.agents.deletePolicyGrant({
          agentPolicyGrantIDForm: { agentId: agent.id, grantId: grant.id },
        });
      }
      for (const form of create) {
        await sdk.agents.createPolicyGrant({
          createAgentPolicyGrantForm: { agentId: agent.id, ...form },
        });
      }
    } catch (cause) {
      writeError = cause;
    }

    // Each grant is its own request, so after a failure part-way the cached
    // list is neither the draft nor what is stored. Nothing may be presented as
    // the stored ceiling until a read confirms it, so the editor stays locked
    // until this resolves.
    let refreshed;
    try {
      refreshed = await grants.refetch();
    } catch {
      refreshed = undefined;
    }
    setSaving(false);

    if (!refreshed || refreshed.isError || refreshed.data === undefined) {
      // Fail closed: an editable base built from the pre-save cache would
      // invite the user to save again on top of a ceiling that has moved.
      setRefreshFailed(true);
      setError(
        "Could not confirm this agent's stored permissions after saving, so some changes may not have been applied. Reload before editing again.",
      );
      return;
    }

    setDraft(null);
    void invalidateAgentPolicy(queryClient, organizationId, userId, agent.id);
    if (writeError) {
      setError(
        writeError instanceof Error && writeError.message
          ? `${writeError.message} Some changes may not have been applied — the list below is what is stored.`
          : "Could not update agent permissions. Some changes may not have been applied — the list below is what is stored.",
      );
      return;
    }
    toast.success("Agent permissions updated");
  };

  const reload = async () => {
    const refreshed = await grants.refetch();
    if (refreshed.isError || refreshed.data === undefined) return;
    setRefreshFailed(false);
    setDraft(null);
    setError(null);
  };

  if (grants.isLoading)
    return (
      <SettingsSection.Body>
        <Text muted>Loading permissions…</Text>
      </SettingsSection.Body>
    );

  if (grants.isError || refreshFailed)
    return (
      <SettingsSection.Body>
        <div role="alert">
          <Text>{error ?? "Could not load this agent's permissions."}</Text>
        </div>
        <Button
          variant="secondary"
          disabled={grants.isFetching}
          onClick={() => void reload()}
        >
          Reload permissions
        </Button>
      </SettingsSection.Body>
    );

  return (
    <>
      <SettingsSection.Body>
        {(grants.data ?? []).length === 0 && !dirty && (
          <Text muted small>
            This agent has no permissions, so any API key issued for it will not
            authorize anything. Add permissions to make it usable.
          </Text>
        )}
        <AgentPolicyEditor
          draft={current}
          onChange={setDraft}
          disabled={!canWrite || saving}
          lockedScopes={view.preservedScopes}
        />
        {view.preserved.length > 0 && (
          <Text muted small>
            {`${view.preserved.length} stored permission${view.preserved.length === 1 ? "" : "s"} (${view.preservedScopes.join(", ")}) use constraints this editor cannot show, so ${view.preserved.length === 1 ? "it is" : "they are"} left exactly as stored. Use the API to change ${view.preserved.length === 1 ? "it" : "them"}.`}
          </Text>
        )}
        {error && (
          <p role="alert" className="text-sm">
            {error}
          </p>
        )}
      </SettingsSection.Body>
      <SettingsSection.Footer>
        <Text muted small>
          {canWrite
            ? "Permission changes are recorded in the organization audit log."
            : "You do not have permission to change this agent's permissions."}
        </Text>
        <div className="flex gap-2">
          <Button
            variant="secondary"
            onClick={() => {
              setDraft(null);
              setError(null);
            }}
            disabled={!dirty || saving}
          >
            Discard changes
          </Button>
          <Button
            onClick={() => void save()}
            disabled={!canWrite || !dirty || saving}
          >
            {saving ? "Saving…" : "Save permissions"}
          </Button>
        </div>
      </SettingsSection.Footer>
    </>
  );
}
