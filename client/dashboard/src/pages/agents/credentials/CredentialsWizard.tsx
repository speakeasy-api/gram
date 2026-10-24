import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type JSX,
} from "react";
import { useSearchParams } from "react-router";
import {
  agentPolicyGrantsFromDraft,
  invalidateAgentPolicy,
  type AgentPolicyDraft,
} from "../agent-policy-grants";
import { buildRequestedGrants } from "../agent-api-key-grants";
import { discoverKeyServerGrants } from "../agent-key-discovery";
import {
  agentGatewayURL,
  isCredentialSafe,
  mintInstallCommand,
} from "./gateway";
import { StepInstall } from "./StepInstall";
import { StepServers, type ServerSelection } from "./StepServers";
import { StepVerify, type VerifyState } from "./StepVerify";
import { StepWorkload, WorkloadPlatformPicker } from "./StepWorkload";
import { useWorkloadSetup } from "./useWorkloadSetup";
import { CUSTOM_PLATFORM_KEY } from "./CustomSetupInstructions";
import {
  CONNECT_SCOPE,
  grantChanges,
  grantedServers,
  reachCoversProject,
  wildcardConnectGrant,
  type GrantedServer,
  type WildcardConnectGrant,
} from "@/pages/agents/agentPolicyEdit";
import { useAgentsCreatePolicyGrantMutation } from "@gram/client/react-query/agentsCreatePolicyGrant.js";
import { useAgentsDeletePolicyGrantMutation } from "@gram/client/react-query/agentsDeletePolicyGrant.js";
import { useRenameAgentMutation } from "@gram/client/react-query/renameAgent.js";
import { useAgents } from "@gram/client/react-query/agents.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import { useAgentAdmissions } from "@/pages/agents/agentAdmissions";
import { useCatalogEntries, valuesFromSubject } from "./workloadCatalog";
import { useRBAC } from "@/hooks/useRBAC";
import { useServerInventory } from "./useServerInventory";
import {
  WizardFooter,
  WizardStepHeader,
  WizardStepper,
  WizardSummary,
} from "./WizardChrome";

/**
 * Creating an agent identity here is only ever a means to provisioning one
 * somewhere else, so the flow runs to that end: name it, choose what it may
 * reach, pick how it proves who it is, hand the runtime its credential, and
 * watch for the first call.
 *
 * The identity is created at the end of the choices, not the start: everything
 * before Install is still a draft the user can abandon without leaving a
 * half-made agent behind.
 */

/** Each step's name, and the one or two words it shrinks to in a full row. */
const STEPS = [
  { label: "Name", short: "Name" },
  { label: "Server selection", short: "Servers" },
  { label: "Credential", short: "Credential" },
  { label: "Install", short: "Install" },
  { label: "Verify", short: "Verify" },
];

/** How the agent proves who it is. */
type Credential = "api-key" | "workload";

/** Keys live 90 days unless the agent page rotates them sooner. */
const KEY_LIFETIME_DAYS = 90;

/**
 * What the agent API accepts as a name. The input's own `maxLength` stops
 * typing past it but not a value this file writes into state, which would be
 * refused on submit with nothing on screen to explain why.
 */
const NAME_LIMIT = 120;

/**
 * The preset name, with a number appended while the organization already
 * has one: the server refuses a duplicate, and a prefilled field that is
 * refused on submit is worse than no prefill at all.
 */
function freeName(base: string, taken: Set<string>): string {
  const plain = base.slice(0, NAME_LIMIT);
  if (!taken.has(plain)) return plain;
  // The suffix is what makes the name free, so the base gives up the
  // characters it needs rather than the suffix falling off the end.
  const numbered = (suffix: number) =>
    `${base.slice(0, NAME_LIMIT - ` ${suffix}`.length)} ${suffix}`;
  // Every candidate ends in its own number, so they are all distinct and one
  // of the first `taken.size + 1` of them must be free: the loop ends on a
  // name the server will accept rather than giving up on the occupied one.
  let suffix = 2;
  while (taken.has(numbered(suffix))) suffix++;
  return numbered(suffix);
}

