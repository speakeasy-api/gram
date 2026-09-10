import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { GrantRuleDrawerContent } from "@/pages/access/GrantRuleDrawerContent";
import { PermissionScopeControl } from "@/pages/access/PermissionScopeControl";
import { RolePermissionsSection } from "@/pages/access/RolePermissionsSection";
import { computeRuleLabel } from "@/pages/access/roleDialogState";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import { useEffect, useMemo, useState, type JSX } from "react";

import {
  AGENT_POLICY_SCOPES,
  AGENT_POLICY_SCOPE_GROUPS,
  isAgentPolicyNarrowable,
  type AgentPolicyDraft,
} from "./agent-policy-grants";

/**
 * The permissions an agent may ever be delegated, as the same list-and-narrow
 * control roles use.
 *
 * This is a ceiling, not a credential: adding a permission here grants nothing
 * on its own. Issuing an API key intersects this list with the owner's live
 * permissions and the issuer's own, so a ceiling can safely be set by an owner
 * who holds no RBAC grant of their own.
 */
export function AgentPolicyEditor({
  draft,
  onChange,
  disabled,
  lockedScopes,
}: {
  draft: AgentPolicyDraft;
  onChange: (next: AgentPolicyDraft) => void;
  disabled?: boolean;
  /** Scopes held by a stored grant the editor must not rewrite. */
  lockedScopes?: string[];
}): JSX.Element {
  const organization = useOrganization();
  const [editingScope, setEditingScope] = useState<string | null>(null);
  const [pending, setPending] = useState<Selector[]>([]);

  const groups = useMemo(() => {
    if (!lockedScopes?.length) return AGENT_POLICY_SCOPE_GROUPS;
    const locked = new Set(lockedScopes);
    // A locked scope is not offered at all. Adding it here would sit a fresh
    // wildcard grant beside the constrained one it cannot replace.
    return AGENT_POLICY_SCOPE_GROUPS.map((group) => ({
      ...group,
      scopes: group.scopes.filter((scope) => !locked.has(scope.slug)),
    }));
  }, [lockedScopes]);

  // Losing write access mid-edit must not leave a live picker behind: the
  // dialog sits above the disabled rows and would otherwise still accept a
  // choice. Saving cannot begin while it is open, so `disabled` turning true
  // here only ever means the permission went away.
  useEffect(() => {
    if (disabled) {
      setEditingScope(null);
      setPending([]);
    }
  }, [disabled]);

  const projectList = useMemo(
    () =>
      (organization.projects ?? []).map((project) => ({
        id: project.id,
        name: project.name,
      })),
    [organization.projects],
  );

  const editingDefinition = editingScope
    ? (AGENT_POLICY_SCOPES.find((scope) => scope.slug === editingScope) ?? null)
    : null;

  const toggleScope = (scope: Scope) => {
    const next = { ...draft };
    // A permission starts at the breadth the scope itself has — "All servers",
    // "All projects" — stated on the row and narrowable in place. It is never
    // applied to a permission the user did not add.
    if (scope in next) delete next[scope];
    else next[scope] = null;
    onChange(next);
  };

  const openResourcePicker = (scope: string) => {
    setEditingScope(scope);
    setPending(draft[scope] ?? []);
  };

  const closeResourcePicker = () => {
    setEditingScope(null);
    setPending([]);
  };

  return (
    <>
      <RolePermissionsSection
        groups={groups}
        selectedScopes={new Set(Object.keys(draft))}
        disabled={disabled}
        onToggleScope={toggleScope}
        subjectLabel="this agent"
        renderScopeRule={(definition: ScopeDefinition) => {
          if (!(definition.slug in draft)) return null;
          // Environments and risk policies are not lists you pick from, so the
          // row carries no control rather than an "All" chip that cannot
          // change.
          if (!isAgentPolicyNarrowable(definition.resourceType)) return null;
          const selectors = draft[definition.slug] ?? null;
          return (
            <PermissionScopeControl
              allowRule={{
                id: definition.slug,
                effect: "allow",
                selectors,
              }}
              // Agent policy is allow-only: the server rejects any other
              // effect, so there is no exception to state or add.
              denyRules={[]}
              canAddException={false}
              resourceType={definition.resourceType}
              allowLabel={computeRuleLabel(
                selectors,
                definition.resourceType,
                projectList,
              )}
              denyLabel={() => ""}
              disabled={disabled}
              onChooseSpecific={() => openResourcePicker(definition.slug)}
              onResetToAll={() =>
                onChange({ ...draft, [definition.slug]: null })
              }
              onAddException={() => {}}
              onEditException={() => {}}
              onRemoveException={() => {}}
            />
          );
        }}
      />

      <Dialog
        open={editingScope !== null && !disabled}
        onOpenChange={(open) => {
          if (!open) closeResourcePicker();
        }}
      >
        <Dialog.Content className="max-w-3xl">
          <Dialog.Header>
            <Dialog.Title>
              Choose resources for {editingDefinition?.slug}
            </Dialog.Title>
            <Dialog.Description>
              The agent may never be delegated more than what you pick here.
              What a given API key actually carries is narrowed again when the
              key is issued.
            </Dialog.Description>
          </Dialog.Header>
          {editingDefinition && (
            <GrantRuleDrawerContent
              resourceType={editingDefinition.resourceType}
              scope={editingDefinition.slug}
              selectors={pending}
              onChangeSelectors={(selectors) => setPending(selectors ?? [])}
            />
          )}
          {pending.length === 0 && (
            <Text muted small>
              Nothing is selected yet, so this permission applies to nothing.
              Pick at least one resource, or cancel to leave it unrestricted.
            </Text>
          )}
          <Dialog.Footer>
            <Button variant="secondary" onClick={closeResourcePicker}>
              Cancel
            </Button>
            <Button
              disabled={disabled || pending.length === 0}
              onClick={() => {
                if (!editingScope || disabled) return;
                onChange({ ...draft, [editingScope]: pending });
                closeResourcePicker();
              }}
            >
              Done
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
