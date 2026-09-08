import { useEffect, useRef, useState } from "react";
import { ArrowLeft, Ban, ChevronRight } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { AGENT_PLATFORMS } from "../setup-data";
import type { AgentPlatform } from "../types";
import { PlatformSetupStepBody } from "./platform-setup-steps";
import { usePlatformApiKeys } from "./platform-setup-values";

interface PlatformInstrumentationSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// Instrumentation instructions for one agent platform at a time, opened from
// a surface that has no platform in hand — so the sheet's first step is always
// the picker. The setup card sections instead lay a single platform's steps
// out inline with PlatformSetupFlow.
export function PlatformInstrumentationSheet({
  open,
  onOpenChange,
}: PlatformInstrumentationSheetProps): JSX.Element {
  const [pickedPlatformId, setPickedPlatformId] = useState<string | null>(null);
  const [activeStepIndex, setActiveStepIndex] = useState<
    Record<string, number>
  >({});
  const [eligibility, setEligibility] = useState<Record<string, boolean>>({});
  const apiKeys = usePlatformApiKeys();

  const activePlatform =
    AGENT_PLATFORMS.find((p) => p.id === pickedPlatformId) ?? null;
  const availablePlatforms = AGENT_PLATFORMS.filter(
    (p) => p.available !== false,
  );

  const gate = activePlatform?.setupSteps[0]?.eligibility;
  const gated = !!gate && eligibility[activePlatform!.id] !== true;

  // Mint the key the snippets need as soon as a platform is picked, unless its
  // first step still has to establish the org qualifies. Guarded by a ref so a
  // failed mint isn't retried on every render.
  const { ensure } = apiKeys;
  const initializedPlatform = useRef<string | null>(null);
  useEffect(() => {
    if (!open) {
      initializedPlatform.current = null;
      return;
    }
    if (!activePlatform || gated) return;
    if (initializedPlatform.current === activePlatform.id) return;
    initializedPlatform.current = activePlatform.id;
    ensure(activePlatform);
  }, [open, activePlatform, gated, ensure]);

  const advanceStep = (platform: AgentPlatform) => {
    const currentIdx = activeStepIndex[platform.id] ?? 0;
    if (currentIdx < platform.setupSteps.length - 1) {
      setActiveStepIndex((prev) => ({
        ...prev,
        [platform.id]: currentIdx + 1,
      }));
    } else {
      onOpenChange(false);
    }
  };

  const goBackStep = () => {
    if (!activePlatform) return;
    const currentIdx = activeStepIndex[activePlatform.id] ?? 0;
    if (currentIdx > 0) {
      setActiveStepIndex((prev) => ({
        ...prev,
        [activePlatform.id]: currentIdx - 1,
      }));
    } else {
      setPickedPlatformId(null);
    }
  };

  // The picker is step 1, so a platform's own steps start at 2.
  const PICKER_OFFSET = 1;
  const currentStepIdx = activePlatform
    ? (activeStepIndex[activePlatform.id] ?? 0)
    : 0;
  const totalSteps =
    PICKER_OFFSET + Math.max(activePlatform?.setupSteps.length ?? 0, 1);
  const overallStepIndex = !activePlatform ? 0 : PICKER_OFFSET + currentStepIdx;

  const goToDot = (idx: number) => {
    if (idx >= overallStepIndex) return;
    if (idx === 0) {
      setPickedPlatformId(null);
      return;
    }
    if (activePlatform) {
      setActiveStepIndex((prev) => ({
        ...prev,
        [activePlatform.id]: idx - PICKER_OFFSET,
      }));
    }
  };

  const renderPicker = () => (
    <>
      <SheetHeader className="sr-only">
        <SheetTitle>Choose a platform</SheetTitle>
        <SheetDescription>
          Pick which AI coding assistant you're setting up.
        </SheetDescription>
      </SheetHeader>
      <div className="w-full min-w-0 shrink-0 space-y-4 overflow-y-auto px-6 pb-6">
        <div>
          <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
            Step 1
          </p>
          <h3 className="text-foreground mt-1 text-lg font-semibold">
            Choose a platform
          </h3>
          <p className="text-muted-foreground mt-1 text-sm">
            Instructions differ per agent — pick which one you're setting up.
          </p>
        </div>
        <div className="space-y-2">
          {availablePlatforms.map((platform) => (
            <button
              key={platform.id}
              type="button"
              onClick={() => setPickedPlatformId(platform.id)}
              className="border-border bg-card hover:border-foreground/20 flex w-full items-center gap-4 border p-4 text-left transition-all"
            >
              <div className="bg-secondary flex h-10 w-10 flex-shrink-0 items-center justify-center">
                <AgentProviderIcon source={platform.icon} className="h-5 w-5" />
              </div>
              <div className="min-w-0 flex-1 space-y-1">
                <p className="text-foreground text-sm font-medium">
                  {platform.name}
                </p>
                <p className="text-muted-foreground text-xs">
                  {platform.description}
                </p>
              </div>
              <ChevronRight className="text-muted-foreground h-4 w-4 flex-shrink-0" />
            </button>
          ))}
        </div>
      </div>
    </>
  );

  const renderPlatformSteps = () => {
    if (!activePlatform) return null;

    const blocked = eligibility[activePlatform.id] === false ? gate : undefined;
    if (blocked) {
      return (
        <>
          <SheetHeader className="sr-only">
            <SheetTitle>Set up {activePlatform.name}</SheetTitle>
            <SheetDescription>{activePlatform.description}</SheetDescription>
          </SheetHeader>
          <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 pb-6 text-center">
            <div className="bg-destructive/10 text-destructive flex h-12 w-12 items-center justify-center rounded-full">
              <Ban className="h-6 w-6" />
            </div>
            <h4 className="text-foreground text-base font-medium">
              {blocked.blockedTitle}
            </h4>
            <p className="text-muted-foreground max-w-sm text-sm leading-relaxed">
              {blocked.blockedDescription}
            </p>
          </div>
          <div className="border-border flex items-center justify-end border-t px-6 py-4">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => onOpenChange(false)}
            >
              <Button.Text>Close</Button.Text>
            </Button>
          </div>
        </>
      );
    }

    const stepCount = activePlatform.setupSteps.length;
    const currentStep = activePlatform.setupSteps[currentStepIdx];
    const isLastStep = currentStepIdx === stepCount - 1;

    return (
      <>
        <SheetHeader className="sr-only">
          <SheetTitle>Set up {activePlatform.name}</SheetTitle>
          <SheetDescription>{activePlatform.description}</SheetDescription>
        </SheetHeader>

        <div className="relative flex-1 overflow-hidden">
          <div
            className="flex h-full transition-transform duration-300 ease-in-out"
            style={{ transform: `translateX(-${currentStepIdx * 100}%)` }}
          >
            {activePlatform.setupSteps.map((step, idx) => (
              <div
                key={step.title}
                className="w-full shrink-0 overflow-y-auto px-6 pb-4"
              >
                <PlatformSetupStepBody
                  step={step}
                  eyebrow={`Step ${PICKER_OFFSET + idx + 1}`}
                  apiKey={apiKeys.keys[activePlatform.id]}
                  apiKeyPending={apiKeys.pending[activePlatform.id]}
                  apiKeyError={apiKeys.errors[activePlatform.id]}
                  onRetryApiKey={() => ensure(activePlatform)}
                  onEligibilityAnswer={(answer) => {
                    setEligibility((prev) => ({
                      ...prev,
                      [activePlatform.id]: answer,
                    }));
                    if (answer) {
                      ensure(activePlatform);
                      advanceStep(activePlatform);
                    }
                  }}
                />
              </div>
            ))}
          </div>
        </div>

        {!currentStep?.eligibility && (
          <div className="border-border flex items-center justify-between border-t px-6 py-4">
            <Button variant="tertiary" size="sm" onClick={goBackStep}>
              <Button.LeftIcon>
                <ArrowLeft className="h-3 w-3" />
              </Button.LeftIcon>
              <Button.Text>Back</Button.Text>
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={() => advanceStep(activePlatform)}
            >
              <Button.Text>{isLastStep ? "Done" : "Next step"}</Button.Text>
            </Button>
          </div>
        )}
      </>
    );
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
      >
        <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
          {Array.from({ length: totalSteps }, (_, idx) => (
            <button
              key={idx}
              type="button"
              onClick={() => goToDot(idx)}
              className={cn(
                "h-1 rounded-full transition-all",
                idx === overallStepIndex
                  ? "bg-foreground w-6"
                  : idx < overallStepIndex
                    ? "bg-foreground/40 hover:bg-foreground/60 w-4 cursor-pointer"
                    : "bg-border w-4",
              )}
            />
          ))}
          <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
            {overallStepIndex + 1}/{totalSteps}
          </span>
        </div>
        {!activePlatform ? renderPicker() : renderPlatformSteps()}
      </SheetContent>
    </Sheet>
  );
}
