import { Button } from "@/components/ui/Button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { cn } from "@/lib/utils";
import { inlineError } from "@/pages/org/identity-provider/identityProviderQueries";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import { useAdmitWorkloadSubjectMutation } from "@gram/client/react-query/admitWorkloadSubject.js";
import {
  invalidateAllAgents,
  useAgents,
} from "@gram/client/react-query/agents.js";
import { useCreateAgentMutation } from "@gram/client/react-query/createAgent.js";
import { useRegisterWorkloadIssuerMutation } from "@gram/client/react-query/registerWorkloadIssuer.js";
import { invalidateAllWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import type { CatalogEntry, SetupDefinition, SetupStep } from "./definition";
import {
  SetupBlockView,
  type Option,
  type SetupBlockContext,
} from "./SetupBlocks";
import { useSetupValues } from "./setupValues";
import {
  stepComplete,
  stepIndexById,
  stepReachable,
  type StepInputs,
} from "./steps";
import { tagsProblem } from "../tagLimits";
import { subjectRule, type VariableValues } from "./template";
import { duplicateRuleMessage, existingRule } from "./platforms";

interface PlatformSetupSheetProps {
  entry: CatalogEntry;
  definition: SetupDefinition;
  /** Whether this organization already trusts the entry's issuer. */
  connected: boolean;
  /** The organization's access rules, to catch one the values would repeat. */
  admissions: WorkloadAdmission[];
  /** The step named in the URL, if any. */
  stepId: string | null;
  onStepChange: (stepId: string) => void;
  onClose: () => void;
}

/**
 * A catalog platform's guided setup: the platform's definition, rendered one
 * step at a time in a sheet.
 *
 * Nothing here is specific to a platform. The steps, their copy and the values
 * they ask for come from the definition; what the create step writes is the
 * same issuer and access rule the hand-registration forms write.
 */
export function PlatformSetupSheet({
  entry,
  definition,
  connected,
  admissions,
  stepId,
  onStepChange,
  onClose,
}: PlatformSetupSheetProps): JSX.Element {
  const queryClient = useQueryClient();
  const [values, setValues] = useState<VariableValues>({});
  const [agentId, setAgentId] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [checkedItems, setCheckedItems] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  const [creating, setCreating] = useState(false);
  // Set once this flow has trusted the platform, so the platform-side steps
  // unlock without waiting for the refetch that reports it.
  const [trusted, setTrusted] = useState(false);
  const isTrusted = connected || trusted;
  // The rule this flow saved, which the refetched list then holds; it is not
  // a duplicate of itself.
  const [admittedSubject, setAdmittedSubject] = useState<string | null>(null);

  const agentsQuery = useAgents({}, undefined, { throwOnError: false });
  const agents = useMemo<Option[]>(
    () =>
      (agentsQuery.data ?? [])
        // A suspended, revoked or orphaned agent authenticates and can reach
        // nothing, so it is not offered.
        .filter(
          (agent) =>
            agent.lifecycle === "active" && !agent.ownerReassignmentRequiredAt,
        )
        .map((agent) => ({ id: agent.id, name: agent.name })),
    [agentsQuery.data],
  );
  const agentsUnavailable = agentsUnavailableReason(
    agentsQuery.isPending,
    agentsQuery.isError,
    agents.length,
  );

  const setupValues = useSetupValues();

  const rule = subjectRule(entry, values);
  const conflicting =
    rule === null || rule.subject === admittedSubject
      ? undefined
      : existingRule(entry, admissions, rule);
  const ruleConflict =
    conflicting === undefined
      ? null
      : duplicateRuleMessage(entry, conflicting.agentName);

  const inputs: StepInputs = {
    entry,
    values,
    agentId,
    tags,
    conflictingRuleAgent: conflicting?.agentName ?? null,
  };
  const requested = stepIndexById(definition, stepId);
  const activeIndex =
    requested !== -1 && stepReachable(definition, requested, inputs, isTrusted)
      ? requested
      : 0;
  const activeStep = definition.steps[activeIndex]!;
  const nextStep = definition.steps[activeIndex + 1];
  const previousStep = definition.steps[activeIndex - 1];
  const activeComplete = stepComplete(activeStep, inputs);

  // Each failure below is toasted with its own message, so the global
  // "Request failed" toast would only repeat it.
  const createAgent = useCreateAgentMutation({ onError: inlineError });
  const onCreateAgent = async (name: string): Promise<boolean> => {
    try {
      // Organization-wide, like the access rule it is assigned to.
      const agent = await createAgent.mutateAsync({
        request: { createAgentForm: { name } },
      });
      await invalidateAllAgents(queryClient);
      setAgentId(agent.id);
      toast.success(`Agent ${agent.name} created`);
      return true;
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to create the agent",
      );
      return false;
    }
  };

  const registerIssuer = useRegisterWorkloadIssuerMutation({
    onError: inlineError,
  });
  const admitSubject = useAdmitWorkloadSubjectMutation({
    onError: inlineError,
  });

  const create = async () => {
    if (rule === null || agentId === "" || creating) return;
    setCreating(true);
    // Registering the issuer conflicts on a duplicate name, admitting the rule
    // on a duplicate rule; only the second means the values are taken.
    let admitting = false;
    try {
      if (!isTrusted) {
        await registerIssuer.mutateAsync({
          request: {
            registerWorkloadIssuerForm: {
              name: entry.displayName,
              description: entry.description,
              issuer: entry.issuer.value,
              jwksUri: entry.jwksUri.value,
              tags: [],
            },
          },
        });
        setTrusted(true);
      }
      admitting = true;
      await admitSubject.mutateAsync({
        request: {
          admitWorkloadSubjectForm: {
            issuer: entry.issuer.value,
            subject: rule.subject,
            matchKind: rule.matchKind,
            agentId,
            tags,
          },
        },
      });
      setAdmittedSubject(rule.subject);
      toast.success(
        isTrusted
          ? `Access rule added to ${entry.displayName}`
          : `${entry.displayName} is trusted`,
      );
      if (nextStep !== undefined) onStepChange(nextStep.id);
    } catch (error) {
      toast.error(setupFailureMessage(entry, error, admitting));
    } finally {
      // Refetched on failure too: the platform may have been trusted before
      // the access rule was refused, and a retry must then only add the rule.
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setCreating(false);
    }
  };

  // Another access rule under a platform already trusted, such as a second
  // Anthropic organization. The platform's own values stay, since it is
  // already trusted with them; the rule's are asked for again, starting at the
  // first step that collects one.
  const addAccess = () => {
    setValues((current) =>
      Object.fromEntries(
        Object.entries(current).filter(([key]) =>
          entry.variables.some(
            (variable) => variable.key === key && variable.tier === "platform",
          ),
        ),
      ),
    );
    setAgentId("");
    setTags([]);
    setAdmittedSubject(null);
    const first =
      definition.steps.find((step) => collectsRuleValue(entry, step)) ??
      definition.steps.find((step) => step.phase === "collect");
    if (first !== undefined) onStepChange(first.id);
  };

  const context: SetupBlockContext = {
    entry,
    values,
    onValueChange: (key, value) =>
      setValues((current) => ({ ...current, [key]: value })),
    agents,
    agentsUnavailable,
    agentId,
    onAgentChange: setAgentId,
    onCreateAgent,
    tags,
    onTagsChange: setTags,
    ruleConflict,
    setupValues,
    checkedItems,
    onCheckedChange: (blockKey, checked) =>
      setCheckedItems((current) => {
        const next = new Set(current);
        if (checked) next.add(blockKey);
        else next.delete(blockKey);
        return next;
      }),
  };

  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        side="right"
        className="flex w-[640px] max-w-[calc(100vw-2rem)] flex-col overflow-hidden p-0 sm:max-w-[640px]"
      >
        <SheetHeader className="px-6 pt-6 pr-14 pb-0">
          <SheetTitle className="text-lg font-semibold">
            Set up {entry.displayName}
          </SheetTitle>
          <SheetDescription>{entry.description}</SheetDescription>
        </SheetHeader>

        <StepProgress
          steps={definition.steps}
          activeIndex={activeIndex}
          isReachable={(index) =>
            stepReachable(definition, index, inputs, isTrusted)
          }
          onPick={(index) => onStepChange(definition.steps[index]!.id)}
        />

        <div className="relative min-h-0 flex-1 overflow-hidden">
          <div
            className="flex h-full transition-transform duration-300 ease-in-out"
            style={{ transform: `translateX(-${activeIndex * 100}%)` }}
          >
            {definition.steps.map((step, index) => (
              <div
                key={step.id}
                // Off-screen steps stay mounted for the slide, but out of the
                // tab order and the accessibility tree.
                inert={index !== activeIndex}
                className="w-full shrink-0 space-y-4 overflow-y-auto px-6 pb-6"
              >
                <p className="text-eyebrow">Step {index + 1}</p>
                <h3 className="text-foreground text-base font-medium">
                  {step.title}
                </h3>
                {step.blocks.map((block, blockIndex) => (
                  <SetupBlockView
                    key={blockIndex}
                    block={block}
                    blockKey={`${step.id}-${blockIndex}`}
                    context={context}
                  />
                ))}
              </div>
            ))}
          </div>
        </div>

        <SheetFooter className="flex-row items-center justify-between gap-2 border-t px-6 py-4">
          <div>
            {previousStep !== undefined && activeStep.phase !== "connect" && (
              <Button
                variant="secondary"
                onClick={() => onStepChange(previousStep.id)}
                disabled={creating}
              >
                <Button.Text>Back</Button.Text>
              </Button>
            )}
            {activeStep.phase === "connect" && (
              <Button variant="tertiary" onClick={addAccess}>
                <Button.Text>Allow another</Button.Text>
              </Button>
            )}
          </div>
          <StepAction
            step={activeStep}
            nextStep={nextStep}
            complete={activeComplete}
            canCreate={
              activeComplete &&
              subjectRule(entry, values) !== null &&
              agentId !== "" &&
              tagsProblem(tags) === null
            }
            creating={creating}
            entry={entry}
            onNext={(id) => onStepChange(id)}
            onCreate={() => void create()}
            onDone={onClose}
          />
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

/** Whether a step has a field for a variable supplied per access rule. */
function collectsRuleValue(entry: CatalogEntry, step: SetupStep): boolean {
  return step.blocks.some(
    (block) =>
      block.type === "field" &&
      entry.variables.some(
        (variable) =>
          variable.key === block.variable && variable.tier === "rule",
      ),
  );
}

function agentsUnavailableReason(
  isPending: boolean,
  isError: boolean,
  activeCount: number,
): string | null {
  if (isPending) return null;
  if (isError) return "Agents are unavailable right now.";
  if (activeCount === 0) {
    return "There is no active agent to choose. Create one under Agents, then come back.";
  }
  return null;
}

function StepAction({
  step,
  nextStep,
  complete,
  canCreate,
  creating,
  entry,
  onNext,
  onCreate,
  onDone,
}: {
  step: SetupStep;
  nextStep: SetupStep | undefined;
  complete: boolean;
  /** Whether the collected values make an access rule. */
  canCreate: boolean;
  creating: boolean;
  entry: CatalogEntry;
  onNext: (stepId: string) => void;
  onCreate: () => void;
  onDone: () => void;
}): JSX.Element {
  if (step.phase === "create") {
    return (
      <Button onClick={onCreate} disabled={creating || !canCreate}>
        <Button.Text>{createLabel(entry, creating)}</Button.Text>
      </Button>
    );
  }
  if (nextStep === undefined) {
    return (
      <Button onClick={onDone}>
        <Button.Text>Done</Button.Text>
      </Button>
    );
  }
  return (
    <Button onClick={() => onNext(nextStep.id)} disabled={!complete}>
      <Button.Text>Continue</Button.Text>
    </Button>
  );
}

function createLabel(entry: CatalogEntry, creating: boolean): string {
  return creating ? "Saving…" : `Connect ${entry.displayName}`;
}

/** One dash per step; a dash is a link back to any step that can be shown. */
function StepProgress({
  steps,
  activeIndex,
  isReachable,
  onPick,
}: {
  steps: SetupStep[];
  activeIndex: number;
  isReachable: (index: number) => boolean;
  onPick: (index: number) => void;
}): JSX.Element {
  return (
    <div className="flex items-center gap-1.5 px-6 pt-4 pb-4">
      {steps.map((step, index) => {
        const reachable = isReachable(index);
        return (
          <button
            key={step.id}
            type="button"
            aria-current={index === activeIndex ? "step" : undefined}
            aria-label={`Step ${index + 1}: ${step.title}`}
            disabled={!reachable}
            onClick={() => onPick(index)}
            className={cn(
              "h-1 transition-all disabled:cursor-not-allowed",
              dashClass(index, activeIndex),
            )}
          />
        );
      })}
      <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
        {activeIndex + 1}/{steps.length}
      </span>
    </div>
  );
}

function dashClass(index: number, activeIndex: number): string {
  if (index === activeIndex) return "bg-foreground w-6";
  if (index < activeIndex) return "bg-foreground/40 hover:bg-foreground/60 w-4";
  return "bg-border w-4";
}

/**
 * What to tell the operator when the create step fails. A conflict while
 * admitting the rule means it was added since the list loaded, for instance in
 * another tab.
 */
function setupFailureMessage(
  entry: CatalogEntry,
  error: unknown,
  admitting: boolean,
): string {
  if (admitting && error instanceof ServiceError && error.statusCode === 409) {
    return duplicateRuleMessage(entry);
  }
  return error instanceof Error
    ? error.message
    : `Failed to set up ${entry.displayName}`;
}
