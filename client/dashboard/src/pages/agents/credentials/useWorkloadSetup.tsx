import { useOrganization, useProject } from "@/contexts/Auth";
import { WorkloadIssuerSelector } from "./WorkloadIssuerSelector";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { AssertionReuseField } from "../WorkloadIssuerPolicy";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import { useAdmitWorkloadSubjectMutation } from "@gram/client/react-query/admitWorkloadSubject.js";
import { useRegisterWorkloadIssuerMutation } from "@gram/client/react-query/registerWorkloadIssuer.js";
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
  connectedSubject,
  settled = false,
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
   * The subject this agent is already admitted under. Its own rule is not a
   * duplicate of itself, and the create step has nothing left to write.
   */
  connectedSubject?: string;
  /**
   * The rule is not being written here: this flow is editing an agent that
   * already holds one. Nothing on screen can clash with it, and the create
   * step has nothing to do.
   */
  settled?: boolean;
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
  // The rule this flow saved, which the refetched list then holds; it is not
  // a duplicate of itself.
  const [admittedSubject, setAdmittedSubject] = useState<string | null>(
    connectedSubject ?? null,
  );

  const registerIssuer = useRegisterWorkloadIssuerMutation();
  const admitSubject = useAdmitWorkloadSubjectMutation();

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

  const isTrusted =
    connection !== null &&
    (connection.issuer === trustedIssuer ||
      connectedIssuer(connection.issuer, issuers) !== undefined);
  // The rule this flow has already written, or the one the agent being
  // edited already holds. Read from the prop rather than from state seeded
  // with it: the admission arrives a render or two after the first one, and
  // the agent's own rule must never be reported as a clash with itself.
  const ours =
    settled ||
    (connection !== null &&
      (connection.subject === admittedSubject ||
        connection.subject === connectedSubject));
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
   * without one is refused at the token endpoint.
   */
  const connect = async (): Promise<boolean> => {
    if (connection === null || connecting) return false;
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
      admitting = true;
      await admitSubject.mutateAsync({
        request: {
          ...(customFlow ? { gramProject: project.slug } : {}),
          admitWorkloadSubjectForm: {
            ...(customFlow
              ? { projectScoped: !!selectedIssuer?.projectId }
              : {}),
            issuer: connection.issuer,
            subject: connection.subject,
            matchKind: connection.matchKind,
            agentId: agentID,
            tags: [],
          },
        },
      });
      setAdmittedSubject(connection.subject);
      onConnected(agentID);
      return true;
    } catch (failure) {
      setError(connectFailureMessage(entry, platformName, failure, admitting));
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
    render: (step) =>
      customFlow && step.id === "issuer" ? (
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
          {step.phase === "create" && !isTrusted && !settled && (
            <AssertionReuseField
              checked={connection !== null && reuseIssuer === connection.issuer}
              disabled={connecting || connection === null}
              onChange={(checked) =>
                setReuseIssuer(checked ? (connection?.issuer ?? null) : null)
              }
            />
          )}
        </div>
      ),
    complete: (step) =>
      customFlow && step.id === "issuer"
        ? !!selectedIssuer && !identities.isPending && !identities.isError
        : (step.complete?.(context) ?? true),
    connect,
    connecting,
    canConnect: connection !== null,
    connected: connection !== null && connection.subject === admittedSubject,
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
