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
import { useOrgAssistants } from "@/pages/access/useOrgAssistants";
import {
  RolePermissionsSection,
  type ScopeGroup,
} from "@/pages/access/RolePermissionsSection";
import { invalidateAllAgentsListPolicyGrants } from "@gram/client/react-query/agentsListPolicyGrants.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import { categoryCopy } from "@/pages/access/roleMatrixModel";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import {
  useResourceLinkMaps,
  type ResourceLinkMaps,
} from "@/pages/access/useResourceLinkMaps";
import { ANY_RESOURCE } from "./agent-api-key-grants";
import {
  agentPolicyChangedScopes,
  agentPolicyViewFromGrants,
  agentPolicyGrantsFromDraft,
  agentPolicyFingerprint,
  agentPolicyUnrestrictedLabel,
  diffAgentPolicyGrants,
  invalidateAgentPolicy,
  isAgentPolicyEditorScope,
  isAgentPolicyNarrowable,
  isAgentPolicySelectorUnrestricted,
  type AgentPolicyDiff,
} from "./agent-policy-grants";
import { useAgentPolicyScopes } from "./useAgentPolicyScopes";

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
  const [editingScope, setEditingScope] = useState<string | null>(null);
  // The open picker works on its own copy: Done keeps it, and closing the
  // picker any other way leaves the permission as it was.
  const [pickerSelectors, setPickerSelectors] = useState<Selector[] | null>(
    null,
  );
  const changedScopes = agentPolicyChangedScopes(view.draft, draft);
  // The permissions an agent may hold come from the server, so their names
  // and descriptions are the role editor's; nothing here is a copy.
  const {
    scopes: policyScopes,
    isLoading: scopesLoading,
    isError: scopesError,
  } = useAgentPolicyScopes();
  const scope = policyScopes.find((item) => item.slug === editingScope);
  const groups: ScopeGroup[] = [];
  for (const definition of policyScopes) {
    if (!isAgentPolicyEditorScope(definition.slug)) continue;
    if (view.preservedScopes.includes(definition.slug)) continue;
    const copy = categoryCopy(definition.category ?? definition.resourceType);
    const existing = groups.find((group) => group.label === copy.label);
    if (existing) existing.scopes.push(definition);
    else
      groups.push({
        label: copy.label,
        resourceType: definition.resourceType,
        description: copy.description,
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
      // Nothing changed, so nothing is written.
      if (changes.create.length === 0 && changes.remove.length === 0) return;
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
            resources they apply to. The MCP servers it connects to are edited
            in their own section.
          </Dialog.Description>
        </Dialog.Header>
        {view.preservedScopes.length > 0 && (
          <Text small muted>
            Kept as they are, because this dialog cannot show them exactly:{" "}
            {view.preservedScopes.join(", ")}.
          </Text>
        )}
        {scopesLoading && (
          <Text small muted>
            Loading the permissions an agent can hold…
          </Text>
        )}
        {scopesError && (
          <Text role="alert" destructive>
            The permissions an agent can hold could not be loaded. Reload this
            page before making changes.
          </Text>
        )}
        {/* Without the catalogue the picker is empty, and an empty picker
            reads as an agent with no permissions. */}
        {!scopesLoading && !scopesError && (
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
              setDraft((previous) => {
                const next = { ...previous };
                if (slug in next) delete next[slug];
                else {
                  const definition = policyScopes.find(
                    (item) => item.slug === slug,
                  );
                  next[slug] =
                    definition &&
                    isAgentPolicyNarrowable(definition.resourceType)
                      ? []
                      : null;
                }
                return next;
              });
            }}
            renderScopeRule={(definition) => {
              const resource = policyScopes.find(
                (item) => item.slug === definition.slug,
              );
              if (!resource || !isAgentPolicyNarrowable(resource.resourceType))
                return (
                  <Text small muted>
                    {agentPolicyUnrestrictedLabel(resource?.resourceType)}
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
                  onClick={() => {
                    setPickerSelectors(draft[definition.slug] ?? null);
                    setEditingScope(definition.slug);
                  }}
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
        )}
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
        <AgentPolicyChanges changes={changes} />
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
              scopesLoading ||
              scopesError ||
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
                selectors={pickerSelectors}
                allowedPanels={
                  scope.resourceType === "inference" &&
                  view.draft[scope.slug] !== null
                    ? ["projects"]
                    : undefined
                }
                onChangeSelectors={setPickerSelectors}
              />
              <Dialog.Footer>
                <Button
                  onClick={() => {
                    setDraft((previous) => ({
                      ...previous,
                      [scope.slug]: pickerSelectors,
                    }));
                    setEditingScope(null);
                  }}
                >
                  Done
                </Button>
              </Dialog.Footer>
            </Dialog.Content>
          </Dialog>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

/** What a grant reaches, in words, for the list of changes a save makes. */
function agentPolicyGrantTarget(
  selector: AgentPolicySelector,
  maps: ResourceLinkMaps,
  assistantNames: Map<string, { name: string; projectName: string }>,
): string {
  const projectName = (id: string) => maps.projectMap.get(id)?.name ?? id;
  if (isAgentPolicySelectorUnrestricted(selector))
    return agentPolicyUnrestrictedLabel(selector.resourceKind);
  const inProject =
    selector.projectId && selector.projectId !== ANY_RESOURCE
      ? ` in ${projectName(selector.projectId)}`
      : "";
  if (selector.resourceId === ANY_RESOURCE) return `All resources${inProject}`;
  if (selector.resourceKind === "project" || selector.resourceKind === "skill")
    return projectName(selector.resourceId);
  if (selector.resourceKind === "mcp") {
    // A name only counts when its server is in the project the grant names;
    // otherwise the id stands, rather than another project's server.
    const inGrantProject = (projectId: string) =>
      !selector.projectId ||
      selector.projectId === ANY_RESOURCE ||
      selector.projectId === projectId;
    const toolset = maps.toolsetMap.get(selector.resourceId);
    const mcpServer = maps.mcpServerMap.get(selector.resourceId);
    const server =
      (toolset && inGrantProject(toolset.projectId)
        ? toolset.name
        : undefined) ??
      (mcpServer && inGrantProject(mcpServer.projectId)
        ? mcpServer.name
        : undefined) ??
      selector.resourceId;
    return `${server}${inProject}`;
  }
  if (selector.resourceKind === "assistant") {
    const assistant = assistantNames.get(selector.resourceId);
    if (!assistant) return `assistant ${selector.resourceId}${inProject}`;
    return `${assistant.name}${inProject || ` in ${assistant.projectName}`}`;
  }
  return `${selector.resourceKind} ${selector.resourceId}${inProject}`;
}

/**
 * Every grant a save adds or removes, shown before it is made. Nothing is
 * removed from an agent without the reader having seen it listed here.
 */
function AgentPolicyChanges({
  changes,
}: {
  changes: AgentPolicyDiff;
}): JSX.Element | null {
  const all = [...changes.create, ...changes.remove];
  const maps = useResourceLinkMaps(
    all.some(
      ({ selector }) =>
        selector.resourceKind === "mcp" && selector.resourceId !== ANY_RESOURCE,
    ),
  );
  // Assistants are named as the picker names them, with their project.
  const assistants = useOrgAssistants(
    all.some(
      ({ selector }) =>
        selector.resourceKind === "assistant" &&
        selector.resourceId !== ANY_RESOURCE,
    ),
  );
  const assistantNames = new Map(
    assistants.groups.flatMap((group) =>
      group.assistants.map(
        (assistant): [string, { name: string; projectName: string }] => [
          assistant.id,
          { name: assistant.name, projectName: group.projectName },
        ],
      ),
    ),
  );
  if (all.length === 0) return null;
  const rows = [
    ...changes.remove.map((grant) => ({ grant, removed: true })),
    ...changes.create.map((grant) => ({ grant, removed: false })),
  ];
  return (
    <div
      role="region"
      aria-label="Changes to save"
      className="border-border space-y-1 border px-3 py-2"
    >
      <Text small className="font-medium">
        Saving makes these changes
      </Text>
      <ul className="space-y-0.5">
        {rows.map(({ grant, removed }, index) => (
          <li key={index} className="flex flex-wrap gap-x-2 text-sm">
            <span className={removed ? "text-destructive" : undefined}>
              {removed ? "Removes" : "Adds"}
            </span>
            <span className="font-mono">{grant.scope}</span>
            <span className="text-muted-foreground">
              {agentPolicyGrantTarget(grant.selector, maps, assistantNames)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
