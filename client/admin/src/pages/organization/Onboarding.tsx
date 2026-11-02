import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useBlocker, useParams } from "@tanstack/react-router";
import { useRef, useState, type JSX } from "react";
import type { AdminOnboardingPlaybook } from "@gram/admin-client/models/components/adminonboardingplaybook";
import type { AdminOnboardingStep } from "@gram/admin-client/models/components/adminonboardingstep";

import { PlaybookStepEditor } from "@/components/onboarding/PlaybookStepEditor";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { organizationQuery } from "@/lib/adminQueries";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  assignAdminOrganizationOnboardingPlaybook,
  createAdminOnboardingPlaybook,
  onboardingPlaybooksQuery,
  onboardingStepsQuery,
  onboardingUseCasesQuery,
  organizationOnboardingPlaybookQuery,
  updateAdminOnboardingPlaybook,
} from "@/lib/gramAdminClient";
import { OnboardingStack } from "./OnboardingStack";

export function OnboardingRoute(): JSX.Element {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const organization = useQuery({
    ...organizationQuery(idOrSlug),
    throwOnError: false,
  });
  if (organization.isError)
    return (
      <div role="alert">
        Unable to load organization.{" "}
        <Button onClick={() => void organization.refetch()}>
          Retry organization
        </Button>
      </div>
    );
  if (!organization.data) return <p role="status">Loading organization…</p>;
  return (
    <Onboarding
      key={organization.data.id}
      organizationId={organization.data.id}
      organizationName={organization.data.name}
    />
  );
}

type DraftChoice =
  | { kind: "custom"; steps: string[]; current?: AdminOnboardingPlaybook }
  | { kind: "template"; template: AdminOnboardingPlaybook };

type Draft = DraftChoice & { base: string };

function snapshot(plan?: AdminOnboardingPlaybook): string {
  return JSON.stringify(
    plan
      ? [
          plan.id,
          plan.name,
          plan.description,
          plan.steps.map((step) => step.slug),
        ]
      : null,
  );
}

function contentSnapshot(plan: {
  name: string;
  description?: string;
  steps: { slug: string }[];
}): string {
  return JSON.stringify([
    plan.name,
    plan.description,
    plan.steps.map((step) => step.slug),
  ]);
}

function StepSummary({
  slugs,
  steps,
}: {
  slugs: { slug: string; title: string }[];
  steps: AdminOnboardingStep[];
}): JSX.Element {
  return (
    <ol className="list-decimal space-y-3 pl-5">
      {slugs.map((step) => {
        const children = steps.filter(
          (child) => child.parentSlug === step.slug,
        );
        return (
          <li key={step.slug}>
            <span className="font-medium">{step.title}</span>
            {children.length > 0 && (
              <p className="text-muted-foreground text-sm">
                Includes {children.map((child) => child.title).join(" · ")}
              </p>
            )}
          </li>
        );
      })}
    </ol>
  );
}

