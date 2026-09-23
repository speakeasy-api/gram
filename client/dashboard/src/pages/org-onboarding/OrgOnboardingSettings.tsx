import type { SelectUseCaseRequestBodyUseCase } from "@gram/client/models/components/selectusecaserequestbody.js";
import {
  invalidateAllOnboarding,
  useOnboarding,
} from "@gram/client/react-query/onboarding.js";
import { useOnboardingReferenceData } from "@gram/client/react-query/onboardingReferenceData.js";
import { invalidateAllOnboardingUseCaseStatus } from "@gram/client/react-query/onboardingUseCaseStatus.js";
import { useSaveOnboardingStackMutation } from "@gram/client/react-query/saveOnboardingStack.js";
import { useSelectOnboardingUseCaseMutation } from "@gram/client/react-query/selectOnboardingUseCase.js";
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
import { ProviderPicker } from "./components/provider-picker";
import { UseCasePicker } from "./components/use-case-picker";
import {
  draftFromAnswers,
  draftProblem,
  toSaveStackRequest,
  type Draft,
} from "./wizard-state";

/**
 * The same answers the wizard asks for, editable after the fact. The stack
 * saves as one unit and keeps the use case; changing the use case starts a
 * new plan. The progress section shows where the plan stands.
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
  const saveStack = useSaveOnboardingStackMutation();
  const selectUseCase = useSelectOnboardingUseCaseMutation();
  const verify = useVerifyOnboardingStepMutation();
  const [draftOverride, setDraftOverride] = useState<Draft | null>(null);
  const [lastCheck, setLastCheck] = useState<CheckResult | null>(null);

  const state = onboarding.data;
  const providers = reference.data?.providers ?? [];
  const draft = draftOverride ?? draftFromAnswers(state?.answers);
  const problem = draftProblem(draft, providers);
  const dirty = draftOverride !== null;
  const stackSaved = Boolean(state?.answers);

  const refresh = async () => {
    await invalidateAllOnboarding(queryClient);
    await invalidateAllOnboardingUseCaseStatus(queryClient);
  };

  const submitStack = async () => {
    if (problem) {
      toast.error(problem);
      return;
    }
    try {
      await saveStack.mutateAsync({
        request: { saveStackRequestBody: toSaveStackRequest(draft) },
      });
      await refresh();
      setDraftOverride(null);
      setLastCheck(null);
      toast.success("Stack saved");
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Could not save your stack",
      );
    }
  };

  const submitUseCase = async (useCase: string) => {
    if (useCase === state?.answers?.useCase) return;
    try {
      await selectUseCase.mutateAsync({
        request: {
          selectUseCaseRequestBody: {
            useCase: useCase as SelectUseCaseRequestBodyUseCase,
          },
        },
      });
      await refresh();
      setLastCheck(null);
      toast.success("Use case saved");
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Could not save the use case",
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
      description="Your stack as you described it, the use case you picked, and the next step they point to. Change the stack and save to replan; change the use case to start a new plan."
      primaryAction={
        <Button
          onClick={() => void submitStack()}
          disabled={!dirty || Boolean(problem) || saveStack.isPending}
        >
          {saveStack.isPending ? "Saving..." : "Save stack"}
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
            {state?.answers?.useCase ? (
              <NextStepPanel
                state={state}
                onCheck={() => void check()}
                checking={verify.isPending}
                lastCheck={lastCheck}
                autoCheck={false}
              />
            ) : (
              <p className="text-muted-foreground text-sm">
                {stackSaved
                  ? "Pick a use case below to get a next step."
                  : "Describe your stack below and save it, then pick a use case to get a next step."}
              </p>
            )}
          </SettingsSection>
          <SettingsSection>
            <SettingsSection.Header>
              <SettingsSection.Title>Providers and plans</SettingsSection.Title>
              <SettingsSection.Description>
                Which AI vendors your organization uses, and the plan you are on
                with each. The plan applies to every product of the provider.
              </SettingsSection.Description>
            </SettingsSection.Header>
            <ProviderPicker
              providers={providers}
              draft={draft}
              onChange={setDraftOverride}
              disabled={saveStack.isPending}
            />
          </SettingsSection>
          <SettingsSection>
            <SettingsSection.Header>
              <SettingsSection.Title>Products</SettingsSection.Title>
              <SettingsSection.Description>
                Which of their products your people actually use.
              </SettingsSection.Description>
            </SettingsSection.Header>
            <ProductPicker
              providers={providers}
              draft={draft}
              onChange={setDraftOverride}
              disabled={saveStack.isPending}
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
              disabled={saveStack.isPending}
            />
          </SettingsSection>
          <SettingsSection>
            <SettingsSection.Header>
              <SettingsSection.Title>Use case</SettingsSection.Title>
              <SettingsSection.Description>
                The one outcome to reach first. Picking a different one saves
                right away and starts a new plan.
              </SettingsSection.Description>
            </SettingsSection.Header>
            {stackSaved ? (
              <UseCasePicker
                useCases={reference.data?.useCases ?? []}
                value={state?.answers?.useCase ?? null}
                onChange={(useCase) => void submitUseCase(useCase)}
                disabled={selectUseCase.isPending}
              />
            ) : (
              <p className="text-muted-foreground text-sm">
                Save your stack first.
              </p>
            )}
          </SettingsSection>
        </>
      )}
    </SettingsPage>
  );
}
