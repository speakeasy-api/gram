import { Alert } from "@/components/ui/Alert";
import { useOrganization, useProject } from "@/contexts/Auth";
import { WorkloadIssuerSelector } from "./WorkloadIssuerSelector";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { AssertionReuseField } from "../WorkloadIssuerPolicy";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { useAdmitWorkloadSubjectMutation } from "@gram/client/react-query/admitWorkloadSubject.js";
import { useRegisterWorkloadIssuerMutation } from "@gram/client/react-query/registerWorkloadIssuer.js";
import { useWithdrawWorkloadSubjectMutation } from "@gram/client/react-query/withdrawWorkloadSubject.js";
import {
  invalidateAllWorkloadIdentities,
  queryKeyWorkloadIdentities,
  useWorkloadIdentities,
} from "@gram/client/react-query/workloadIdentities.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  customSetupInstructions,
  CUSTOM_PLATFORM_KEY,
} from "./CustomSetupInstructions";
import { platformSetup } from "./platformSetups";
import { ComputedStatus } from "./SetupFields";
import type {
  PlatformSetup,
  SetupContext,
  SetupServer,
  SetupStep,
} from "./setupInstructions";
import {
  connectedIssuer,
  duplicateRuleMessage,
  existingRule,
  type CatalogEntry,
  type VariableValues,
} from "./workloadCatalog";
import { useSetupValues, type SetupValues } from "./workloadTokenEndpoints";

/**
 * The state behind a platform's guided setup, run for the agent this wizard
 * is provisioning.
 *
 * Each of the platform's steps becomes a step of this wizard, so there is a
 * single stepper and a single Back and Continue throughout. The state lives
 * here rather than in a step because every step is a separate render, and
 * what one collects the next reads.
 */

export interface WorkloadSetup {
  exchange: SetupValues;
  /** The platform's steps, each of which is a step of the wizard. */
  steps: SetupStep[];
  /**
   * What the platform demands before any of this works, shown as a disclosure
   * on its first step. Null when it demands nothing.
   */
  requirements: ReactNode | null;
  /** One step's body, with the wizard's own answers already filled in. */
  render: (step: SetupStep) => ReactNode;
  /** Whether a step has everything it asks for. */
  complete: (step: SetupStep) => boolean;
  /** Trusts the platform and admits the subject; false if either failed. */
  connect: () => Promise<boolean>;
  connecting: boolean;
  /** Whether the values collected so far make a rule that can be saved. */
  canConnect: boolean;
  /** Why connecting failed, or null. */
  error: string | null;
  /** Whether the rule on screen is one this agent already holds. */
  connected: boolean;
  /** What the reader has typed, for the summary rail to name. */
  values: Record<string, string>;
}

