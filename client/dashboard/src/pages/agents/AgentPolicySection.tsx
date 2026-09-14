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
  discardAgentPolicyCaches,
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
          // A draft is only valid for the context it was built in, so any of
          // these changing must discard it.
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

  // A save spans several awaits; values captured in its closure go stale.
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
  const current = draft?.value ?? view.draft;
  const dirty = draft !== null;

  // The first edit pins the base every later comparison is measured against.
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
    // Locked before the first await, so a second click cannot write twice.
    setSaving(true);
    setError(null);

    const stop = (message: string) => {
      setSaving(false);
      setBlocked(true);
      setError(message);
    };

    // Judge against a fresh read: nothing need ever have refetched the cache
    // while this draft was open.
    //
    // This narrows the window, it does not close it. The grant endpoints take
    // no precondition, so this is not a compare-and-swap — a change landing
    // between this read and the writes below is still missed. What the pinned
    // base does guarantee is that a grant this draft never saw is not deleted.
    let confirmed;
    try {
      confirmed = await grants.refetch();
    } catch {
      confirmed = undefined;
    }
    // Context may have changed while the read was in flight; this instance is
    // gone and must not write.
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

    // A grant this draft never saw is indistinguishable from one the user
    // removed, so diffing against a moved ceiling would delete someone else's
    // work. Write nothing.
    if (agentPolicyFingerprint(confirmed.data) !== draft.fingerprint) {
      stop(
        "Someone else changed this agent's permissions while you were editing. Nothing was saved, and their changes are intact. Reload and make your changes again.",
      );
      return;
    }

    // Preserved grants are outside `base`, so they are never removed or
    // re-created.
    const { create, remove } = diffAgentPolicyGrants(
      draft.base,
      agentPolicyGrantsFromDraft(draft.value),
    );
    // Cleanup below must target the ids this save began under, never whatever
    // the app has moved to by the time it finishes.
    const savedOrganizationId = organizationId;
    const savedUserId = userId;
    const savedAgentID = agent.id;

    // Set before the await, not after: a rejection is as ambiguous as an
    // abandonment, because the server may commit and lose the response.
    let issued = false;
    let abandoned = false;
    let writeError: unknown = null;
    try {
      // Removals first: a narrowed permission would otherwise collide with the
      // grant it replaces, which the server rejects as a duplicate.
      for (const grant of remove) {
        if (!mounted.current || !canWriteRef.current) {
          abandoned = true;
          break;
        }
        issued = true;
        await sdk.agents.deletePolicyGrant({
          agentPolicyGrantIDForm: { agentId: savedAgentID, grantId: grant.id },
        });
      }
      for (const form of create) {
        if (abandoned) break;
        if (!mounted.current || !canWriteRef.current) {
          abandoned = true;
          break;
        }
        issued = true;
        await sdk.agents.createPolicyGrant({
          createAgentPolicyGrantForm: { agentId: savedAgentID, ...form },
        });
      }
    } catch (cause) {
      writeError = cause;
    }

    // Anything issued may have landed, so the cached ceiling and the delegable
    // candidates it feeds may describe a state that never existed. This must
    // run before any early return below.
    if (!mounted.current || abandoned) {
      if (issued) {
        await discardAgentPolicyCaches(
          queryClient,
          savedOrganizationId,
          savedUserId,
          savedAgentID,
        );
      }
      if (!mounted.current) return;
      setSaving(false);
      setBlocked(true);
      setError(
        issued
          ? "Your permission to change this agent's permissions ended part-way through saving, so some changes may have been applied. Reload to see what is stored."
          : "Your permission to change this agent's permissions ended before anything was saved.",
      );
      return;
    }

    // Each grant is its own request, so after a partial failure the cache is
    // neither the draft nor what is stored. The editor stays locked until a
    // read confirms the ceiling.
    let refreshed;
    try {
      refreshed = await grants.refetch();
    } catch {
      refreshed = undefined;
    }
    if (!mounted.current) {
      // The confirming read landed too late to be shown, so nothing checked it
      // against this save. Drop it rather than let the next reader trust it.
      if (issued) {
        await discardAgentPolicyCaches(
          queryClient,
          savedOrganizationId,
          savedUserId,
          savedAgentID,
        );
      }
      return;
    }
    setSaving(false);

    if (!refreshed || refreshed.isError || refreshed.data === undefined) {
      // Fail closed: an editable base from the pre-save cache would invite a
      // second save on top of a ceiling that has moved.
      setBlocked(true);
      setError(
        "Could not confirm this agent's stored permissions after saving, so some changes may not have been applied. Reload before editing again.",
      );
      return;
    }

    setDraft(null);
    void invalidateAgentPolicy(
      queryClient,
      savedOrganizationId,
      savedUserId,
      savedAgentID,
    );
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
    if (!mounted.current) return;
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