/** Tenant-only edits: shared templates are copied by the assignment API, never edited here. */
export function Onboarding({
  organizationId,
  organizationName,
}: {
  organizationId: string;
  organizationName?: string;
}): JSX.Element {
  const client = useQueryClient();
  const assignmentQuery = organizationOnboardingPlaybookQuery(organizationId);
  const templatesQuery = onboardingPlaybooksQuery(organizationId);
  const assignment = useQuery({ ...assignmentQuery, throwOnError: false });
  const steps = useQuery({ ...onboardingStepsQuery(), throwOnError: false });
  const templates = useQuery({ ...templatesQuery, throwOnError: false });
  const [draft, setDraft] = useState<Draft | null>(null);
  const [templateId, setTemplateId] = useState("");
  const [saved, setSaved] = useState(false);
  const saving = useRef(false);
  const createdPlan = useRef<AdminOnboardingPlaybook | null>(null);
  const pendingCreation = useRef<{
    knownIds: string[];
    content: string;
  } | null>(null);
  const blocker = useBlocker({
    shouldBlockFn: () => draft !== null || saving.current,
    enableBeforeUnload: draft !== null,
    withResolver: true,
  });
  const refresh = () =>
    Promise.all([
      client.invalidateQueries({ queryKey: assignmentQuery.queryKey }),
      client.invalidateQueries({ queryKey: templatesQuery.queryKey }),
      client.invalidateQueries({
        queryKey: onboardingPlaybooksQuery().queryKey,
      }),
      client.invalidateQueries({
        queryKey: onboardingUseCasesQuery().queryKey,
      }),
    ]);
  const save = useMutation({
    mutationFn: async (value: Draft) => {
      // A fresh read detects observed conflicts, not the race after this read:
      // the API does not provide transactional compare-and-swap.
      const latest = await assignment.refetch({ throwOnError: true });
      if (
        createdPlan.current &&
        snapshot(latest.data?.playbook) === snapshot(createdPlan.current)
      ) {
        if (
          value.kind === "custom" &&
          JSON.stringify(value.steps) ===
            JSON.stringify(createdPlan.current.steps.map((step) => step.slug))
        )
          return;
        throw new Error(
          "The previously saved selection is already assigned, but your draft has changed. Reload the current plan and review it before saving.",
        );
      }
      if (
        value.kind === "template" &&
        latest.data?.playbook?.organizationId === organizationId &&
        contentSnapshot(latest.data.playbook) ===
          contentSnapshot(value.template)
      )
        return;
      if (snapshot(latest.data?.playbook) !== value.base) {
        throw new Error(
          "The current plan changed since editing began. Reload the current plan and review it before saving. Your draft has not been applied.",
        );
      }
      if (value.kind === "template" || createdPlan.current) {
        const reviewed =
          value.kind === "template" ? value.template : createdPlan.current!;
        const library = await templates.refetch({ throwOnError: true });
        const fresh = library.data?.playbooks.find(
          (plan) => plan.id === reviewed.id,
        );
        if (snapshot(fresh) !== snapshot(reviewed)) {
          throw new Error(
            "The reviewed template or retained plan changed or was deleted. Reload the current plan and review your selection before saving.",
          );
        }
      }
      if (value.kind === "template") {
        try {
          await assignAdminOrganizationOnboardingPlaybook({
            organizationId,
            playbookId: value.template.id,
          });
        } catch (error) {
          // Shared assignment creates a tenant copy, so its ID is different.
          const reconciled = await assignment.refetch();
          const copied = reconciled.data?.playbook;
          if (
            !reconciled.isError &&
            copied?.organizationId === organizationId &&
            contentSnapshot(copied) === contentSnapshot(value.template)
          )
            return;
          throw error;
        }
      } else if (value.current?.organizationId === organizationId) {
        await updateAdminOnboardingPlaybook({
          organizationId,
          playbookId: value.current.id,
          name: value.current.name,
          description: value.current.description,
          stepSlugs: value.steps,
        });
      } else {
        if (!createdPlan.current && pendingCreation.current) {
          const library = await templates.refetch({ throwOnError: true });
          const attempt = pendingCreation.current;
          const matches =
            library.data?.playbooks.filter(
              (plan) =>
                plan.organizationId === organizationId &&
                !attempt.knownIds.includes(plan.id) &&
                contentSnapshot(plan) === attempt.content,
            ) ?? [];
          if (matches.length !== 1) {
            throw new Error(
              "Creation could not be verified. Retry checks for the previously created plan without creating another. Review the plan library before starting over.",
            );
          }
          createdPlan.current = matches[0]!;
        }
        if (!createdPlan.current) {
          const library = await templates.refetch({ throwOnError: true });
          const request = {
            organizationId,
            name: `${organizationName ?? "Organization"} onboarding`,
            description: "Tenant-specific setup wizard steps",
            isDefault: false,
            stepSlugs: value.steps,
          };
          // Remember the pre-write library and original content even if the response
          // is lost. Until discovery succeeds, retries must not issue another create.
          pendingCreation.current = {
            knownIds: library.data?.playbooks.map((plan) => plan.id) ?? [],
            content: contentSnapshot({
              ...request,
              steps: value.steps.map((slug) => ({ slug })),
            }),
          };
          createdPlan.current = await createAdminOnboardingPlaybook(request);
        }
        if (
          JSON.stringify(value.steps) !==
          JSON.stringify(createdPlan.current.steps.map((step) => step.slug))
        ) {
          // The fresh read observed this plan as unassigned. This is still not
          // transactional: another actor may assign it between read and update.
          const retained = createdPlan.current;
          createdPlan.current = await updateAdminOnboardingPlaybook({
            organizationId,
            playbookId: retained.id,
            name: retained.name,
            description: retained.description,
            stepSlugs: value.steps,
          });
        }
        const created = createdPlan.current;
        try {
          await assignAdminOrganizationOnboardingPlaybook({
            organizationId,
            playbookId: created.id,
          });
        } catch (error) {
          // A rejected response may arrive after assignment committed. Never delete
          // the new plan: deleting an assigned plan would change the live wizard.
          const reconciled = await assignment.refetch();
          if (
            !reconciled.isError &&
            snapshot(reconciled.data?.playbook) === snapshot(created)
          )
            return;
          throw new Error(
            `Assignment could not be verified: ${errorMessage(error)} The new plan is preserved, not deleted. Retry reuses that plan; reload and review before starting over to avoid duplicates.`,
          );
        }
      }
    },
    onSuccess: async () => {
      setDraft(null);
      setSaved(true);
      await refresh();
    },
    onError: async () => {
      await refresh();
    },
    onSettled: () => {
      saving.current = false;
    },
  });
  const begin = (value: DraftChoice) => {
    createdPlan.current = null;
    pendingCreation.current = null;
    save.reset();
    setSaved(false);
    setDraft({ ...value, base: snapshot(assignment.data?.playbook) });
  };
  const submit = () => {
    if (!draft || saving.current) return;
    saving.current = true;
    save.mutate(draft);
  };
  const current = assignment.data?.playbook;
  const shared =
    templates.data?.playbooks.filter((template) => !template.organizationId) ??
    [];
  const selected = shared.find((template) => template.id === templateId);
  const readFailed = assignment.isError || steps.isError;
  const ready = !readFailed && !!assignment.data && !!steps.data;
  const busy = save.isPending;
  return (
    <div className="max-w-4xl space-y-8">
      <Dialog
        open={blocker.status === "blocked"}
        onOpenChange={(open) => {
          if (!open && blocker.status === "blocked") blocker.reset();
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Leave onboarding changes?</DialogTitle>
            <DialogDescription>
              {busy
                ? "A save is in progress. Wait for its result before leaving."
                : "Your unsaved wizard changes or template selection will be discarded."}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                if (blocker.status === "blocked") blocker.reset();
              }}
            >
              Stay on page
            </Button>
            <Button
              disabled={busy}
              onClick={() => {
                if (blocker.status === "blocked") blocker.proceed();
              }}
            >
              Leave without saving
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <header className="space-y-2">
        <h1 className="text-2xl font-semibold">Onboarding</h1>
        <p>Choose the steps this organization sees in its setup wizard.</p>
        {organizationName && <p className="font-medium">{organizationName}</p>}
        <p className="text-muted-foreground text-sm">
          Groups include the cards listed below them. This does not change the
          homepage.
        </p>
      </header>
      <section
        aria-labelledby="wizard-heading"
        className="space-y-4 rounded-lg border p-5"
      >
        <h2 id="wizard-heading" className="text-lg font-semibold">
          Wizard steps
        </h2>
        {readFailed ? (
          <div role="alert">
            Unable to load current wizard steps.{" "}
            {errorMessage(assignment.error ?? steps.error)}{" "}
            <Button
              onClick={() => {
                void assignment.refetch();
                void steps.refetch();
              }}
            >
              Retry wizard steps
            </Button>
          </div>
        ) : !ready ? (
          <p role="status">Loading wizard steps…</p>
        ) : (
          <>
            {draft?.kind === "custom" ? (
              <>
                <p>
                  Changes apply immediately to this organization. Existing
                  progress and assignees are kept.
                </p>
                {!draft.current ||
                draft.current.organizationId !== organizationId ? (
                  <p className="text-muted-foreground text-sm">
                    Saving creates and assigns a plan for this organization.
                  </p>
                ) : null}
                <PlaybookStepEditor
                  steps={steps.data!.steps}
                  value={draft.steps}
                  onChange={(next) => setDraft({ ...draft, steps: next })}
                  disabled={busy}
                />
                <Button
                  disabled={busy || draft.steps.length === 0}
                  onClick={submit}
                >
                  {busy ? "Saving…" : "Save wizard steps"}
                </Button>
              </>
            ) : draft?.kind === "template" ? (
              <>
                <h3 className="font-medium">Use {draft.template.name}?</h3>
                <StepSummary
                  slugs={draft.template.steps}
                  steps={steps.data!.steps}
                />
                <p>
                  This assigns an organization-owned copy and replaces the
                  current plan for {organizationName ?? "this organization"}.
                  Later shared-template changes will not sync. Existing progress
                  and assignees are not reset.
                </p>
                <Button disabled={busy} onClick={submit}>
                  {busy ? "Assigning…" : "Confirm template assignment"}
                </Button>
              </>
            ) : (
              <>
                {current ? (
                  <>
                    <h3 className="font-medium">{current.name}</h3>
                    <StepSummary
                      slugs={current.steps}
                      steps={steps.data!.steps}
                    />
                  </>
                ) : (
                  <p>
                    No custom plan is assigned. The setup wizard uses its saved
                    task selection and catalog defaults; this does not mean
                    onboarding is disabled.
                  </p>
                )}
                <Button
                  disabled={busy}
                  onClick={() =>
                    begin({
                      kind: "custom",
                      current,
                      steps: current?.steps.map((step) => step.slug) ?? [],
                    })
                  }
                >
                  Edit wizard steps
                </Button>
              </>
            )}
          </>
        )}
        {draft && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              setDraft(null);
              save.reset();
            }}
          >
            Discard changes
          </Button>
        )}
        {save.isError && (
          <p role="alert">
            Unable to save onboarding. {errorMessage(save.error)} Your draft is
            retained. Refresh current wizard steps before retrying if the result
            is uncertain.
          </p>
        )}
        {save.isError && !readFailed && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              void assignment.refetch().then((refreshed) => {
                if (!refreshed.isError) {
                  setDraft(null);
                  save.reset();
                }
              });
            }}
          >
            Reload current plan and discard draft
          </Button>
        )}
        {saved && <p role="status">Onboarding saved.</p>}
      </section>
      {!draft && (
        <section
          aria-labelledby="template-heading"
          className="space-y-3 rounded-lg border p-5"
        >
          <h2 id="template-heading" className="text-lg font-semibold">
            Use a template
          </h2>
          <p className="text-muted-foreground text-sm">
            Start from a shared template, copied for this organization. No
            shared template is edited here.
          </p>
          {templates.isError ? (
            <div role="alert">
              Unable to load templates. {errorMessage(templates.error)}{" "}
              <Button onClick={() => void templates.refetch()}>
                Retry templates
              </Button>
            </div>
          ) : templates.isPending ? (
            <p role="status">Loading templates…</p>
          ) : (
            <>
              <label className="block" htmlFor="onboarding-template">
                Shared template
              </label>
              <select
                id="onboarding-template"
                className="border-input bg-background rounded-md border p-2"
                value={templateId}
                disabled={busy}
                onChange={(event) => setTemplateId(event.target.value)}
              >
                <option value="">Choose a template</option>
                {shared.map((template) => (
                  <option key={template.id} value={template.id}>
                    {template.useCaseName ? `${template.useCaseName} — ` : ""}
                    {template.name}
                  </option>
                ))}
              </select>
              {shared.length === 0 && <p>No shared templates are available.</p>}
              <Button
                variant="outline"
                disabled={!selected || !ready || busy}
                onClick={() => {
                  if (selected) begin({ kind: "template", template: selected });
                }}
              >
                Review template
              </Button>
            </>
          )}
          <p>
            <Link to="/onboarding-playbooks" className="underline">
              Manage shared templates
            </Link>
          </p>
        </section>
      )}
      <section aria-labelledby="stack-heading" className="space-y-3">
        <h2 id="stack-heading" className="text-lg font-semibold">
          Recorded stack
        </h2>
        <p className="text-muted-foreground text-sm">
          Recorded software can affect step applicability. This is separate from
          choosing wizard steps.
        </p>
        <OnboardingStack organizationId={organizationId} />
      </section>
    </div>
  );
}
