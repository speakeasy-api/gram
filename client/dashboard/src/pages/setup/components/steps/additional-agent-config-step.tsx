import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ArrowLeft,
  Check,
  ChevronRight,
  KeyRound,
  Loader2,
} from "lucide-react";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { cn } from "@/lib/utils";
import {
  AI_INTEGRATION_PROVIDERS,
  type AIIntegrationProvider,
} from "@/pages/org/ai-integration-providers";
import { CodexIcon } from "@/pages/hooks/HookSourceIcon";
import { useAIIntegrationConfigForm } from "@/pages/org/use-ai-integration-config-form";
import { useAiIntegrationConfig } from "@gram/client/react-query/aiIntegrationConfig";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { StepContainer } from "../step-container";

interface AdditionalAgentConfigStepProps {
  onComplete: () => void;
  onSkip: () => void;
  onBack: () => void;
}

type ProviderStatus = "not_started" | "complete";
type AdditionalAgentConfigProvider = AIIntegrationProvider & {
  available?: boolean;
};

const ADDITIONAL_AGENT_CONFIG_PROVIDERS: AdditionalAgentConfigProvider[] = [
  ...AI_INTEGRATION_PROVIDERS,
  {
    provider: "codex",
    name: "OpenAI Compliance API",
    description: "Export Codex activity logs for audit workflows.",
    onboardingDescription:
      "Codex compliance exports for audit, monitoring, and investigations are coming soon.",
    icon: CodexIcon,
    apiKeyLabel: "OpenAI Compliance API key",
    apiKeyPlaceholder: "Coming soon",
    requiresOrganizationId: false,
    available: false,
  },
];

export function AdditionalAgentConfigStep({
  onComplete,
  onSkip,
  onBack,
}: AdditionalAgentConfigStepProps): JSX.Element {
  const [drawerProviderId, setDrawerProviderId] = useState<string | null>(null);
  const [providerStatus, setProviderStatus] = useState<
    Record<string, ProviderStatus>
  >(() =>
    Object.fromEntries(
      ADDITIONAL_AGENT_CONFIG_PROVIDERS.map((provider) => [
        provider.provider,
        "not_started",
      ]),
    ),
  );
  const availableProviders = useMemo(
    () =>
      ADDITIONAL_AGENT_CONFIG_PROVIDERS.filter(
        (provider) => provider.available !== false,
      ),
    [],
  );
  const comingSoonProviders = useMemo(
    () =>
      ADDITIONAL_AGENT_CONFIG_PROVIDERS.filter(
        (provider) => provider.available === false,
      ),
    [],
  );

  const completedCount = useMemo(
    () =>
      availableProviders.filter(
        (provider) => providerStatus[provider.provider] === "complete",
      ).length,
    [availableProviders, providerStatus],
  );

  const activeProvider =
    availableProviders.find(
      (provider) => provider.provider === drawerProviderId,
    ) ?? null;

  const handleConfigured = useCallback((providerId: string) => {
    setProviderStatus((prev) => ({ ...prev, [providerId]: "complete" }));
  }, []);

  const closeDrawer = () => setDrawerProviderId(null);

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <KeyRound className="text-foreground h-6 w-6" />
        </div>
      }
      title="Additional agent configuration"
      description="Optionally connect admin and compliance APIs so Gram can import usage, spend, and review data across the agent platforms your team uses."
      onContinue={onComplete}
      onSkip={onSkip}
      skipLabel="Skip for now"
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground text-sm">
            {completedCount} of {availableProviders.length} providers configured
          </span>
        </div>

        {availableProviders.map((provider) => (
          <ProviderSetupRow
            key={provider.provider}
            provider={provider}
            status={providerStatus[provider.provider] ?? "not_started"}
            onConfigured={handleConfigured}
            onOpen={() => setDrawerProviderId(provider.provider)}
          />
        ))}

        {comingSoonProviders.length > 0 ? (
          <div className="pt-3">
            <p className="text-muted-foreground mb-2 text-[11px] font-medium tracking-wider uppercase">
              Coming soon
            </p>
            <div className="grid grid-cols-2 gap-2">
              {comingSoonProviders.map((provider) => (
                <ProviderComingSoonCard
                  key={provider.provider}
                  provider={provider}
                />
              ))}
            </div>
          </div>
        ) : null}

        <p className="text-muted-foreground pt-2 text-xs leading-relaxed">
          These connections are optional. You can skip this step and add or edit
          provider keys from organization settings later.
        </p>
      </div>

      <Sheet
        open={!!drawerProviderId}
        onOpenChange={(open) => {
          if (!open) closeDrawer();
        }}
      >
        <SheetContent
          side="right"
          className="flex w-full flex-col overflow-hidden p-0 sm:max-w-[662px]"
        >
          {activeProvider ? (
            <ProviderConfigDrawer
              provider={activeProvider}
              onConfigured={() => handleConfigured(activeProvider.provider)}
              onClose={closeDrawer}
            />
          ) : null}
        </SheetContent>
      </Sheet>
    </StepContainer>
  );
}

