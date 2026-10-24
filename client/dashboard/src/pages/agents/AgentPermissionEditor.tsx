import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { GrantRuleDrawerContent } from "@/pages/access/GrantRuleDrawerContent";
import { computeRuleLabel } from "@/pages/access/roleDialogState";
import {
  RolePermissionsSection,
  type ScopeGroup,
} from "@/pages/access/RolePermissionsSection";
import { invalidateAllAgentsListPolicyGrants } from "@gram/client/react-query/agentsListPolicyGrants.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import {
  AGENT_POLICY_SCOPES,
  agentPolicyViewFromGrants,
  agentPolicyGrantsFromDraft,
  agentPolicyFingerprint,
  diffAgentPolicyGrants,
  invalidateAgentPolicy,
  isAgentPolicyNarrowable,
} from "./agent-policy-grants";

/** The role permission picker and resource drawer, applied to an agent's policy. */
export function AgentPermissionEditor({
  agent,
  grants,
  onClose,
}: {
  agent: ManagedAgent;
  grants: AgentPolicyGrant[];
  onClose: () => void;
}): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const { user } = useSession();
  const queryClient = useQueryClient();
  const [base, setBase] = useState(grants);
  const [view, setView] = useState(() => agentPolicyViewFromGrants(grants));
  const [needsReload, setNeedsReload] = useState(false);
  const rbac = useRBAC();
  const [draft, setDraft] = useState(view.draft);
  const [changedScopes, setChangedScopes] = useState<Set<string>>(new Set());
  const [editingScope, setEditingScope] = useState<string | null>(null);
  const scope = AGENT_POLICY_SCOPES.find((item) => item.slug === editingScope);
  const groups: ScopeGroup[] = [];
  for (const definition of AGENT_POLICY_SCOPES) {
    if (view.preservedScopes.includes(definition.slug)) continue;
    const existing = groups.find(
      (group) => group.resourceType === definition.resourceType,
    );
    if (existing) existing.scopes.push(definition);
    else
      groups.push({
        label: definition.resourceType,
        resourceType: definition.resourceType,
        description: definition.description,
        scopes: [definition],
      });
  }
  const refresh = () =>
    Promise.all([
      invalidateAgentPolicy(queryClient, organization.id, user.id, agent.id),
      invalidateAllAgentsListPolicyGrants(queryClient),
    ]);
  const changes = diffAgentPolicyGrants(
    view.editable.filter((grant) => changedScopes.has(grant.scope)),
    agentPolicyGrantsFromDraft(draft).filter((grant) =>
      changedScopes.has(grant.scope),
    ),
  );
  const inferenceAdditions = changes.create.filter(
    (grant) => grant.scope === "inference:execute",
  );
  let authorizationError: string | null = null;
  if (inferenceAdditions.length > 0) {
    if (rbac.isLoading || rbac.error)
      authorizationError =
        "Your inference permissions could not be verified yet. Wait for permissions to load or reload this page before saving.";
    else {
      for (const grant of inferenceAdditions) {
        const projectId = grant.selector.projectId;
        if (!projectId || projectId === "*") {
          authorizationError =
            "Choose specific projects before adding inference permission.";
          break;
        }
        if (!rbac.hasScope("inference:execute", "*", projectId)) {
          const projectName =
            organization.projects.find((project) => project.id === projectId)
              ?.name ?? projectId;
          authorizationError = `You need inference permission in ${projectName} before granting it to this agent.`;
          break;
        }
      }
    }
  }
  const save = useMutation({
    mutationFn: async () => {
      if (!agent.permissions.write)
        throw new Error("You need permission to manage this agent.");
      if (needsReload)
        throw new Error("Reload saved permissions before trying again.");
      if (authorizationError) throw new Error(authorizationError);
      const current = await sdk.agents.listPolicyGrants({ agentId: agent.id });
      if (agentPolicyFingerprint(current) !== agentPolicyFingerprint(base)) {
        setNeedsReload(true);
        throw new Error(
          "Permissions changed while you were editing. Reload saved permissions before trying again.",
        );
      }
      let completed = 0;
      try {
        // Match the existing agent editor: add first so a rejected addition
        // cannot remove access the agent already holds. These are separate writes.
        for (const grant of changes.create) {
          await sdk.agents.createPolicyGrant({
            createAgentPolicyGrantForm: { agentId: agent.id, ...grant },
          });
          completed++;
        }
        for (const grant of changes.remove) {
          await sdk.agents.deletePolicyGrant({
            deleteAgentPolicyGrantForm: {
              agentId: agent.id,
              grantId: grant.id,
              expectedPolicy: { scope: grant.scope, selector: grant.selector },
            },
          });
          completed++;
        }
      } catch (failure) {
        setNeedsReload(true);
        const detail =
          failure instanceof Error ? failure.message : "The request failed.";
        const progress =
          completed > 0
            ? `${completed} ${completed === 1 ? "change was" : "changes were"} saved before the error.`
            : "A change may already have been saved.";
        throw new Error(
          `${progress} No changes were rolled back; closing does not undo saved changes. Reload saved permissions before trying again. ${detail}`,
        );
      }
    },
    onSuccess: async () => {
      await refresh();
      onClose();
    },
    onError: async () => {
      await refresh();
    },
  });
  const reload = useMutation({
    mutationFn: () => sdk.agents.listPolicyGrants({ agentId: agent.id }),
    onSuccess: (current) => {
      const next = agentPolicyViewFromGrants(current);
      setBase(current);
      setView(next);
      setDraft(next.draft);
      setChangedScopes(new Set());
      setEditingScope(null);
      setNeedsReload(false);
      save.reset();
      queryClient.setQueryData(
        ["agent-policy-grants", organization.id, agent.id],
        current,
      );
    },
  });
  const incomplete = Object.values(draft).some(
    (selectors) => selectors?.length === 0,
  );
  const close = () => {
    if (!save.isPending && !reload.isPending) onClose();
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <Dialog.Content className="sm:max-w-3xl">
        <Dialog.Header>
          <Dialog.Title>Edit permissions</Dialog.Title>
          <Dialog.Description>
            Choose the permissions {agent.name} can use and the projects or
            resources they apply to.
          </Dialog.Description>
        </Dialog.Header>
        {view.preservedScopes.length > 0 && (
          <Text small muted>
            Permissions with additional constraints are preserved:{" "}
            {view.preservedScopes.join(", ")}.
          </Text>
        )}
        <RolePermissionsSection
          groups={groups}
          subjectLabel="this agent"
          selectedScopes={new Set(Object.keys(draft))}
          disabled={
            save.isPending ||
            reload.isPending ||
            needsReload ||
            !agent.permissions.write
          }
          onToggleScope={(slug) => {
            if (
              save.isPending ||
              reload.isPending ||
              needsReload ||
              !agent.permissions.write
            )
              return;
            setChangedScopes((previous) => new Set(previous).add(slug));
            setDraft((previous) => {
              const next = { ...previous };
              if (slug in next) delete next[slug];
              else {
                const definition = AGENT_POLICY_SCOPES.find(
                  (item) => item.slug === slug,
                );
                next[slug] =
                  definition && isAgentPolicyNarrowable(definition.resourceType)
                    ? []
                    : null;
              }
              return next;
            });
          }}
          renderScopeRule={(definition) => {
            const resource = AGENT_POLICY_SCOPES.find(
              (item) => item.slug === definition.slug,
            );
            if (!resource || !isAgentPolicyNarrowable(resource.resourceType))
              return (
                <Text small muted>
                  All resources
                </Text>
              );
            return (
              <Button
                variant="secondary"
                size="sm"
                disabled={
                  save.isPending ||
                  reload.isPending ||
                  needsReload ||
                  !agent.permissions.write
                }
                onClick={() => setEditingScope(definition.slug)}
                aria-label={`Choose resources for ${definition.slug}`}
              >
                {computeRuleLabel(
                  draft[definition.slug] ?? null,
                  resource.resourceType,
                  organization.projects,
                )}
              </Button>
            );
          }}
        />
        {authorizationError && (
          <Text role="alert" destructive>
            {authorizationError}
          </Text>
        )}
        {reload.error && (
          <Text role="alert" destructive>
            Could not reload saved permissions. Try again before making further
            changes.
          </Text>
        )}
        {needsReload && (
          <div className="space-y-2">
            <Text small muted>
              Reload replaces this draft with the permissions currently saved.
            </Text>
            <Button
              variant="secondary"
              disabled={save.isPending || reload.isPending}
              onClick={() => reload.mutate()}
            >
              Reload saved permissions
            </Button>
          </div>
        )}
        {save.error && (
          <Text role="alert" destructive>
            {save.error.message}
          </Text>
        )}
        <Dialog.Footer>
          <Button
            variant="secondary"
            disabled={save.isPending || reload.isPending}
            onClick={close}
          >
            {needsReload ? "Close" : "Cancel"}
          </Button>
          <Button
            disabled={
              save.isPending ||
              reload.isPending ||
              needsReload ||
              !!authorizationError ||
              !agent.permissions.write ||
              incomplete
            }
            onClick={() => save.mutate()}
          >
            Save permissions
          </Button>
        </Dialog.Footer>
        {scope && (
          <Dialog
            open
            onOpenChange={(open) => {
              if (!open) setEditingScope(null);
            }}
          >
            <Dialog.Content>
              <Dialog.Header>
                <Dialog.Title>Choose resources</Dialog.Title>
                <Dialog.Description>{scope.description}</Dialog.Description>
              </Dialog.Header>
              <GrantRuleDrawerContent
                resourceType={scope.resourceType}
                scope={scope.slug}
                selectors={draft[scope.slug] ?? null}
                allowedPanels={
                  scope.resourceType === "inference" &&
                  view.draft[scope.slug] !== null
                    ? ["projects"]
                    : undefined
                }
                onChangeSelectors={(selectors) => {
                  setChangedScopes((previous) =>
                    new Set(previous).add(scope.slug),
                  );
                  setDraft((previous) => ({
                    ...previous,
                    [scope.slug]: selectors,
                  }));
                }}
              />
              <Dialog.Footer>
                <Button onClick={() => setEditingScope(null)}>Done</Button>
              </Dialog.Footer>
            </Dialog.Content>
          </Dialog>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
