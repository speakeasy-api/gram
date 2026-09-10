import { SettingsSection } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type JSX } from "react";
import { toast } from "sonner";

import {
  agentPolicyFingerprint,
  agentPolicyGrantsFromDraft,
  agentPolicyViewFromGrants,
  diffAgentPolicyGrants,
  invalidateAgentPolicy,
  type AgentPolicyDraft,
} from "./agent-policy-grants";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import { AgentPolicyEditor } from "./AgentPolicyEditor";

/** An in-progress edit, pinned to the ceiling it started from. */
interface PolicyDraft {
  value: AgentPolicyDraft;
  /** The editable grants as they stood when the first change was made. */
  base: AgentPolicyGrant[];
  /** Identity of the whole stored ceiling at that moment. */
  fingerprint: string;
}

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
  const [draft, setDraft] = useState<PolicyDraft | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [blocked, setBlocked] = useState(false);

  // A save spans several awaits. These survive them; `canWrite` and `draft`
  // captured in the closure do not.
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const canWriteRef = useRef(canWrite);
  canWriteRef.current = canWrite;

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
  const current = draft?.value ?? view.draft;
  const dirty = draft !== null;

  // The first edit pins the ceiling the draft was built from. Everything after
  // it is measured against that base, never against a version that arrived
  // while the user was still typing.
  const edit = (value: AgentPolicyDraft) => {
    setDraft((previous): PolicyDraft =>
      previous
        ? { ...previous, value }
        : {
            value,
            base: view.editable,
            fingerprint: agentPolicyFingerprint(grants.data ?? []),
          },
    );
  };

  const save = async () => {
    if (!draft || saving) return;
    // Taken before the first await, so a second click cannot start a parallel
    // save that writes the same diff twice.
    setSaving(true);
    setError(null);

    const stop = (message: string) => {
      setSaving(false);
      setBlocked(true);
      setError(message);
    };

    // The cache only learns of another administrator's change when something
    // happens to refetch it, which may be never while this draft is open. Read
    // the ceiling now and judge against that, not against whatever the cache
    // happens to hold.
    //
    // This narrows the window; it does not close it. There is no precondition
    // on `agents.createPolicyGrant` / `deletePolicyGrant`, so this is not a
    // compare-and-swap: a change landing between this read and the writes below
    // is still missed. Pinning the base does reliably stop a grant this draft
    // never saw from being deleted. Closing the window needs an API that takes
    // an expected version.
    let confirmed;
    try {
      confirmed = await grants.refetch();
    } catch {
      confirmed = undefined;
    }
    // The agent, organization, or signed-in user may have changed while that
    // read was in flight; this instance is gone and must not write.
    if (!mounted.current) return;
    if (!confirmed || confirmed.isError || confirmed.data === undefined) {
      stop(
        "Could not read this agent's current permissions, so nothing was saved. Reload and try again.",
      );
      return;
    }
    if (!canWriteRef.current) {
      stop(
        "Your permission to change this agent's permissions ended before the save began, so nothing was saved.",
      );
      return;
    }

    // Someone else changed the ceiling since this draft was started. Diffing
    // against the newer list would delete their grants, because a grant this
    // draft never saw looks exactly like one the user removed. Write nothing
    // and make them reload, so the other change survives intact.
    if (agentPolicyFingerprint(confirmed.data) !== draft.fingerprint) {
      stop(
        "Someone else changed this agent's permissions while you were editing. Nothing was saved, and their changes are intact. Reload and make your changes again.",
      );
      return;
    }

    // Only the grants the editor could represent are diffed, and only as they
    // stood when the draft began. Preserved ones are never removed and never
    // re-created.
    const { create, remove } = diffAgentPolicyGrants(
      draft.base,
      agentPolicyGrantsFromDraft(draft.value),
    );
    let writeError: unknown = null;
    try {
      // Removals first: a narrowed permission would otherwise collide with the
      // grant it replaces, which the server rejects as a duplicate.
      for (const grant of remove) {
        if (!mounted.current || !canWriteRef.current) return;
        await sdk.agents.deletePolicyGrant({
          agentPolicyGrantIDForm: { agentId: agent.id, grantId: grant.id },
        });
      }
      for (const form of create) {
        if (!mounted.current || !canWriteRef.current) return;
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
    if (!mounted.current) return;
    setSaving(false);

    if (!refreshed || refreshed.isError || refreshed.data === undefined) {
      // Fail closed: an editable base built from the pre-save cache would
      // invite the user to save again on top of a ceiling that has moved.
      setBlocked(true);
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
    setBlocked(false);
    setDraft(null);
    setError(null);
  };

  if (grants.isLoading)
    return (
      <SettingsSection.Body>
        <Text muted>Loading permissions…</Text>
      </SettingsSection.Body>
    );

  if (grants.isError || blocked)
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
          onChange={edit}
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