function ProviderComingSoonCard({
  provider,
}: {
  provider: AdditionalAgentConfigProvider;
}): JSX.Element {
  const Icon = provider.icon;

  return (
    <div
      aria-disabled
      className="border-border bg-card flex cursor-not-allowed items-center gap-3 border p-3 opacity-50"
    >
      <div className="bg-secondary flex h-8 w-8 flex-shrink-0 items-center justify-center">
        <Icon className="text-foreground h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-foreground truncate text-sm font-medium">
          {provider.name}
        </p>
        <p className="text-muted-foreground truncate text-xs">
          {provider.onboardingDescription}
        </p>
      </div>
    </div>
  );
}

function ProviderSetupRow({
  provider,
  status,
  onConfigured,
  onOpen,
}: {
  provider: AIIntegrationProvider;
  status: ProviderStatus;
  onConfigured: (providerId: string) => void;
  onOpen: () => void;
}): JSX.Element {
  const { data } = useAiIntegrationConfig({ provider: provider.provider });
  const isComplete = status === "complete" || Boolean(data?.id);
  const Icon = provider.icon;

  useEffect(() => {
    if (data?.id) onConfigured(provider.provider);
  }, [data?.id, onConfigured, provider.provider]);

  return (
    <button
      type="button"
      onClick={onOpen}
      className={cn(
        "flex w-full items-center gap-4 border p-4 text-left transition-all",
        isComplete
          ? "border-foreground/10 bg-secondary/20"
          : "border-border bg-card hover:border-foreground/20",
      )}
    >
      <div
        className={cn(
          "flex h-10 w-10 flex-shrink-0 items-center justify-center",
          isComplete ? "bg-foreground/10" : "bg-secondary",
        )}
      >
        <Icon className="text-foreground h-5 w-5" />
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-2">
          <p className="text-foreground text-sm font-medium">{provider.name}</p>
          {isComplete ? (
            <Badge variant="success" background>
              <Badge.LeftIcon>
                <Check className="h-3 w-3" />
              </Badge.LeftIcon>
              <Badge.Text>Complete</Badge.Text>
            </Badge>
          ) : null}
        </div>
        <p className="text-muted-foreground text-xs">
          {provider.onboardingDescription}
        </p>
      </div>
      <ChevronRight className="text-muted-foreground h-4 w-4 flex-shrink-0" />
    </button>
  );
}