export function useWorkloadSetup({
  entry,
  platformKey,
  agentName,
  ensureAgent,
  onConnected,
  onValueChange,
  initialValues,
  replacing,
  readOnly = false,
  setupOverride,
  targetProjectId,
  servers = [],
}: {
  /** The servers the agent may call, for steps that name one to call. */
  servers?: SetupServer[];
  /** Reuses issuer registration and admission for non-MCP setup. */
  setupOverride?: PlatformSetup;
  /** An inference exchange cannot use another project’s issuer. */
  targetProjectId?: string;
  /** The catalog's policy for the platform, absent for a custom one. */
  entry: CatalogEntry | undefined;
  /** The chosen platform, which may be the custom one. Null until chosen. */
  platformKey: string | null;
  /** The agent the access rule will name, by the name the wizard holds. */
  agentName: string;
  /** Creates the agent if it does not exist yet, and returns its id. */
  ensureAgent: () => Promise<string>;
  /** The platform is trusted and the rule saved, against this agent. */
  onConnected: (agentID: string) => void;
  /** What the rule already says, when one is being edited rather than made. */
  initialValues?: VariableValues;
  /**
   * The rule this agent already holds, when it is being edited. Left as it
   * is, the create step has nothing to write. Changed, the create step admits
   * the new rule for the same agent and then withdraws this one: the API
   * fixes a rule's issuer and subject at admission, so a change is a
   * replacement.
   */
  replacing?: WorkloadAdmission;
  /**
   * The reader may look at the setup but not change it, such as someone
   * editing an agent without permission to manage workload identities.
   */
  readOnly?: boolean;
  /** A value was typed, so the rail can mark what just changed. */
  onValueChange?: (key: string) => void;
}): WorkloadSetup {
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const project = useProject();
  const identityRequest = { gramProject: project.slug };
  const identities = useWorkloadIdentities(identityRequest, undefined, {
    throwOnError: false,
  });
  const issuers = (identities.data?.issuers ?? []).filter(
    (issuer) =>
      issuer.organizationId === organization.id &&
      (issuer.projectId === "" || issuer.projectId === project.id),
  );
  const admissions = identities.data?.admissions ?? [];

  const [values, setValues] = useState<VariableValues>(initialValues ?? {});
  // The rule and the catalog both land after the first render, so what the
  // rule already says is filled in when it arrives — once, and only while
  // the reader has typed nothing of their own.
  const adopted = useRef(false);
  useEffect(() => {
    if (adopted.current || initialValues === undefined) return;
    if (Object.keys(initialValues).length === 0) return;
    if (Object.keys(values).length > 0) return;
    adopted.current = true;
    setValues(initialValues);
  }, [initialValues, values]);
  const [connecting, setConnecting] = useState(false);
  const [reuseIssuer, setReuseIssuer] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const setupValues = useSetupValues(targetProjectId);

  // The issuer this flow has already trusted, so a retry after a later failure
  // does not try to trust it a second time. The URL rather than a flag: a
  // custom platform's issuer can still be edited afterwards, and registering
  // the new one is exactly what the retry then has to do.
  const [trustedIssuer, setTrustedIssuer] = useState<string | null>(null);
  // The rule this flow saved. Once there is one it, not the prop, is what
  // the agent holds: the refetched list may name any of the agent's rules
  // first while a superseded one is still waiting to be withdrawn.
  const [saved, setSaved] = useState<HeldRule | null>(null);
  // Rules this flow superseded but has not withdrawn yet, so a retry after a
  // failed withdrawal finishes the job, and a further change does not lose
  // track of the rule it replaces.
  const [stale, setStale] = useState<string[]>([]);

  const registerIssuer = useRegisterWorkloadIssuerMutation();
  const admitSubject = useAdmitWorkloadSubjectMutation();
  const withdrawSubject = useWithdrawWorkloadSubjectMutation();

  // The catalog's platforms come from the same map the picker offers tiles
  // from, plus the custom flow for everything the catalog does not know.
  const setup =
    setupOverride ??
    (platformKey === CUSTOM_PLATFORM_KEY
      ? customSetupInstructions()
      : entry === undefined
        ? undefined
        : platformSetup(entry));

  const customFlow = platformKey === CUSTOM_PLATFORM_KEY;
  const selectedIssuer = issuers.find((issuer) =>
    values.issuer_id
      ? issuer.id === values.issuer_id
      : issuer.issuer === values.issuer,
  );
  const resolvedValues =
    selectedIssuer && customFlow
      ? {
          ...values,
          name: selectedIssuer.name,
          issuer: selectedIssuer.issuer,
          jwks_uri: selectedIssuer.jwksUri,
        }
      : values;
  const connection =
    customFlow &&
    (!selectedIssuer || identities.isPending || identities.isError)
      ? null
      : (setup?.connection(resolvedValues) ?? null);
  const selectIssuer = (issuer: WorkloadIssuer) => {
    setValues((current) => ({
      ...current,
      issuer_id: issuer.id,
      name: issuer.name,
      issuer: issuer.issuer,
      jwks_uri: issuer.jwksUri,
    }));
    onValueChange?.("issuer");
  };
  const platformName = entry?.displayName ?? "this platform";

  // The rule the agent holds as far as this flow knows: the one it saved,
  // or else the one being edited. The prop is read rather than seeded into
  // state because the admission arrives a render or two after the first.
  const current: HeldRule | null = saved ?? replacing ?? null;
  // The rule this flow already holds is not a clash with itself.
  const ours =
    connection !== null && current !== null && sameRule(connection, current);
  // Nothing to write: the values on screen are the rule the agent holds, and
  // nothing it replaced is still waiting to be withdrawn.
  const unchanged = ours && stale.length === 0;
  // A replacement keeps the replaced rule's tier: a rule admitted for one
  // project stays with that project rather than widening to the
  // organization, whatever tier its issuer was registered at.
  const keptProjectSlug = current?.projectId
    ? organization.projects.find((p) => p.id === current.projectId)?.slug
    : undefined;
  const tier = customFlow
    ? {
        gramProject: keptProjectSlug ?? project.slug,
        projectScoped: !!selectedIssuer?.projectId || !!current?.projectId,
      }
    : current === null
      ? undefined
      : keptProjectSlug !== undefined
        ? { gramProject: keptProjectSlug, projectScoped: true }
        : { projectScoped: false };
  // A catalog platform's new rule is written at the organization tier, so
  // only an organization-tier issuer counts as already trusting it there. A
  // project's own row for the same URL would skip the registration that
  // admission needs, and the admission would then find no issuer at its tier.
  // A replacement that keeps a project's tier may use that project's issuer.
  const organizationTier = !customFlow && !tier?.projectScoped;
  const tierIssuers = organizationTier
    ? issuers.filter((issuer) => issuer.projectId === "")
    : issuers;
  const isTrusted =
    connection !== null &&
    (connection.issuer === trustedIssuer ||
      connectedIssuer(connection.issuer, tierIssuers) !== undefined);
  // A project that trusts the same issuer on its own takes precedence in that
  // project: its MCP servers match a token against the project's rules, not
  // the organization-tier one this flow saves, so a job reaches them as a
  // different identity or not at all.
  const shadowingIssuer =
    !organizationTier || connection === null
      ? undefined
      : issuers.find(
          (issuer) =>
            issuer.projectId !== "" && issuer.issuer === connection.issuer,
        );
  const conflicting =
    connection === null || ours
      ? undefined
      : existingRule(
          connection.issuer,
          admissions,
          connection,
          customFlow ? selectedIssuer?.projectId : undefined,
        );
  const ruleConflict =
    conflicting === undefined
      ? null
      : duplicateRuleMessage(entry, platformName, conflicting.agentName);

  /**
   * Create the agent, trust the platform, and admit the subject against it.
   * The agent comes first because the access rule names it: a rule saved
   * without one is refused at the token endpoint. When a rule is being
   * replaced, the old one is withdrawn only after the new one is saved, so
   * the agent is never left without one.
   */
  const connect = async (): Promise<boolean> => {
    if (connection === null || connecting || readOnly) return false;
    if (unchanged) return true;
    if (current?.projectId && keptProjectSlug === undefined) {
      setError(
        "This access rule belongs to a project you cannot open. Ask an organization admin to change it.",
      );
      return false;
    }
    // A clash the list already shows is refused by the server anyway, and
    // going ahead would create the agent first and strand it behind a failure
    // the step had been naming all along.
    if (ruleConflict !== null) {
      setError(ruleConflict);
      return false;
    }
    setConnecting(true);
    setError(null);
    // Registering the issuer conflicts on a duplicate name, admitting the rule
    // on a duplicate rule; only the second means the values are taken.
    let admitting = false;
    let withdrawing = false;
    let pending = stale;
    try {
      const agentID = await ensureAgent();
      if (!isTrusted) {
        await registerIssuer.mutateAsync({
          request: {
            registerWorkloadIssuerForm: {
              name: connection.name,
              description: connection.description,
              issuer: connection.issuer,
              jwksUri: connection.jwksUri,
              allowAssertionReuse: reuseIssuer === connection.issuer,
              tags: [],
            },
          },
        });
        setTrustedIssuer(connection.issuer);
      }
      if (!ours) {
        admitting = true;
        const policy = await admitSubject.mutateAsync({
          request: {
            ...(tier?.gramProject ? { gramProject: tier.gramProject } : {}),
            admitWorkloadSubjectForm: {
              ...(tier ? { projectScoped: tier.projectScoped } : {}),
              issuer: connection.issuer,
              subject: connection.subject,
              matchKind: connection.matchKind,
              agentId: agentID,
              // A replacement keeps the label and tags the old rule carried.
              ...(current?.name ? { name: current.name } : {}),
              tags: current?.tags ?? [],
            },
          },
        });
        admitting = false;
        // The rule the new one supersedes, withdrawn below. Its id comes from
        // the prop or from the policy the last save returned.
        if (current?.id) pending = [...pending, current.id];
        setStale(pending);
        const created = policy?.admissions?.find(
          (admission) =>
            admission.agentId === agentID && sameRule(connection, admission),
        );
        setSaved(
          created ?? {
            id: "",
            issuer: connection.issuer,
            subject: connection.subject,
            matchKind: connection.matchKind,
            name: current?.name ?? "",
            tags: current?.tags ?? [],
            projectId: tier?.projectScoped ? (current?.projectId ?? "") : "",
          },
        );
      }
      for (const id of pending) {
        withdrawing = true;
        await withdrawSubject.mutateAsync({ request: { id } });
        pending = pending.filter((other) => other !== id);
        setStale(pending);
      }
      onConnected(agentID);
      return true;
    } catch (failure) {
      setError(
        withdrawing
          ? "The new access rule was saved, but the old one could not be removed. Try again."
          : connectFailureMessage(entry, platformName, failure, admitting),
      );
      return false;
    } finally {
      // Refetched on failure too: the platform may have been trusted before
      // the access rule was refused, and a retry must then only add the rule.
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setConnecting(false);
    }
  };

  const context: SetupContext = {
    values: resolvedValues,
    setValue: (key: string, value: string) => {
      setValues((current) => ({ ...current, [key]: value }));
      onValueChange?.(key);
    },
    agentName,
    computed: setupValues.values ?? {},
    servers,
    // Folded away when it has nothing to say, which is almost always. When it
    // does, the values above it are blank and this is the reason, so it is
    // shown where that can be read rather than behind a disclosure.
    issuerPicker:
      setupValues.unavailableReason !== null ? (
        <ComputedStatus setupValues={setupValues} />
      ) : (
        <details className="pt-2">
          <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-sm">
            Advanced
          </summary>
          <div className="bg-muted/40 border-muted-foreground/30 mt-2 border p-4">
            <ComputedStatus setupValues={setupValues} />
          </div>
        </details>
      ),
    ruleConflict,
  };

  return {
    exchange: setupValues,
    steps: setup?.steps ?? [],
    requirements: setup?.requirements ?? null,
    // A disabled fieldset disables every control inside it, the catalog's
    // fields and the issuer picker alike, so a read-only reader cannot type
    // a change that Continue would then drop.
    render: (step) => (
      <fieldset disabled={readOnly} className="m-0 min-w-0 border-0 p-0">
        {customFlow && step.id === "issuer" ? (
          <WorkloadIssuerSelector
            key={`${organization.id}:${project.id}`}
            issuers={issuers}
            selectedId={selectedIssuer?.id ?? ""}
            loading={identities.isPending}
            failed={identities.isError}
            onRetry={() => {
              void identities.refetch();
            }}
            onSelect={selectIssuer}
            issuerStep={step}
            onCreated={(policy, issuer) => {
              queryClient.setQueryData(
                queryKeyWorkloadIdentities(identityRequest),
                policy,
              );
              selectIssuer(issuer);
              // An organization-tier create response may omit project-tier rows.
              if (!issuer.projectId) {
                void queryClient.invalidateQueries({
                  queryKey: queryKeyWorkloadIdentities(identityRequest),
                });
              }
            }}
          />
        ) : (
          <div className="space-y-4">
            {step.body(context)}
            {step.phase === "create" && shadowingIssuer !== undefined && (
              <Alert variant="warning">
                <div className="text-sm">
                  This project also trusts{" "}
                  <strong>{shadowingIssuer.name}</strong> on its own, at the
                  same issuer URL. Its MCP servers check a job against that
                  issuer&apos;s rules instead of the one saved here, so the job
                  reaches them as another identity, or not at all. Withdraw that
                  issuer from the Federated access section of an agent that uses
                  it, so the rule saved here applies in this project too.
                </div>
              </Alert>
            )}
            {step.phase === "create" && !isTrusted && (
              <AssertionReuseField
                checked={
                  connection !== null && reuseIssuer === connection.issuer
                }
                disabled={connecting || connection === null}
                onChange={(checked) =>
                  setReuseIssuer(checked ? (connection?.issuer ?? null) : null)
                }
              />
            )}
          </div>
        )}
      </fieldset>
    ),
    complete: (step) =>
      customFlow && step.id === "issuer"
        ? !!selectedIssuer && !identities.isPending && !identities.isError
        : (step.complete?.(context) ?? true),
    connect,
    connecting,
    canConnect: connection !== null,
    connected: unchanged,
    error,
    values,
  };
}

/**
 * What to tell the operator when connecting fails. A conflict while admitting
 * the rule means it was added since the list loaded, for instance in another
 * tab.
 */
function connectFailureMessage(
  entry: CatalogEntry | undefined,
  platformName: string,
  error: unknown,
  admitting: boolean,
): string {
  if (admitting && error instanceof ServiceError && error.statusCode === 409) {
    return duplicateRuleMessage(entry, platformName);
  }
  return error instanceof Error
    ? error.message
    : `Failed to set up ${platformName}`;
}

/** The parts of an access rule this flow compares and carries over. */
type HeldRule = Pick<
  WorkloadAdmission,
  "id" | "issuer" | "subject" | "name" | "tags" | "projectId"
> & { matchKind: string };

/** Whether two rules admit the same subject under the same issuer. */
function sameRule(
  a: { issuer: string; subject: string; matchKind: string },
  b: { issuer: string; subject: string; matchKind: string },
): boolean {
  return (
    a.issuer === b.issuer &&
    a.subject === b.subject &&
    a.matchKind === b.matchKind
  );
}