export function CredentialsWizard({
  agent,
  initialPlatformKey,
  editing = false,
  grants,
  onDone,
  onBusy,
  verifyPollMs = 5000,
}: {
  /**
   * The platform this flow was opened for, from a link that names one. The
   * credential question is then already answered, and the flow starts where
   * the reader still has something to decide.
   */
  initialPlatformKey?: string;
  /**
   * Change what an agent already is — its name, its reach — rather than
   * provision it again. The credential steps are not part of that: a key is
   * issued from the agent's own page, and a platform from this flow.
   */
  editing?: boolean;
  /** The servers the agent already reaches, for the editing flow. */
  grants?: AgentPolicyGrant[];
  /**
   * The agent being provisioned again. While creating one there is no agent:
   * the identity does not exist until the choices are made, so every step
   * ahead is still a draft and none of them can be revisited.
   */
  agent?: ManagedAgent;
  /** Leaves the wizard for the agent it created, or the list if it made none. */
  onDone: (agentID?: string) => void;
  onBusy?: (busy: boolean) => void;
  /**
   * How often the verify step re-reads the key. Only a test shortens it: at
   * the real cadence a test either waits five seconds per tick or races one.
   */
  verifyPollMs?: number;
}): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const project = useProject();
  const { user } = useSession();
  const queryClient = useQueryClient();
  const inventory = useServerInventory();
  const catalog = useCatalogEntries();
  // Trusting a platform and admitting a subject are both writes against the
  // organization's workload identities, so the choice is offered only to
  // someone who could carry it out.
  const { hasAllScopes } = useRBAC();
  const canFederate = hasAllScopes(["workload:write"]);
  const renameAgent = useRenameAgentMutation();
  // Only to keep a prefilled name from colliding, so a failure to read the
  // list leaves the field empty rather than blocking the flow.
  const agents = useAgents({}, undefined, { throwOnError: false });
  const existingNames = useMemo(
    () =>
      agents.data === undefined
        ? undefined
        : new Set(agents.data.result.items.map((item) => item.name)),
    [agents.data],
  );
  const createGrant = useAgentsCreatePolicyGrantMutation();
  const deleteGrant = useAgentsDeletePolicyGrantMutation();

  // Provisioning an agent that already exists: its name and scope are settled,
  // so the flow is only about which servers this key reaches and how the key
  // is delivered. Every step is a destination because none of them creates
  // anything until the key is issued.
  const existing = agent !== undefined;

  // Opened for a named platform: the credential is settled and the agent is
  // named after what will be signing in, so the flow starts at the servers —
  // the first thing still left to decide.
  const preset =
    initialPlatformKey === undefined
      ? undefined
      : catalog.entries.find((entry) => entry.key === initialPlatformKey);
  // The step lives in the URL so a step can be linked, reloaded and gone
  // back to. Replace rather than push: the browser's Back belongs to the
  // page the reader came from, and the wizard has its own Back.
  const [params, setParams] = useSearchParams();
  const defaultStep = initialPlatformKey === undefined ? 0 : 1;
  // Presence first: `Number(null)` and `Number("")` are both 0, which reads as
  // a deep link to the first step and leaves `defaultStep` unreachable.
  const linked = params.get("step");
  const fromUrl =
    linked === null || linked.trim() === "" ? Number.NaN : Number(linked);
  const step =
    Number.isInteger(fromUrl) && fromUrl >= 0 ? fromUrl : defaultStep;
  // Stable, so an effect that corrects the step can depend on it.
  const setStep = useCallback(
    (next: number) => {
      setParams(
        (current) => {
          const updated = new URLSearchParams(current);
          updated.set("step", String(next));
          return updated;
        },
        { replace: true },
      );
    },
    [setParams],
  );
  const [name, setName] = useState(agent?.name ?? "");
  // null until the person picks, so the default can follow the project as it

  const [selected, setSelected] = useState<ServerSelection[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [provisioning, setProvisioning] = useState(false);
  const [saving, setSaving] = useState(false);
  // The credential the agent already holds, when one is being edited: an
  // agent that signs in with a platform's tokens must not open on "API key".
  const admitted = useAgentAdmissions(agent?.id ?? "").admissions[0];
  const admittedEntry =
    admitted === undefined
      ? undefined
      : catalog.entries.find((entry) => entry.issuer.value === admitted.issuer);
  const [credential, setCredential] = useState<Credential>(
    initialPlatformKey === undefined ? "api-key" : "workload",
  );
  const [platformKey, setPlatformKey] = useState<string | null>(
    initialPlatformKey ?? null,
  );
  // Applied once the catalog and the admission have both arrived, and only
  // while the reader has not chosen something else.
  const adopted = useRef(false);
  useEffect(() => {
    if (adopted.current || !editing || admitted === undefined) return;
    // Both, not just the admission: the entry is looked up in the catalog, so
    // running before it arrives latches the custom key, and `adopted` then
    // stops the real platform from ever being recognised.
    if (catalog.isPending) return;
    adopted.current = true;
    setCredential("workload");
    setPlatformKey(admittedEntry?.key ?? CUSTOM_PLATFORM_KEY);
  }, [editing, admitted, admittedEntry, catalog.isPending]);
  // The catalog loads after the first render, so the preset name is filled
  // in when it arrives rather than in a state initializer that has nothing
  // to read yet. Only while the field is still untouched.
  const named = useRef(false);
  useEffect(() => {
    if (named.current || preset === undefined || name !== "") return;
    if (existingNames === undefined) return;
    named.current = true;
    setName(freeName(preset.displayName, existingNames));
  }, [preset, name, existingNames]);

  // The servers the agent already reaches, filled in once its grants and the
  // server list have both arrived, and only once: after that the selection
  // is the reader's.
  //
  // Only a server list that actually loaded counts. A failed read leaves the
  // selection empty, which is indistinguishable from "no servers chosen" —
  // and saving that against the stored grants withdraws every one of them, so
  // nothing may be saved until this has run.
  const [reconciled, setReconciled] = useState(false);
  // The grants as the save last knew them, so a retry after a partial save
  // writes what is still missing rather than creating a grant a second time.
  const applied = useRef(new Map<string, GrantedServer>());
  // Keep the policy originally read for every pending deletion, including
  // grants created by a partially completed save. A rejected delete keeps it.
  const appliedPolicies = useRef(new Map<string, AgentPolicyGrant>());
  // The wildcard as the save last knew it, for the same reason: `grants` is
  // the answer the query gave before the save started, so a retry reading it
  // would create a second wildcard or ask for an id that is already gone.
  const appliedWildcard = useRef<WildcardConnectGrant | undefined>(undefined);
  useEffect(() => {
    if (reconciled || grants === undefined) return;
    if (inventory.isLoading || inventory.isError) return;
    const granted = grantedServers(grants);
    applied.current = granted;
    appliedPolicies.current = new Map(grants.map((grant) => [grant.id, grant]));
    setSelected(
      inventory.servers
        .filter((server) => granted.has(server.resourceId))
        .map((server) => ({ server })),
    );
    const wildcard = wildcardConnectGrant(grants);
    appliedWildcard.current = wildcard;
    // The wildcard is the broader claim, so it wins the display: an agent
    // holding one reaches every server whatever else its policy lists.
    setAllServers(wildcard !== undefined);
    setReconciled(true);
  }, [
    grants,
    inventory.servers,
    inventory.isLoading,
    inventory.isError,
    reconciled,
  ]);
  // A wildcard grant reaches every server its selector covers and is
  // deliberately not a row in the list, so the reader has to be told it is
  // there — and how far it goes, because one scoped to a project does not
  // reach the organization's other projects.
  const wildcardReach =
    editing && grants !== undefined ? wildcardConnectGrant(grants) : undefined;
  // "Every server, including ones added later" as a choice the picker can
  // hold, rather than a state it can only describe. A wildcard grant is what
  // it saves to, so an agent that already has one opens with it on and the
  // list reads as what the policy actually allows.
  const [allServers, setAllServers] = useState(false);

  // What provisioning produced. The secret is held only while this page is
  // open; nothing writes it to storage or to the query cache.
  const [agentID, setAgentID] = useState<string | null>(agent?.id ?? null);
  const [secret, setSecret] = useState<string | null>(null);
  const [command, setCommand] = useState<string | null>(null);
  const [commandError, setCommandError] = useState<string | null>(null);
  const [minting, setMinting] = useState(false);
  const [verify, setVerify] = useState<VerifyState>("waiting");
  const [firstCallAt, setFirstCallAt] = useState<Date | undefined>();
  const keyID = useRef<string | null>(null);
  // Minting a setup command authenticates with the key, so the key's own
  // access time moves before any runtime has called. Only an access after the
  // last thing this page did counts as the agent's first call.
  const lastSelfUse = useRef<number>(0);

  useEffect(() => {
    // Minting counts as busy too: the key exists by then, and leaving while
    // the setup command is still being prepared throws away the one chance
    // to copy a secret that is shown once. So does saving: it writes one
    // grant per request, and leaving part-way through stores half an edit.
    onBusy?.(provisioning || minting || saving);
  }, [provisioning, minting, saving, onBusy]);

  // Where the agent lives, not a question anyone is asked: a new agent
  // belongs to the project it is being created in. It was offered as a
  // choice, which was the same question the server list answers and a second
  // place for the two to disagree — `project_id` narrows nothing on its own,
  // and the grants below decide the whole of what the agent can reach.
  //
  // An existing agent's is its own and is stored on it: the project being
  // browsed is not necessarily the one it was bound to. Agents from before
  // the column existed hold NULL, which is "not yet scoped" rather than
  // "organization", so they keep being offered every project's servers.
  const scopedProjectID = existing ? agent.projectId : project.id || undefined;
  // Falls back to the id, not the browsed project: an agent bound to a project
  // the caller cannot see would otherwise be labelled with the wrong one.
  const projectName = (id: string): string =>
    (organization.projects ?? []).find((candidate) => candidate.id === id)
      ?.name ?? id;
  const scopeName = scopedProjectID
    ? projectName(scopedProjectID)
    : organization.name;
  // What "All servers" covers. An agent that already holds wildcards keeps
  // their own scope: re-issuing a key must not quietly narrow an
  // organization-wide grant down to the project the agent is labelled with,
  // nor drop the projects of every wildcard but the first. Empty is every
  // project. The reach is named on screen from the same value, so a wildcard
  // wider than its agent reads as what it is rather than as the agent's
  // project.
  const allServersProjectIDs = wildcardReach
    ? wildcardReach.projectIds
    : scopedProjectID
      ? [scopedProjectID]
      : [];
  const allServersScopeName =
    allServersProjectIDs.length === 0
      ? organization.name
      : allServersProjectIDs.map(projectName).join(", ");
  // The rows the picker offers. Under "All servers" they are the ones those
  // wildcards reach, so an organization-wide one shows every project's;
  // otherwise it is the project the agent is bound to.
  const listedServers = inventory.servers.filter((server) =>
    allServers
      ? reachCoversProject(allServersProjectIDs, server.projectId)
      : scopedProjectID === undefined || server.projectId === scopedProjectID,
  );

  const gatewayURL = agentID ? agentGatewayURL(agentID) : "";
  const platform = catalog.entries.find((entry) => entry.key === platformKey);

  // The verify step reads the key itself: a key with an access time is a
  // runtime that reached the gateway and was admitted.
  useEffect(() => {
    if (step !== 4 || !agentID || verify === "connected") return;
    let cancelled = false;
    const poll = async () => {
      try {
        const listed = await sdk.keys.list({ agentId: agentID });
        const issued = listed.keys.find((key) => key.id === keyID.current);
        if (
          !cancelled &&
          issued?.lastAccessedAt &&
          issued.lastAccessedAt.getTime() > lastSelfUse.current
        ) {
          setVerify("connected");
          setFirstCallAt(issued.lastAccessedAt);
        }
      } catch {
        // A failed poll says nothing about the agent; the next one retries.
      }
    };
    void poll();
    const timer = window.setInterval(() => void poll(), verifyPollMs);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [step, agentID, verify, sdk, verifyPollMs]);

  const regenerate = (key: string, url: string) => {
    // Minting authenticates with the key, so a plaintext endpoint would hand
    // it to anyone on the path. Loopback is exempt: it never reaches a
    // network. The same rule withholds the instructions on the step itself.
    if (!isCredentialSafe(url)) {
      setCommandError(
        `This deployment serves MCP over plaintext HTTP at ${url}, so a setup command is not prepared: it would send the agent key unencrypted. Configure an HTTPS server URL and reload.`,
      );
      return;
    }
    setMinting(true);
    setCommandError(null);
    lastSelfUse.current = Date.now();
    mintInstallCommand(url, key)
      .then((next) => {
        lastSelfUse.current = Date.now();
        setCommand(next);
      })
      .catch((failure: Error) => setCommandError(failure.message))
      .finally(() => setMinting(false));
  };

  /**
   * The agent this flow provisions, created on first use. Both credentials
   * need it and neither creates it until the choices before it are made, so
   * it is created once here and reused — a retry after a later failure must
   * not leave a second agent behind.
   */
  const ensureAgent = async (): Promise<string> => {
    // An agent that exists keeps its stored ceiling: this flow provisions
    // against it rather than rewriting what it may ever be delegated.
    if (agent) return agent.id;
    if (agentID) return agentID;
    // A new agent belongs to the project it is created in, so there is
    // nothing to create until that project has resolved. An empty id is a
    // context still loading, not an organization-wide agent: creating on it
    // would leave the agent bound to no project and, under "All servers", a
    // wildcard with no project on it — the whole organization, by accident.
    const projectID = project.id;
    if (!projectID) {
      throw new Error("This project is still loading. Try again in a moment.");
    }
    // "All servers" is one wildcard selector rather than today's inventory
    // listed out, so it covers the servers added after this is saved. Scoped
    // to the project, never the draft's bare `null`: `null` means
    // unrestricted and produces a wildcard with no project on it, which would
    // reach every project while the agent itself belongs to one.
    const draft: AgentPolicyDraft = {
      "mcp:connect": allServers
        ? [
            {
              resourceKind: "mcp" as const,
              resourceId: "*",
              projectId: projectID,
            },
          ]
        : selected.map((entry) => ({
            resourceKind: "mcp" as const,
            resourceId: entry.server.resourceId,
            projectId: entry.server.projectId,
          })),
    };
    const created = await sdk.agents.create({
      createAgentForm: {
        name: name.trim(),
        projectId: projectID,
        policyGrants: agentPolicyGrantsFromDraft(draft),
      },
    });
    setAgentID(created.id);
    void invalidateAgentPolicy(
      queryClient,
      organization.id,
      user.id,
      created.id,
    );
    void queryClient.invalidateQueries({
      queryKey: ["managed-agents", organization.id],
    });
    return created.id;
  };

  /**
   * Write an edit back: the name if it changed, and the difference between
   * the servers the agent reaches and the ones now chosen.
   *
   * A diff rather than a rewrite, because each grant is a row with its own
   * id and its own line in the audit log: replacing all of them would churn
   * every one of those on a change to a single server.
   */
  const save = async (): Promise<void> => {
    if (agent === undefined || grants === undefined) return;
    // The selection is only the agent's reach once the grants have been
    // reconciled into it; before that every grant would read as withdrawn.
    if (!reconciled || saving) return;
    setSaving(true);
    setError(null);
    try {
      if (name.trim() !== agent.name) {
        await renameAgent.mutateAsync({
          request: { renameAgentForm: { id: agent.id, name: name.trim() } },
        });
      }
      // Only the grants whose server this list offers: one naming a server the
      // reader was never shown cannot have been unchecked, so an absence from
      // the selection does not mean they gave it up.
      const offered = new Set(
        inventory.servers.map((server) => server.resourceId),
      );
      const known = new Map(
        [...applied.current].filter(([resourceId]) => offered.has(resourceId)),
      );
      // Under "All servers" the per-server rows say nothing the wildcard does
      // not already say, so the chosen list is empty and every named grant it
      // replaces is withdrawn. Otherwise it is the ticked list as it stands.
      const changes = grantChanges(
        known,
        allServers
          ? []
          : selected.map((entry) => ({
              resourceId: entry.server.resourceId,
              projectId: entry.server.projectId,
            })),
      );
      // The wildcard goes in before the named grants it supersedes come out,
      // so no moment of this save leaves the agent reaching less than both the
      // old policy and the new one allow.
      if (allServers && appliedWildcard.current === undefined) {
        const created = await createGrant.mutateAsync({
          request: {
            createAgentPolicyGrantForm: {
              agentId: agent.id,
              effect: "allow",
              scope: CONNECT_SCOPE,
              selector: {
                resourceKind: "mcp",
                resourceId: "*",
                // Scoped where the agent is, so "all servers" means all the
                // ones this agent could ever be pointed at, not the whole
                // organization's.
                ...(scopedProjectID ? { projectId: scopedProjectID } : {}),
              },
            },
          },
        });
        // Recorded as the write lands, so a failure further down this save
        // does not make the next attempt create a second wildcard.
        appliedPolicies.current.set(created.id, created);
        appliedWildcard.current = {
          projectIds: scopedProjectID ? [scopedProjectID] : [],
          grantIds: [created.id],
        };
      }
      for (const server of changes.add) {
        const created = await createGrant.mutateAsync({
          request: {
            createAgentPolicyGrantForm: {
              agentId: agent.id,
              effect: "allow",
              scope: CONNECT_SCOPE,
              selector: {
                resourceKind: "mcp",
                resourceId: server.resourceId,
                projectId: server.projectId,
              },
            },
          },
        });
        // Recorded as each write lands, so a later failure does not send the
        // whole diff again on the next attempt.
        appliedPolicies.current.set(created.id, created);
        applied.current.set(server.resourceId, {
          grantIds: [created.id],
          resourceId: server.resourceId,
          projectId: server.projectId,
        });
      }
      // Last, so a failure part-way through leaves the agent reaching more
      // than intended rather than less: the servers it is meant to keep are
      // already granted by the time the wildcard goes.
      const removals = allServers
        ? changes.remove
        : [...changes.remove, ...(appliedWildcard.current?.grantIds ?? [])];
      for (const grantId of removals) {
        const baseline = appliedPolicies.current.get(grantId);
        if (!baseline)
          throw new Error(
            "The original permission could not be found. Reopen the editor before saving.",
          );
        await deleteGrant.mutateAsync({
          request: {
            deleteAgentPolicyGrantForm: {
              agentId: agent.id,
              grantId,
              expectedPolicy: {
                scope: baseline.scope,
                selector: baseline.selector,
              },
            },
          },
        });
        appliedPolicies.current.delete(grantId);
        for (const [resourceId, server] of applied.current) {
          const left = server.grantIds.filter((id) => id !== grantId);
          // A server can be reached through several grants, so it is only off
          // the list once the last of them is gone.
          if (left.length === 0) applied.current.delete(resourceId);
          else applied.current.set(resourceId, { ...server, grantIds: left });
        }
        // Same for the wildcard rows: a retry after a later failure asks for
        // the ones still standing rather than for an id already deleted.
        const wildcard = appliedWildcard.current;
        if (wildcard !== undefined) {
          const left = wildcard.grantIds.filter((id) => id !== grantId);
          appliedWildcard.current =
            left.length === 0 ? undefined : { ...wildcard, grantIds: left };
        }
      }
      // The agent's own page reads its servers from these, and it is where
      // saving leads.
      void invalidateAgentPolicy(
        queryClient,
        organization.id,
        user.id,
        agent.id,
      );
      // Saving is the end of editing: what follows are the platform's own
      // settings, which this flow only describes.
      onDone();
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "Could not save this agent. Try again.",
      );
    } finally {
      setSaving(false);
    }
  };

  // What the reader last decided, marked on the rail. The rail is a growing
  // block of text, and without this it looks the same before and after a
  // click: the mark is how a glance confirms the choice landed.
  const [highlight, setHighlight] = useState<string | null>(null);

  const workload = useWorkloadSetup({
    entry: credential === "workload" ? platform : undefined,
    platformKey: credential === "workload" ? platformKey : null,
    agentName: name.trim(),
    ensureAgent,
    onConnected: setAgentID,
    // The rule this agent already holds: its own rule is not a duplicate of
    // itself, and the create step has nothing left to write.
    connectedSubject: admitted?.subject,
    // Editing never writes a rule: the agent already holds one, and the
    // steps after this are the platform's own settings to read.
    settled: editing,
    // What the rule already says, so editing shows it rather than an empty
    // field that would write a second rule.
    initialValues:
      admitted !== undefined && admittedEntry !== undefined
        ? valuesFromSubject(admittedEntry, admitted.subject)
        : undefined,
    onValueChange: (key) =>
      setHighlight(
        key === "org_id"
          ? "Anthropic organization"
          : key === "name"
            ? "Platform"
            : key === "issuer"
              ? "Issuer"
              : null,
      ),
  });
  // A platform's own setup steps are steps of this wizard, not a wizard of
  // their own: one stepper, one Back and one Continue throughout.
  const stepNames =
    credential === "workload" && workload.steps.length > 0
      ? [
          ...STEPS.slice(0, 3),
          ...workload.steps.map((entry) => ({
            label: entry.title,
            short: entry.short,
          })),
        ]
      : STEPS;
  /** The platform setup step on screen, when one is. */
  const workloadStep =
    credential === "workload" ? workload.steps[step - 3] : undefined;
  // How many steps there are is only known here, once the platform's own have
  // been counted, so an out-of-range link is corrected rather than refused:
  // the URL is rewritten to the last real step instead of leaving the wizard
  // on a step that does not exist.
  const lastStep = stepNames.length - 1;
  // A new agent's draft lives only in this page's memory, so a reload past
  // its first step has nothing to resume: the name, the servers and the key
  // are gone, and a later step would act on an empty draft — issuing a key
  // there tried to create an agent with no name. Start the draft over. One
  // effect with the clamp below, so the two cannot both rewrite the step.
  const restarted = useRef(false);
  useEffect(() => {
    const first = !restarted.current;
    restarted.current = true;
    if (first && !existing && step > defaultStep) {
      setStep(defaultStep);
      return;
    }
    if (step <= lastStep) return;
    setStep(lastStep);
  }, [existing, step, defaultStep, lastStep, setStep]);

  /**
   * Create the agent, discover what it may delegate, issue its first key, and
   * mint the setup command. Each step depends on the one before it, so a
   * failure stops here and says which part did not happen.
   */
  const provision = async () => {
    if (provisioning) return;
    setProvisioning(true);
    setError(null);
    try {
      const target = { id: await ensureAgent() };

      const controller = new AbortController();
      // A key names servers one by one — there is no wildcard in a key, only
      // in the agent's policy. So "all servers" is issued as every server in
      // the agent's reach today, and the agent's own wildcard grant is what
      // carries the ones added after this key was cut.
      //
      // Without this the key asks for nothing: `selected` is empty under
      // "all servers", and discovery rightly answers that nothing can be
      // delegated.
      const reach = allServers
        ? inventory.servers
            .filter((server) =>
              reachCoversProject(allServersProjectIDs, server.projectId),
            )
            // Each of these needs an account attached to the agent before a
            // key can call it, which this flow has no step for.
            .filter((server) => !server.issuerId)
        : selected.map((entry) => entry.server);
      const delegable = await discoverKeyServerGrants(
        sdk.agents,
        target.id,
        reach.map((server) => ({
          resourceId: server.resourceId,
          projectId: server.projectId,
          kind: server.kind,
        })),
        controller.signal,
      );
      const requestedGrants = buildRequestedGrants(
        delegable.map((grant) => ({ grant, narrowing: {} })),
      );
      // Discovery answers per server, so it can come back covering only some
      // of them. A key issued on that answer reaches the servers it covers
      // and silently fails on the rest, while every selected server is still
      // listed as provisioned — so none of it is issued until the gap is
      // named.
      const reachable = new Set(
        delegable.map(
          (grant) =>
            `${grant.selector.projectId ?? ""}:${grant.selector.resourceId}`,
        ),
      );
      const excluded = reach.filter(
        (server) => !reachable.has(`${server.projectId}:${server.resourceId}`),
      );
      if (excluded.length > 0 || requestedGrants.length === 0) {
        // A key is the overlap of three permissions: the agent's, its
        // owner's, and the caller's. The owner's are what a reader never
        // thinks to check — the agent's own page looks right up until the
        // moment its owner loses the server.
        //
        // The caller is almost always the owner, so naming all three as
        // equals would send people looking for a difference that is not
        // there. It is said last, and only as the case where it applies.
        const because =
          "A key cannot give more access than the agent's owner has, so check the agent's permissions and that its owner can still reach them." +
          (agent && agent.ownerUserId !== user.id
            ? " You do not own this agent, so it is limited by your own access too."
            : "");
        throw new Error(
          excluded.length === reach.length
            ? existing
              ? `This key would reach none of these servers. ${because}`
              : `The agent was created, but a key for it would reach none of these servers. ${because}`
            : `This key would not reach ${excluded.map((server) => server.name).join(", ")}. ${because} Deselect ${excluded.length === 1 ? "that server" : "those servers"} to issue a key for the rest.`,
        );
      }

      const expiresAt = new Date(Date.now() + KEY_LIFETIME_DAYS * 86_400_000);
      const issued = await sdk.keys.create({
        createKeyForm: {
          agentId: target.id,
          // Unique per issue, not per day: live key names are unique across
          // the organization, so a date alone makes the second key of the day
          // collide and the issue fail with a 409.
          name: `${name.trim()} key ${new Date().toISOString().replace(/[:.]/g, "-")}`,
          expiresAt,
          delegatedGrantsVersion: 2,
          requestedGrants,
          scopes: [],
        },
      });
      keyID.current = issued.id;
      setSecret(issued.key ?? null);
      setStep(3);
      if (issued.key) regenerate(issued.key, agentGatewayURL(target.id));
    } catch (failure) {
      const conflict =
        failure instanceof GramError && failure.statusCode === 409
          ? failure.message
          : null;
      setError(
        conflict ??
          (failure instanceof Error
            ? failure.message
            : "Could not set this agent up. Try again."),
      );
    } finally {
      setProvisioning(false);
    }
  };

  const summary = (
    <WizardSummary
      step={step + 1}
      stepCount={stepNames.length}
      name={name}
      rows={[
        { label: "Owner", value: user.displayName || user.email },
        {
          label: "Project",
          value: scopeName,
        },
        {
          label: "Grants",
          value: <code className="text-xs">mcp:connect</code>,
        },
        {
          label: "Credential",
          value: credential === "api-key" ? "API key" : "Workload identity",
        },
        // The platform, and what it was pinned to, are decisions like the
        // others: they belong on the rail rather than only on the step that
        // asked for them, which is two screens back by the time they matter.
        ...(credential === "workload" && platformKey !== null
          ? [
              {
                label: "Platform",
                value:
                  platformKey === CUSTOM_PLATFORM_KEY
                    ? (workload.values["name"]?.trim() ?? "") || "Custom"
                    : (platform?.displayName ?? "Custom"),
              },
            ]
          : []),
        ...(credential === "workload" &&
        (workload.values["org_id"] ?? "").trim() !== ""
          ? [
              {
                label: "Anthropic organization",
                value: (
                  <code className="text-xs">
                    {workload.values["org_id"]?.trim()}
                  </code>
                ),
              },
            ]
          : []),
        ...(credential === "workload" &&
        platformKey === CUSTOM_PLATFORM_KEY &&
        (workload.values["issuer"] ?? "").trim() !== ""
          ? [
              {
                label: "Issuer",
                value: (
                  <code className="text-xs break-all">
                    {workload.values["issuer"]?.trim()}
                  </code>
                ),
              },
            ]
          : []),
      ]}
      highlight={highlight}
      // One line rather than every server listed: the rail would otherwise
      // name today's inventory for a choice that is deliberately not a list.
      servers={
        allServers
          ? [{ id: "all", name: "All servers", detail: "and ones added later" }]
          : selected.map((entry) => ({
              id: entry.server.id,
              name: entry.server.name,
              detail: "all tools",
            }))
      }
    />
  );

  const body = () => {
    // Past the credential step, a federated agent is following its platform's
    // own setup, one of its steps per wizard step.
    if (workloadStep !== undefined) {
      return (
        <StepWorkload
          step={workloadStep}
          setup={workload}
          first={workloadStep === workload.steps[0]}
          platformName={platform?.displayName ?? "this platform"}
        />
      );
    }
    switch (step) {
      case 0:
        return (
          <div className="space-y-6">
            <WizardStepHeader
              title="Name"
              description="Every call this agent makes is recorded under this name. Pick one that says whose work it is doing, so an audit log entry or a live session is recognisable without looking the agent up."
            />
            <div className="max-w-xl space-y-2">
              <Label htmlFor="agent-name">Name</Label>
              <Input
                id="agent-name"
                value={name}
                onChange={(next) => {
                  setName(next);
                  setHighlight("Name");
                }}
                placeholder="Release Bot"
                maxLength={NAME_LIMIT}
                // An existing agent's name is its own while this flow is only
                // issuing it a key. Editing is the flow that may change it.
                disabled={existing && !editing}
                autoFocus={!existing}
              />
              <Text muted small>
                {existing
                  ? editing
                    ? "Shown in audit logs and session lists."
                    : "Rename this agent from its own page."
                  : "Shown in audit logs and session lists. You can rename it later."}
              </Text>
            </div>
          </div>
        );
      case 1:
        return (
          <div className="space-y-5">
            <StepServers
              servers={listedServers}
              isLoading={inventory.isLoading}
              isError={inventory.isError}
              onRetry={inventory.refetch}
              scopeLabel={allServersScopeName}
              allServers={allServers}
              onAllServersChange={(next) => {
                setAllServers(next);
                setHighlight(next ? "server:all" : null);
              }}
              selected={selected}
              onChange={(next) => {
                setSelected(next);
                // The one just added, or the last one left when removing.
                const added = next.find(
                  (entry) =>
                    !selected.some((old) => old.server.id === entry.server.id),
                );
                setHighlight(
                  added === undefined ? null : `server:${added.server.id}`,
                );
              }}
            />
          </div>
        );
      case 2:
        return (
          <div className="space-y-6">
            <WizardStepHeader
              title="Credential"
              description="How the agent proves who it is when it connects."
            />
            <RadioCardGroup
              value={credential}
              onValueChange={(next) => {
                setCredential(next as Credential);
                setHighlight("Credential");
              }}
              // Settled for an agent that exists: what it signs in with was
              // decided when it was registered, and changing it here would
              // mean removing a credential rather than editing one.
              disabled={editing}
              className="sm:grid-cols-2"
            >
              <RadioCard value="api-key" title="API key" className="p-3">
                <Text muted small>
                  A key, delivered by a one-line setup script or copied by hand.
                  Works in any runtime.
                </Text>
                <span className="text-muted-foreground mt-1 block font-mono text-xs">
                  Long-lived · revocable
                </span>
              </RadioCard>
              <RadioCard
                value="workload"
                // Offered only to someone who could carry it out: it trusts a
                // platform and admits a subject for the whole organization.
                disabled={!canFederate}
                className="p-3"
                title="Workload identity"
              >
                <Text muted small>
                  Federate a token the platform already issues — GitHub Actions,
                  Kubernetes, SPIFFE, GCP. No Speakeasy secret is created.
                </Text>
                <span className="text-muted-foreground mt-1 block font-mono text-xs">
                  Short-lived tokens · no refresh token
                </span>
                {!canFederate && (
                  <Text muted small className="mt-1 block">
                    Needs permission to manage workload identities.
                  </Text>
                )}
              </RadioCard>
            </RadioCardGroup>
            {credential === "workload" && (
              <WorkloadPlatformPicker
                entries={catalog.entries}
                isPending={catalog.isPending}
                isError={catalog.isError}
                onRetry={catalog.refetch}
                selectedKey={platformKey}
                disabled={editing}
                onSelect={(key) => {
                  setPlatformKey(key);
                  setHighlight("Platform");
                }}
              />
            )}
          </div>
        );
      case 3:
        return (
          <StepInstall
            command={command}
            commandError={commandError}
            minting={minting}
            onRegenerate={() => {
              if (secret && gatewayURL) regenerate(secret, gatewayURL);
            }}
            onIssue={() => void provision()}
            issuing={provisioning || minting}
            secret={secret}
            gatewayURL={gatewayURL}
            serverCount={allServers ? "all" : selected.length}
          />
        );
      default:
        return (
          <StepVerify
            state={verify}
            firstCallAt={firstCallAt}
            gatewayURL={gatewayURL}
          />
        );
    }
  };

  const blocked =
    step === 0
      ? !name.trim()
        ? "Name this agent."
        : null
      : step === 1
        ? !allServers && selected.length === 0
          ? "Select at least one MCP server."
          : // A key names servers one by one, and a server that authorizes
            // each person separately is not one a key can name. So "all
            // servers" with none of them left is a key that would reach
            // nothing — said here rather than after the agent is created.
            allServers &&
              !inventory.isLoading &&
              !inventory.isError &&
              listedServers.every((server) => server.issuerId)
            ? "No server here can be reached with a key yet."
            : null
        : null;

  const footer = () => {
    // Editing writes on the second step. Past it the flow is the ordinary
    // one for an agent that exists: adding a credential it does not have.
    if (editing && step < 2) {
      return (
        <WizardFooter
          note={
            step === 0
              ? "Renaming an agent is recorded in the organization audit log."
              : reconciled
                ? "Changing what an agent reaches is recorded in the organization audit log."
                : inventory.isError
                  ? "The server list could not be loaded, so the servers this agent reaches are not known. Retry the list before saving."
                  : "Reading the servers this agent reaches…"
          }
          onBack={step === 0 ? undefined : () => setStep(step - 1)}
          primary={
            step === 0 ? (
              <Button
                disabled={name.trim().length === 0}
                onClick={() => setStep(1)}
              >
                Continue
              </Button>
            ) : (
              <Button
                // Nothing may be written until the stored grants are in the
                // selection: the diff against an empty one withdraws them all.
                disabled={saving || !reconciled}
                onClick={() => void save()}
              >
                {saving ? "Saving…" : "Save changes and exit"}
              </Button>
            )
          }
        />
      );
    }
    if (workloadStep !== undefined) {
      const last = step - 3 === workload.steps.length - 1;
      const ready = workload.complete(workloadStep);
      return (
        <WizardFooter
          note={
            workloadStep.phase === "create"
              ? "Trusting a platform and allowing its access are recorded in the organization audit log."
              : workloadStep.phase === "connect"
                ? `Nothing here changes Speakeasy; these are ${platform?.displayName ?? "the platform"}'s own settings.`
                : "Nothing is created until the connect step."
          }
          onBack={() => setStep(step - 1)}
          primary={
            workloadStep.phase === "create" ? (
              // Already connected under this very rule: there is nothing to
              // write, so the step is a page to read and move past.
              workload.connected || editing ? (
                <Button onClick={() => setStep(step + 1)}>Continue</Button>
              ) : (
                <Button
                  disabled={
                    workload.connecting || !workload.canConnect || !ready
                  }
                  onClick={() => {
                    void workload.connect().then((connected) => {
                      if (connected) setStep(step + 1);
                    });
                  }}
                >
                  {workload.connecting
                    ? "Connecting…"
                    : `Connect ${platform?.displayName ?? "platform"}`}
                </Button>
              )
            ) : last ? (
              <Button onClick={() => onDone(agentID ?? undefined)}>Done</Button>
            ) : (
              <Button disabled={!ready} onClick={() => setStep(step + 1)}>
                Continue
              </Button>
            )
          }
        />
      );
    }
    switch (step) {
      case 2:
        if (credential === "workload") {
          let note =
            "Choose the platform whose tokens this agent will present.";
          if (platformKey !== null) {
            note =
              workload.steps.length === 0
                ? "Setup is unavailable. Refresh or choose another platform."
                : `${platform?.displayName ?? "Your platform"} issues the tokens; the next step sets it up.`;
          }
          return (
            <WizardFooter
              note={note}
              onBack={() => setStep(1)}
              primary={
                <Button
                  disabled={workload.steps.length === 0}
                  onClick={() => setStep(3)}
                >
                  Continue
                </Button>
              }
            />
          );
        }
        return (
          <WizardFooter
            note={
              existing
                ? "Issuing a key is recorded in the organization audit log."
                : !project.id
                  ? "Reading the project this agent will belong to…"
                  : "Creating the agent is recorded in the organization audit log."
            }
            onBack={() => setStep(1)}
            primary={
              <Button
                // An agent belongs to the project it is made in, so there is
                // nothing to create until that project has resolved.
                disabled={provisioning || (!existing && !project.id)}
                onClick={() => void provision()}
              >
                {provisioning
                  ? existing
                    ? "Issuing…"
                    : "Creating…"
                  : existing
                    ? "Issue key"
                    : "Create agent"}
              </Button>
            }
          />
        );
      case 3:
        return (
          <WizardFooter
            note="The API key can be rotated or revoked from the agent page."
            primary={
              <Button onClick={() => setStep(4)}>
                Continue to verification
              </Button>
            }
          />
        );
      case 4:
        return (
          <WizardFooter
            note={
              verify === "connected"
                ? "The agent has called the gateway."
                : "Run the setup from the previous step, then wait for the first call."
            }
            onBack={() => setStep(3)}
            primary={
              <Button onClick={() => onDone(agentID ?? undefined)}>
                {verify === "connected" ? "Done" : "Skip for now"}
              </Button>
            }
          />
        );
      default:
        return (
          <WizardFooter
            note={
              blocked ??
              (step === 0
                ? existing
                  ? "This agent's identity is settled; this flow issues it a key."
                  : "You can rename it later."
                : allServers
                  ? // Not a count: the choice is deliberately not a list, and
                    // naming today's would be a number that changes behind it.
                    "All servers selected."
                  : `${selected.length} of ${listedServers.length} servers selected.`)
            }
            onBack={step > 0 ? () => setStep(step - 1) : undefined}
            primary={
              <Button disabled={!!blocked} onClick={() => setStep(step + 1)}>
                Continue
              </Button>
            }
          />
        );
    }
  };

  return (
    <div className="space-y-6">
      <WizardStepper
        steps={stepNames}
        current={step}
        // While creating, only a step already completed is a destination: the
        // ones ahead are built from choices not yet made. Provisioning an
        // existing agent has no such order, so every step is reachable until
        // the key is issued.
        // Editing has settled everything a credential step would ask.
        settledFrom={editing ? 2 : undefined}
        forward={existing && !secret}
        // Verify watches a key being used, so it has nothing to watch until
        // one is issued. Without this an existing agent could jump straight
        // to it and finish the flow having provisioned nothing.
        onJump={(next) =>
          setStep(
            secret || credential === "workload" ? next : Math.min(next, 3),
          )
        }
      />
      {error && (
        <p
          role="alert"
          className="border-destructive text-destructive border p-3 text-sm"
        >
          {error}
        </p>
      )}
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_260px] lg:gap-16">
        <div>{body()}</div>
        {/* The cell stretches to the row's height on purpose: a sticky child
            can only travel inside its own containing block, so a cell shrunk
            to the rail's height gives it nowhere to go and it scrolls away
            with the page. */}
        <div className="lg:order-last">{summary}</div>
      </div>
      {footer()}
    </div>
  );
}