function ProviderConfigDrawer({
  provider,
  onConfigured,
  onClose,
}: {
  provider: AIIntegrationProvider;
  onConfigured: () => void;
  onClose: () => void;
}): JSX.Element {
  const [currentStepIdx, setCurrentStepIdx] = useState(0);
  const form = useAIIntegrationConfigForm(provider, {
    onSaveSuccess: () => {
      onConfigured();
      onClose();
    },
  });
  const apiKeyFieldId = `${provider.provider}-onboarding-api-key`;
  const orgIdFieldId = `${provider.provider}-onboarding-org-id`;
  const setupSteps = provider.setupGuide?.steps ?? [
    {
      title: `Configure ${provider.name}`,
      description: provider.onboardingDescription,
      showsForm: true,
    },
  ];
  const currentStep = setupSteps[currentStepIdx];
  const isLastStep = currentStepIdx === setupSteps.length - 1;
  const showForm = currentStep?.showsForm ?? isLastStep;
  const { isConfigured, setEnabled } = form;

  // Onboarding is an opt-in setup path, so a newly saved provider should be
  // enabled by default. Existing configs keep their saved enabled state.
  useEffect(() => {
    if (!isConfigured) setEnabled(true);
  }, [isConfigured, setEnabled]);

  return (
    <>
      <SheetHeader className="sr-only">
        <SheetTitle>Configure {provider.name}</SheetTitle>
        <SheetDescription>{provider.onboardingDescription}</SheetDescription>
      </SheetHeader>

      <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
        {setupSteps.map((_, idx) => (
          <button
            key={idx}
            type="button"
            aria-current={idx === currentStepIdx ? "step" : undefined}
            aria-label={`Go to ${provider.name} setup step ${idx + 1}: ${setupSteps[idx]?.title ?? "Untitled step"}`}
            onClick={() => {
              if (idx <= currentStepIdx) setCurrentStepIdx(idx);
            }}
            className={cn(
              "h-1 rounded-full transition-all",
              idx === currentStepIdx
                ? "bg-foreground w-6"
                : idx < currentStepIdx
                  ? "bg-foreground/40 hover:bg-foreground/60 w-4 cursor-pointer"
                  : "bg-border w-4",
            )}
          />
        ))}
        <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
          {currentStepIdx + 1}/{setupSteps.length}
        </span>
      </div>

      <div className="relative flex-1 overflow-hidden">
        <div
          className="flex h-full transition-transform duration-300 ease-in-out"
          style={{ transform: `translateX(-${currentStepIdx * 100}%)` }}
        >
          {setupSteps.map((step, idx) => (
            <div
              key={idx}
              className="w-full shrink-0 space-y-3 overflow-y-auto px-6 pb-4"
            >
              <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                Step {idx + 1}
              </p>
              <h4 className="text-foreground text-base font-medium">
                {step.title}
              </h4>
              {step.description ? (
                <p className="text-muted-foreground text-sm leading-relaxed">
                  {step.description}
                </p>
              ) : null}

              {step.helpLink
                ? (() => {
                    const { url, linkLabel, sentence } = step.helpLink;
                    const [before, after] = sentence.split("{LINK}", 2);
                    return (
                      <p className="text-muted-foreground text-sm leading-relaxed">
                        {before}
                        <a
                          href={url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-foreground underline underline-offset-4"
                        >
                          {linkLabel}
                        </a>
                        {after}
                      </p>
                    );
                  })()
                : null}

              {step.screenshot ? (
                <figure className="border-border !my-6 overflow-hidden border">
                  <img
                    src={step.screenshot.src}
                    alt={step.screenshot.alt}
                    className="w-full"
                  />
                  {step.screenshot.caption ? (
                    <figcaption className="border-border bg-secondary/40 text-muted-foreground border-t px-3 py-2 text-xs leading-relaxed">
                      {step.screenshot.caption}
                    </figcaption>
                  ) : null}
                </figure>
              ) : null}

              {(step.showsForm ?? idx === setupSteps.length - 1) ? (
                <div className="!mt-6 space-y-4">
                  <Field>
                    <FieldLabel htmlFor={apiKeyFieldId}>
                      {provider.apiKeyLabel}
                    </FieldLabel>
                    <Input
                      id={apiKeyFieldId}
                      placeholder={
                        form.hasSavedKey
                          ? "•••••• (saved)"
                          : provider.apiKeyPlaceholder
                      }
                      value={form.apiKey}
                      onChange={(e) => form.setApiKey(e.target.value)}
                      type="password"
                      disabled={form.isLoading || form.isMutating}
                    />
                    {provider.helpText ? (
                      <p className="text-muted-foreground !mt-3 text-xs leading-relaxed">
                        {provider.helpText}
                      </p>
                    ) : null}
                  </Field>

                  {provider.requiresOrganizationId ? (
                    <Field>
                      <FieldLabel htmlFor={orgIdFieldId}>
                        {provider.organizationIdLabel ?? "Organization ID"}
                      </FieldLabel>
                      <Input
                        id={orgIdFieldId}
                        placeholder={provider.organizationIdPlaceholder}
                        value={form.organizationId}
                        onChange={(e) => form.setOrganizationId(e.target.value)}
                        disabled={form.isLoading || form.isMutating}
                      />
                    </Field>
                  ) : null}
                </div>
              ) : null}
            </div>
          ))}
        </div>
      </div>

      <div className="border-border flex items-center justify-between border-t px-6 py-4">
        <Button
          variant="tertiary"
          size="sm"
          disabled={form.isMutating}
          onClick={() => {
            if (currentStepIdx > 0) {
              setCurrentStepIdx((prev) => prev - 1);
            } else {
              onClose();
            }
          }}
        >
          <Button.LeftIcon>
            <ArrowLeft className="h-3 w-3" />
          </Button.LeftIcon>
          <Button.Text>Back</Button.Text>
        </Button>
        {showForm ? (
          <Button
            variant="primary"
            size="sm"
            disabled={!form.canSave}
            onClick={form.save}
          >
            {form.isMutating ? (
              <Button.LeftIcon>
                <Loader2 className="h-3 w-3 animate-spin" />
              </Button.LeftIcon>
            ) : null}
            <Button.Text>{form.isMutating ? "Saving..." : "Save"}</Button.Text>
          </Button>
        ) : (
          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              if (isLastStep) {
                onClose();
              } else {
                setCurrentStepIdx((prev) => prev + 1);
              }
            }}
          >
            <Button.Text>{isLastStep ? "Done" : "Next step"}</Button.Text>
          </Button>
        )}
      </div>
    </>
  );
}
