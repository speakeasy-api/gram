import {
  invalidateAllOnboarding,
  useOnboarding,
} from "@gram/client/react-query/onboarding.js";
import { useOnboardingReferenceData } from "@gram/client/react-query/onboardingReferenceData.js";
import { invalidateAllOnboardingUseCaseStatus } from "@gram/client/react-query/onboardingUseCaseStatus.js";
import { useSaveOnboardingAnswersMutation } from "@gram/client/react-query/saveOnboardingAnswers.js";
import { useVerifyOnboardingStepMutation } from "@gram/client/react-query/verifyOnboardingStep.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { SettingsPage, SettingsSection } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { MdmPicker } from "./components/mdm-picker";
import { NextStepPanel, type CheckResult } from "./components/next-step-panel";
import { ProductPicker } from "./components/product-picker";
import { UseCasePicker } from "./components/use-case-picker";
import {
  draftFromAnswers,
  draftProblem,
  toSaveRequest,
  type Draft,
} from "./wizard-state";

/**
 * The same answers the wizard asks for, editable after the fact. Saving
 * recomputes the next step; the progress section shows where it stands.
 */
export default function OrgOnboardingSettings(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <SettingsInner />
    </RequireScope>
  );
}

function SettingsInner(): JSX.Element {
  const queryClient = useQueryClient();
  const reference = useOnboardingReferenceData();
  const onboarding = useOnboarding();
  const save = useSaveOnboardingAnswersMutation();
  const verify = useVerifyOnboardingStepMutation();
  const [draftOverride, setDraftOverride] = useState<Draft | null>(null);
  const [lastCheck, setLastCheck] = useState<CheckResult | null>(null);

  const state = onboarding.data;
  const products = reference.data?.products ?? [];
  const draft = draftOverride ?? draftFromAnswers(state?.answers);
  const problem = draftProblem(draft, products);
  const dirty = draftOverride !== null;

  const refresh = async () => {
    await invalidateAllOnboarding(queryClient);
    await invalidateAllOnboardingUseCaseStatus(queryClient);
  };

  const saveAnswers = async () => {
    if (problem) {
      toast.error(problem);
      return;
    }
    try {
      await save.mutateAsync({
        request: { saveAnswersRequestBody: toSaveRequest(draft) },
      });
      await refresh();
      setDraftOverride(null);
      setLastCheck(null);
      toast.success("Onboarding answers saved");
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Could not save answers",
      );
    }
  };

  const nextSlug = state?.nextStep?.slug;
  const check = useCallback(async () => {
    if (!nextSlug || verify.isPending) return;
    try {
      const result = await verify.mutateAsync({
        request: { verifyStepRequestBody: { stepSlug: nextSlug } },
      });
      setLastCheck({ verified: result.verified, evidence: result.evidence });
      await invalidateAllOnboarding(queryClient);
      await invalidateAllOnboardingUseCaseStatus(queryClient);
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Could not check evidence",
      );
    }
  }, [nextSlug, verify, queryClient]);

  const loading = reference.isPending || onboarding.isPending;

  return (
    <SettingsPage
      title="Onboarding"
      description="What you told us during onboarding, and the next step it points to. Change an answer and save to get a new step."
      primaryAction={
        <Button
          onClick={() => void saveAnswers()}
          disabled={!dirty || Boolean(problem) || save.isPending}
        >
          {save.isPending ? "Saving..." : "Save answers"}
        </Button>
      }
    >
      {loading ? (
        <Skeleton>
          <div className="h-64 w-full" />
        </Skeleton>
      ) : (
        <>
          <SettingsSection>
            <SettingsSection.Header>
              <SettingsSection.Title>Progress</SettingsSection.Title>
              <SettingsSection.Description>
                The one step to do next, verified against the last 30 days of
                traffic.
              </SettingsSection.Description>
            </SettingsSection.Header>
            {state?.answers ? (
              <NextStepPanel
                state={state}
                onCheck={() => void check()}
                checking={verify.isPending}
                lastCheck={lastCheck}
                autoCheck={false}
              />
            ) : (
              <p className="text-muted-foreground text-sm">
                Answer the questions below and save to get a next step.
              </p>
            )}
          </SettingsSection>
          <SettingsSection>
            <SettingsSection.Header>
              <SettingsSection.Title>Products</SettingsSection.Title>
              <SettingsSection.Description>
                Which AI products your people use, and on which plan.
              </SettingsSection.Description>
            </SettingsSection.Header>
            <ProductPicker
              products={products}
              draft={draft}
              onChange={setDraftOverride}
              disabled={save.isPending}
            />
          </SettingsSection>
          <SettingsSection>
            <SettingsSection.Header>
              <SettingsSection.Title>Device management</SettingsSection.Title>
              <SettingsSection.Description>
                Whether an MDM can push configuration for you.
              </SettingsSection.Description>
            </SettingsSection.Header>
            <MdmPicker
              vendors={reference.data?.mdmVendors ?? []}
              value={draft.mdmVendor}
              onChange={(mdmVendor) =>
                setDraftOverride({ ...draft, mdmVendor })
              }
              disabled={save.isPending}
            />
          </SettingsSection>
          <SettingsSection>
            <SettingsSection.Header>
              <SettingsSection.Title>Use case</SettingsSection.Title>
              <SettingsSection.Description>
                The one outcome to reach first. Changing it starts a new plan.
              </SettingsSection.Description>
            </SettingsSection.Header>
            <UseCasePicker
              useCases={reference.data?.useCases ?? []}
              value={draft.useCase}
              onChange={(useCase) => setDraftOverride({ ...draft, useCase })}
              disabled={save.isPending}
            />
          </SettingsSection>
        </>
      )}
    </SettingsPage>
  );
}
