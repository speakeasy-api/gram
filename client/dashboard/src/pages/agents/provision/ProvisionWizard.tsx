import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Checkbox } from "@/components/ui/Checkbox";
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
import { agentGatewayURL, mintInstallCommand } from "./gateway";
import { StepProvision } from "./StepProvision";
import { StepServers, type ServerSelection } from "./StepServers";
import { StepVerify, type VerifyState } from "./StepVerify";
import { StepWorkload, WorkloadPlatformPicker } from "./StepWorkload";
import { useWorkloadSetup } from "./useWorkloadSetup";
import { CUSTOM_PLATFORM_KEY } from "./CustomSetupInstructions";
import {
  CONNECT_SCOPE,
  grantChanges,
  grantedServers,
  wildcardConnectGrant,
  type GrantedServer,
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
import {
  DEFAULT_TOKEN_LIFETIME_SECONDS,
  DEVICE_SYNC_SCOPE,
  HEADLESS_DEVICE_KEY,
  HOOKS_INGEST_SCOPE,
  deviceEnrollmentPolicyGrants,
  isDeviceAdmission,
  tokenLifetimeLabel,
  type WatchedToken,
} from "../devices/device-enrollment";
import { DeviceEnrollmentFields } from "../devices/DeviceEnrollmentFields";
import { useHooksProjects } from "../devices/useHooksProjects";
import { DeviceSetupPanel } from "../devices/DeviceSetupPanel";
import { DeviceVerify } from "../devices/DeviceVerify";

/**
 * Creating an agent identity here is only ever a means to provisioning one
 * somewhere else, so the flow runs to that end: name it, choose what it may
 * reach, pick how it proves who it is, hand the runtime its credential, and
 * watch for the first call.
 *
 * The identity is created at the end of the choices, not the start: everything
 * before Provision is still a draft the user can abandon without leaving a
 * half-made agent behind.
 */

/** Each step's name, and the one or two words it shrinks to in a full row. */
const STEPS = [
  { label: "Name & scope", short: "Name" },
  { label: "Server selection", short: "Servers" },
  { label: "Credential", short: "Credential" },
  { label: "Provision", short: "Provision" },
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

type Scope = "project" | "organization";

export function ProvisionWizard({
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
  // resolves. A useState initializer would run once, before useProject has an
  // id, and strand the form on organization scope.
  const [chosenScope, setChosenScope] = useState<Scope | null>(null);
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
  // A headless device enrolls with a bootstrap token rather than a platform's
  // tokens, so it has no setup steps of its own: the wizard's own Provision
  // and Verify steps hand over the token and watch for it to be redeemed.
  const device =
    credential === "workload" && platformKey === HEADLESS_DEVICE_KEY;
  const hooksProjects = useHooksProjects();
  const [chosenHooksProjectId, setHooksProjectId] = useState<string | null>(
    null,
  );
  // Follows the browsed project until the reader picks one, for the same
  // reason the scope does: the project resolves after the first render.
  const hooksProjectId = chosenHooksProjectId ?? project.id ?? "";
  const hooksProject = hooksProjects.find(
    (candidate) => candidate.id === hooksProjectId,
  );
  const [acceptHooks, setAcceptHooks] = useState(true);
  const [tokenLifetime, setTokenLifetime] = useState(
    DEFAULT_TOKEN_LIFETIME_SECONDS,
  );
  // The bootstrap tokens this flow created, which Verify waits on.
  const [watched, setWatched] = useState<WatchedToken[]>([]);
  const watchToken = useCallback(
    (token: WatchedToken) => setWatched((current) => [...current, token]),
    [],
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
    setPlatformKey(
      isDeviceAdmission(admitted)
        ? HEADLESS_DEVICE_KEY
        : (admittedEntry?.key ?? CUSTOM_PLATFORM_KEY),
    );
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
  useEffect(() => {
    if (reconciled || grants === undefined) return;
    if (inventory.isLoading || inventory.isError) return;
    const granted = grantedServers(grants);
    applied.current = granted;
    setSelected(
      inventory.servers
        .filter((server) => granted.has(server.resourceId))
        .map((server) => ({ server })),
    );
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

  // Project leads once one is available; an explicit pick always wins. Until
  // a project resolves, project scope would send an empty id, which the server
  // reads as "omitted" — an organization-wide agent created on behalf of
  // someone who asked for a project one.
  // An existing agent's scope is its own and is stored on it: the project
  // being browsed is not necessarily the one it was bound to, and an
  // organization-wide agent holds grants in projects this one is not.
  const scope: Scope = existing
    ? agent.projectId
      ? "project"
      : "organization"
    : (chosenScope ?? (project.id ? "project" : "organization"));
  // The project the servers on offer belong to, and the binding a new agent
  // is created with. Undefined reaches every project in the organization.
  const scopedProjectID = existing
    ? agent.projectId
    : scope === "project"
      ? project.id
      : undefined;
  // Falls back to the id, not the browsed project: an agent bound to a project
  // the caller cannot see would otherwise be labelled with the wrong one.
  const projectName = (id: string): string =>
    (organization.projects ?? []).find((candidate) => candidate.id === id)
      ?.name ?? id;
  const scopeName = scopedProjectID
    ? projectName(scopedProjectID)
    : organization.name;

  const gatewayURL = agentID ? agentGatewayURL(agentID) : "";
  const platform = catalog.entries.find((entry) => entry.key === platformKey);

  // The verify step reads the key itself: a key with an access time is a
  // runtime that reached the gateway and was admitted.
  useEffect(() => {
    if (step !== 4 || !agentID || verify === "connected" || device) return;
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
  }, [step, agentID, verify, sdk, verifyPollMs, device]);

  const regenerate = (key: string, url: string) => {
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
    const draft: AgentPolicyDraft = {
      "mcp:connect": selected.map((entry) => ({
        resourceKind: "mcp" as const,
        resourceId: entry.server.resourceId,
        projectId: entry.server.projectId,
      })),
    };
    const created = await sdk.agents.create({
      createAgentForm: {
        name: name.trim(),
        ...(scopedProjectID ? { projectId: scopedProjectID } : {}),
        policyGrants: [
          ...agentPolicyGrantsFromDraft(draft),
          // A device polls its policy with device sync, so the token is
          // refused without it. Hooks only for the project chosen, and only
          // when asked for.
          ...(device
            ? deviceEnrollmentPolicyGrants({
                organizationId: organization.id,
                hooksProjectId: acceptHooks ? hooksProjectId : undefined,
              })
            : []),
        ],
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
      const changes = grantChanges(
        new Map(
          [...applied.current].filter(([resourceId]) =>
            offered.has(resourceId),
          ),
        ),
        selected.map((entry) => ({
          resourceId: entry.server.resourceId,
          projectId: entry.server.projectId,
        })),
      );
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
        applied.current.set(server.resourceId, {
          grantIds: [created.id],
          resourceId: server.resourceId,
          projectId: server.projectId,
        });
      }
      for (const grantId of changes.remove) {
        await deleteGrant.mutateAsync({
          request: { agentPolicyGrantIDForm: { agentId: agent.id, grantId } },
        });
        for (const [resourceId, server] of applied.current) {
          const left = server.grantIds.filter((id) => id !== grantId);
          // A server can be reached through several grants, so it is only off
          // the list once the last of them is gone.
          if (left.length === 0) applied.current.delete(resourceId);
          else applied.current.set(resourceId, { ...server, grantIds: left });
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
    platformKey: credential === "workload" && !device ? platformKey : null,
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
  useEffect(() => {
    if (step <= lastStep) return;
    setStep(lastStep);
  }, [step, lastStep, setStep]);

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
      const delegable = await discoverKeyServerGrants(
        sdk.agents,
        target.id,
        selected.map((entry) => ({
          resourceId: entry.server.resourceId,
          projectId: entry.server.projectId,
          kind: entry.server.kind,
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
      const excluded = selected.filter(
        (entry) =>
          !reachable.has(
            `${entry.server.projectId}:${entry.server.resourceId}`,
          ),
      );
      if (excluded.length > 0 || requestedGrants.length === 0) {
        throw new Error(
          excluded.length === selected.length
            ? existing
              ? "Nothing on these servers can be delegated to this agent. Check its permissions, or choose other servers."
              : "The agent was created, but nothing on these servers could be delegated to it. Open the agent to review its permissions."
            : `Nothing on ${excluded.map((entry) => entry.server.name).join(", ")} can be delegated to this agent, so a key would not reach ${excluded.length === 1 ? "it" : "them"}. Deselect ${excluded.length === 1 ? "that server" : "those servers"}, or check the agent's permissions.`,
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
            : "Could not provision this agent. Try again."),
      );
    } finally {
      setProvisioning(false);
    }
  };

  /**
   * Create the agent a headless device will enroll as. Its bootstrap token is
   * created on the next step, where it is shown, so nothing secret exists
   * until the reader is looking at it.
   */
  const provisionDevice = async () => {
    if (provisioning) return;
    setProvisioning(true);
    setError(null);
    try {
      await ensureAgent();
      setStep(3);
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "Could not create this agent. Try again.",
      );
    } finally {
      setProvisioning(false);
    }
  };

  const grantScopes = [
    "mcp:connect",
    ...(device ? [DEVICE_SYNC_SCOPE] : []),
    ...(device && acceptHooks ? [HOOKS_INGEST_SCOPE] : []),
  ];

  const summary = (
    <WizardSummary
      step={step + 1}
      stepCount={stepNames.length}
      name={name}
      rows={[
        { label: "Owner", value: user.displayName || user.email },
        {
          label: "Scope",
          value: scopeName,
        },
        {
          label: "Grants",
          value: (
            <span className="flex flex-col">
              {grantScopes.map((scope) => (
                <code key={scope} className="text-xs break-all">
                  {scope}
                </code>
              ))}
            </span>
          ),
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
                value: device
                  ? "Headless device"
                  : platformKey === CUSTOM_PLATFORM_KEY
                    ? (workload.values["name"]?.trim() ?? "") || "Custom"
                    : (platform?.displayName ?? "Custom"),
              },
            ]
          : []),
        ...(device
          ? [
              {
                label: "Hooks project",
                value: hooksProject?.name ?? "None chosen",
              },
              {
                label: "Bootstrap token",
                value: `Expires after ${tokenLifetimeLabel(tokenLifetime)}`,
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
      servers={selected.map((entry) => ({
        id: entry.server.id,
        name: entry.server.name,
        detail: "all tools",
      }))}
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
              title="Name and scope"
              description="What this identity is called, and how far it may reach."
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
            {/* Folded away: almost every agent is scoped to the project it
                is created in, and the choice is undone by the server list on
                the next step anyway. */}
            <details className="group">
              <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-sm">
                Advanced settings
              </summary>
              <div className="bg-muted/40 border-muted-foreground/30 mt-2 space-y-2 border p-4">
                <Label>Scope</Label>
                <Text muted small>
                  {existing
                    ? "Set when the agent was created. Which of its servers this key reaches is the next step."
                    : "Scope decides whether this agent can reach servers in every project or only this one. You narrow the list itself in the next step."}
                </Text>
                <RadioCardGroup
                  value={scope}
                  onValueChange={(value) => {
                    const next = value as Scope;
                    setChosenScope(next);
                    // Narrowing to the project takes the other projects'
                    // servers off the next step. A selection made before the
                    // switch would otherwise stay in the draft and be granted
                    // without ever being shown again.
                    if (next === "project")
                      setSelected((current) =>
                        current.filter(
                          (entry) => entry.server.projectId === project.id,
                        ),
                      );
                  }}
                  disabled={existing}
                  // The density the design system defines for a choice that
                  // is one field of a step rather than a section of its own.
                  size="sm"
                  className="sm:grid-cols-2"
                >
                  {/* One line each: the choice is a scope, not a paragraph. */}
                  <RadioCard
                    value="project"
                    title="Project"
                    // Offered only once there is a project to bind to.
                    disabled={!project.id}
                  >
                    <Text muted small>
                      Servers in {project.name}.
                    </Text>
                  </RadioCard>
                  <RadioCard value="organization" title="Organization">
                    <Text muted small>
                      Servers in every {organization.name} project.
                    </Text>
                  </RadioCard>
                </RadioCardGroup>
              </div>
            </details>
          </div>
        );
      case 1:
        return (
          <div className="space-y-5">
            {/* A wildcard grant is not a server anyone picked, so the list
                below leaves it out — and a save writes only the servers that
                changed, so it stays. Without this the reader would read an
                unticked list as the agent's whole reach. */}
            {wildcardReach && (
              <Alert variant="warning" alignTop>
                <span>
                  {wildcardReach.projectId
                    ? `This agent reaches every MCP server in ${projectName(wildcardReach.projectId)} through a wildcard grant, which the list below does not show.`
                    : "This agent reaches every MCP server through a wildcard grant, which the list below does not show."}{" "}
                  Saving a narrower list leaves that grant in place, so the
                  agent keeps reaching all of them. Remove it from the
                  agent&rsquo;s permissions first.
                </span>
              </Alert>
            )}
            <StepServers
              servers={inventory.servers.filter(
                (server) =>
                  scopedProjectID === undefined ||
                  server.projectId === scopedProjectID,
              )}
              isLoading={inventory.isLoading}
              isError={inventory.isError}
              onRetry={inventory.refetch}
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
                  A scoped key, delivered by a one-line setup script or copied
                  by hand. Works in any runtime.
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
                  Federate a token the platform already issues, or enroll a
                  headless box with a single-use bootstrap token. No long-lived
                  Speakeasy secret is created.
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
                // Enrolling a device admits a subject for the organization,
                // which is the same write as trusting a platform.
                showHeadlessDevice={canFederate}
              />
            )}
            {device && (
              <DeviceEnrollmentFields
                projectId={hooksProjectId}
                onProjectChange={(next) => {
                  setHooksProjectId(next);
                  setHighlight("Hooks project");
                }}
                lifetimeSeconds={tokenLifetime}
                onLifetimeChange={(next) => {
                  setTokenLifetime(next);
                  setHighlight("Bootstrap token");
                }}
                hooksNote={
                  <div className="space-y-2 pt-1">
                    <label className="flex items-start gap-2 text-sm">
                      <Checkbox
                        checked={acceptHooks}
                        onCheckedChange={(next) => {
                          setAcceptHooks(next === true);
                          setHighlight("Grants");
                        }}
                        aria-label="Accept hooks from this project"
                        className="mt-0.5"
                      />
                      <span>
                        Allow this agent to send hooks to{" "}
                        {hooksProject?.name ?? "this project"}
                      </span>
                    </label>
                    {!acceptHooks && (
                      <Text muted small>
                        The box still enrolls, but its hook events are refused
                        until the agent is allowed to send them.
                      </Text>
                    )}
                  </div>
                }
              />
            )}
          </div>
        );
      case 3:
        if (device) {
          if (!agentID || !hooksProject) {
            return (
              <Text muted>Create the agent on the credential step first.</Text>
            );
          }
          return (
            <div className="space-y-5">
              <WizardStepHeader
                title="Enroll the box"
                description="Run the install on the box this agent runs on."
              />
              <DeviceSetupPanel
                agentId={agentID}
                hooksProject={hooksProject}
                lifetimeSeconds={tokenLifetime}
                onTokenCreated={watchToken}
              />
            </div>
          );
        }
        return (
          <StepProvision
            command={command}
            commandError={commandError}
            minting={minting}
            onRegenerate={() => {
              if (secret && gatewayURL) regenerate(secret, gatewayURL);
            }}
            secret={secret}
            gatewayURL={gatewayURL}
            serverCount={selected.length}
          />
        );
      default:
        if (device) {
          if (!agentID) {
            return (
              <Text muted>Create the agent on the credential step first.</Text>
            );
          }
          return (
            <DeviceVerify
              agentId={agentID}
              watched={watched}
              pollMs={verifyPollMs}
            />
          );
        }
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
        ? selected.length === 0
          ? "Select at least one MCP server."
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
        if (device) {
          return (
            <WizardFooter
              note={
                hooksProject === undefined
                  ? "Choose the project this box sends hooks to."
                  : "Creating the agent is recorded in the organization audit log."
              }
              onBack={() => setStep(1)}
              primary={
                <Button
                  disabled={provisioning || hooksProject === undefined}
                  onClick={() => void provisionDevice()}
                >
                  {provisioning ? "Creating…" : "Create agent"}
                </Button>
              }
            />
          );
        }
        if (credential === "workload") {
          return (
            <WizardFooter
              note={
                // The custom flow has no catalog entry, so a chosen platform
                // can still leave `platform` undefined. Asking the key keeps
                // the note from telling the reader to choose again.
                platformKey === null
                  ? "Choose the platform whose tokens this agent will present."
                  : `${platform?.displayName ?? "Your platform"} issues the tokens; the next step sets it up.`
              }
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
                : "Creating the agent is recorded in the organization audit log."
            }
            onBack={() => setStep(1)}
            primary={
              <Button disabled={provisioning} onClick={() => void provision()}>
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
            note={
              device
                ? "Bootstrap tokens can be revoked from the agent page."
                : "The API key can be rotated or revoked from the agent page."
            }
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
              device
                ? "Enrolled devices are listed on the agent page."
                : verify === "connected"
                  ? "The agent has called the gateway."
                  : "Run the setup from the previous step, then wait for the first call."
            }
            onBack={() => setStep(3)}
            primary={
              <Button onClick={() => onDone(agentID ?? undefined)}>
                {verify === "connected" || device ? "Done" : "Skip for now"}
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
                  : "Name and scope can be changed later."
                : `${selected.length} of ${inventory.servers.length} servers selected.`)
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
