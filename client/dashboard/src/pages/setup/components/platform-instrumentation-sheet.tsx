import { useCallback, useEffect, useRef, useState } from "react";
import {
  Check,
  Copy,
  ArrowLeft,
  Loader2,
  AlertCircle,
  Ban,
  ChevronRight,
} from "lucide-react";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { useSlugs } from "@/contexts/Sdk";
import { toast } from "sonner";
import { codeToHtml, type BundledLanguage } from "shiki";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { AGENT_PLATFORMS } from "../setup-data";
import type { AgentPlatform, PlatformSetupStatus } from "../types";
import { Button, Link } from "@speakeasy-api/moonshine";
import { cn } from "@/lib/utils";
import { PLATFORM_LOGOS, INVERT_LOGO_IN_DARK } from "./platform-logos";

const API_KEY_PLACEHOLDER = "{{GRAM_API_KEY}}";
const MARKETPLACE_URL_PLACEHOLDER = "{{GRAM_MARKETPLACE_URL}}";
const REPO_URL_PLACEHOLDER = "{{GRAM_REPO_URL}}";
const REPO_NAME_PLACEHOLDER = "{{GRAM_REPO_NAME}}";
const REPO_OWNER_PLACEHOLDER = "{{GRAM_REPO_OWNER}}";
const CODEX_PLUGIN_NAME_PLACEHOLDER = "{{GRAM_CODEX_PLUGIN_NAME}}";
const CLAUDE_PLUGIN_NAME_PLACEHOLDER = "{{GRAM_CLAUDE_PLUGIN_NAME}}";
const CURSOR_PLUGIN_NAME_PLACEHOLDER = "{{GRAM_CURSOR_PLUGIN_NAME}}";
const MARKETPLACE_NAME_PLACEHOLDER = "{{GRAM_MARKETPLACE_NAME}}";

function HighlightedCode({
  code,
  language,
}: {
  code: string;
  language?: string;
}): JSX.Element {
  const [html, setHtml] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    codeToHtml(code, {
      lang: (language as BundledLanguage) ?? "text",
      theme: "github-dark-default",
      transformers: [
        {
          pre(node) {
            node.properties.class =
              "px-3 pb-3 text-[13px] leading-relaxed whitespace-pre-wrap break-all max-h-[400px] overflow-y-auto !bg-transparent";
          },
        },
      ],
    })
      .then((out) => {
        if (!cancelled) setHtml(out);
      })
      .catch(() => {
        if (!cancelled) setHtml(null);
      });
    return () => {
      cancelled = true;
    };
  }, [code, language]);

  if (html) {
    return (
      <div
        className="text-zinc-200"
        dangerouslySetInnerHTML={{ __html: html }}
      />
    );
  }
  return (
    <pre className="max-h-[400px] overflow-y-auto px-3 pb-3 text-[13px] leading-relaxed break-all whitespace-pre-wrap">
      <code className="text-zinc-200">{code}</code>
    </pre>
  );
}

interface PlatformInstrumentationSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * Pre-selected platform, e.g. the onboarding wizard's own platform grid
   * chose one before opening the sheet. When omitted, the sheet's first step
   * is a platform picker — instructions differ per agent, so callers that
   * don't already know which one (the Observability plugin card) need it
   * asked inside the sheet instead of guessing.
   */
  initialPlatformId?: string;
  /**
   * Notified whenever a platform's status changes, so callers that track
   * cross-platform progress (the onboarding wizard's grid + "N of M
   * configured" counter) can mirror it. The sheet always tracks status
   * internally regardless — this is purely for external callers to observe.
   */
  onPlatformStatusChange?: (
    platformId: string,
    status: PlatformSetupStatus,
  ) => void;
}

export function PlatformInstrumentationSheet({
  open,
  onOpenChange,
  initialPlatformId,
  onPlatformStatusChange,
}: PlatformInstrumentationSheetProps): JSX.Element {
  const hasPicker = !initialPlatformId;
  const [pickedPlatformId, setPickedPlatformId] = useState<string | null>(null);
  const [activeStepIndex, setActiveStepIndex] = useState<
    Record<string, number>
  >({});
  const [copiedField, setCopiedField] = useState<string | null>(null);
  const [apiKeys, setApiKeys] = useState<Record<string, string>>({});
  const [apiKeyPending, setApiKeyPending] = useState<Record<string, boolean>>(
    {},
  );
  const [apiKeyError, setApiKeyError] = useState<Record<string, string>>({});
  const [platformStatus, setPlatformStatus] = useState<
    Record<string, PlatformSetupStatus>
  >({});
  const [platformBlocked, setPlatformBlocked] = useState<
    Record<string, { title: string; description: string }>
  >({});

  const { data: publishStatus } = usePublishStatus();
  const { orgSlug = "" } = useSlugs();
  const repoOwner = publishStatus?.repoOwner ?? "";
  const repoName = publishStatus?.repoName ?? "";
  const marketplaceUrl = publishStatus?.marketplaceUrl ?? "";
  const repoUrl = publishStatus?.repoUrl ?? "";
  // Marketplace + plugin slug formulas mirror server/internal/plugins/naming/naming.go.
  // Both sides derive from the org slug — NOT the GitHub repo name, which can
  // be anything (e.g. "speakeasy-default-plugins"). The marketplace.json "name"
  // field is what `enabledPlugins`/`plugins.required` reference as the
  // `<plugin>@<marketplace>` suffix, and it's always `<orgSlug>-gram`.
  const marketplaceName = orgSlug ? `${orgSlug}-gram` : "";
  const claudePluginName = orgSlug ? `${orgSlug}-observability` : "";
  const codexPluginName = orgSlug ? `${orgSlug}-observability-codex` : "";
  const cursorPluginName = orgSlug ? `${orgSlug}-observability-cursor` : "";

  const createKeyMutation = useCreateAPIKeyMutation();

  const activePlatformId = initialPlatformId ?? pickedPlatformId;
  const activePlatform =
    AGENT_PLATFORMS.find((p) => p.id === activePlatformId) ?? null;
  const availablePlatforms = AGENT_PLATFORMS.filter(
    (p) => p.available !== false,
  );

  const updateStatus = useCallback(
    (platformId: string, status: PlatformSetupStatus) => {
      setPlatformStatus((prev) => ({ ...prev, [platformId]: status }));
      onPlatformStatusChange?.(platformId, status);
    },
    [onPlatformStatusChange],
  );

  const ensureApiKey = useCallback(
    (platform: AgentPlatform) => {
      const needsKey = platform.setupSteps.some((s) => s.requiresApiKey);
      if (!needsKey) return;
      if (apiKeys[platform.id] || apiKeyPending[platform.id]) return;

      setApiKeyPending((prev) => ({ ...prev, [platform.id]: true }));
      setApiKeyError((prev) => {
        const next = { ...prev };
        delete next[platform.id];
        return next;
      });

      // The api_keys table enforces UNIQUE (organization_id, name) on
      // non-deleted rows, and key tokens are only returned on creation (never
      // re-readable via listKeys). Both constraints mean we can't "fetch the
      // existing key" — we have to mint a fresh one with a unique name on each
      // run. The timestamp suffix guarantees uniqueness and tells admins which
      // run produced each entry in the API Keys list.
      const timestamp = new Date().toISOString().slice(0, 19).replace("T", " ");
      createKeyMutation.mutate(
        {
          security: { sessionHeaderGramSession: "" },
          request: {
            createKeyForm: {
              name: `${platform.name} hooks (setup ${timestamp})`,
              scopes: ["hooks"],
            },
          },
        },
        {
          onSuccess: (data) => {
            setApiKeyPending((prev) => ({ ...prev, [platform.id]: false }));
            if (data.key) {
              setApiKeys((prev) => ({ ...prev, [platform.id]: data.key! }));
            } else {
              setApiKeyError((prev) => ({
                ...prev,
                [platform.id]: "API key token missing from response.",
              }));
            }
          },
          onError: (err) => {
            setApiKeyPending((prev) => ({ ...prev, [platform.id]: false }));
            const msg =
              err instanceof Error
                ? err.message
                : "Failed to generate API key.";
            setApiKeyError((prev) => ({ ...prev, [platform.id]: msg }));
            toast.error(`Failed to generate API key: ${msg}`);
          },
        },
      );
    },
    [apiKeys, apiKeyPending, createKeyMutation],
  );

  // Mirrors the wizard's old openDrawer: mint the platform's API key as soon
  // as it becomes active, unless its first step gates on an unconfirmed
  // eligibility question. Guarded by a ref (not just the dependency array) so
  // it fires once per platform activation rather than on every render that
  // happens to redefine updateStatus/ensureApiKey.
  const initializedPlatformRef = useRef<string | null>(null);
  useEffect(() => {
    if (!open) {
      initializedPlatformRef.current = null;
      return;
    }
    if (!activePlatform) return;
    if (initializedPlatformRef.current === activePlatform.id) return;
    initializedPlatformRef.current = activePlatform.id;

    const currentStatus = platformStatus[activePlatform.id] ?? "not_started";
    if (currentStatus === "not_started") {
      updateStatus(activePlatform.id, "in_progress");
    }
    const firstStep = activePlatform.setupSteps[0];
    if (firstStep?.eligibility && currentStatus === "not_started") return;
    ensureApiKey(activePlatform);
  }, [open, activePlatform, platformStatus, updateStatus, ensureApiKey]);

  const advanceStep = (platform: AgentPlatform) => {
    const currentIdx = activeStepIndex[platform.id] ?? 0;
    if (currentIdx < platform.setupSteps.length - 1) {
      setActiveStepIndex((prev) => ({
        ...prev,
        [platform.id]: currentIdx + 1,
      }));
    } else {
      updateStatus(platform.id, "complete");
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
    } else if (hasPicker) {
      setPickedPlatformId(null);
    }
  };

  const copyToClipboard = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  const handleEligibilityNo = (
    platform: AgentPlatform,
    blocked: { title: string; description: string },
  ) => {
    setPlatformBlocked((prev) => ({ ...prev, [platform.id]: blocked }));
    updateStatus(platform.id, "blocked");
  };

  const handleEligibilityYes = (platform: AgentPlatform) => {
    setPlatformBlocked((prev) => {
      const next = { ...prev };
      delete next[platform.id];
      return next;
    });
    if ((platformStatus[platform.id] ?? "not_started") === "blocked") {
      updateStatus(platform.id, "in_progress");
    }
    ensureApiKey(platform);
    advanceStep(platform);
  };

  const pickerOffset = hasPicker ? 1 : 0;
  const platformStepCount = activePlatform?.setupSteps.length ?? 0;
  const totalSteps = pickerOffset + Math.max(platformStepCount, 1);
  const currentStepIdx = activePlatform
    ? (activeStepIndex[activePlatform.id] ?? 0)
    : 0;
  const overallStepIndex = !activePlatform ? 0 : pickerOffset + currentStepIdx;

  const goToDot = (idx: number) => {
    if (idx >= overallStepIndex) return;
    if (hasPicker && idx === 0) {
      setPickedPlatformId(null);
      return;
    }
    if (activePlatform) {
      setActiveStepIndex((prev) => ({
        ...prev,
        [activePlatform.id]: idx - pickerOffset,
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
              className="border-border bg-card hover:border-foreground/20 flex w-full items-center gap-4 rounded-lg border p-4 text-left transition-all"
            >
              <div className="bg-secondary flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg">
                {PLATFORM_LOGOS[platform.id] ? (
                  <img
                    src={PLATFORM_LOGOS[platform.id]}
                    alt={platform.name}
                    className={cn(
                      "h-5 w-5",
                      INVERT_LOGO_IN_DARK.has(platform.id) && "dark:invert",
                    )}
                  />
                ) : (
                  <span className="text-foreground text-sm font-semibold">
                    {platform.name.charAt(0)}
                  </span>
                )}
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

    const blocked = platformBlocked[activePlatform.id];
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
              {blocked.title}
            </h4>
            <p className="text-muted-foreground max-w-sm text-sm leading-relaxed">
              {blocked.description}
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
    const isEligibilityStep = !!currentStep?.eligibility;

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
            {activePlatform.setupSteps.map((step, idx) => {
              const platformKey = apiKeys[activePlatform.id];
              const isPending = !!apiKeyPending[activePlatform.id];
              const error = apiKeyError[activePlatform.id];
              const needsKey = !!step.requiresApiKey;
              const substitutions: Array<[string, string]> = [
                [API_KEY_PLACEHOLDER, platformKey ?? ""],
                [MARKETPLACE_URL_PLACEHOLDER, marketplaceUrl],
                [REPO_URL_PLACEHOLDER, repoUrl],
                [REPO_NAME_PLACEHOLDER, repoName],
                [REPO_OWNER_PLACEHOLDER, repoOwner],
                [CODEX_PLUGIN_NAME_PLACEHOLDER, codexPluginName],
                [CLAUDE_PLUGIN_NAME_PLACEHOLDER, claudePluginName],
                [CURSOR_PLUGIN_NAME_PLACEHOLDER, cursorPluginName],
                [MARKETPLACE_NAME_PLACEHOLDER, marketplaceName],
              ];
              let displayCode = step.code;
              if (displayCode) {
                for (const [marker, value] of substitutions) {
                  displayCode = displayCode.split(marker).join(value);
                }
              }
              // Don't render the code block when a step depends on the API key
              // and we haven't yet minted one — otherwise users would copy a
              // snippet with an empty Gram-Key value.
              const codeBlockReady = !needsKey || !!platformKey;

              return (
                <div
                  key={idx}
                  className="w-full shrink-0 space-y-3 overflow-y-auto px-6 pb-4"
                >
                  <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                    Step {pickerOffset + idx + 1}
                  </p>
                  <h4 className="text-foreground text-base font-medium">
                    {step.title}
                  </h4>
                  {step.screenshot && (
                    <figure className="border-border !my-6 overflow-hidden rounded-md border">
                      <img
                        src={step.screenshot.src}
                        alt={step.screenshot.alt}
                        className="w-full"
                      />
                      {step.screenshot.caption && (
                        <figcaption className="border-border bg-secondary/40 text-muted-foreground border-t px-3 py-2 text-xs leading-relaxed">
                          {step.screenshot.caption}
                        </figcaption>
                      )}
                    </figure>
                  )}
                  {step.description && (
                    <p className="text-muted-foreground text-sm leading-relaxed">
                      {step.description}
                    </p>
                  )}

                  {step.helpLink &&
                    (() => {
                      const { url, linkLabel, sentence } = step.helpLink;
                      const [before, after] = sentence.split("{LINK}", 2);
                      return (
                        <p className="text-muted-foreground text-sm leading-relaxed">
                          {before}
                          <Link
                            href={url}
                            target="_blank"
                            rel="noopener noreferrer"
                            size="sm"
                            iconSuffixName="external-link"
                          >
                            {linkLabel}
                          </Link>
                          {after}
                        </p>
                      );
                    })()}

                  {step.eligibility && (
                    <div className="bg-secondary/40 border-border !mt-6 space-y-4 rounded-lg border p-4">
                      <p className="text-foreground text-sm font-medium">
                        {step.eligibility.question}
                      </p>
                      <div className="flex gap-2">
                        <Button
                          variant="primary"
                          size="sm"
                          className="flex-1"
                          onClick={() => handleEligibilityYes(activePlatform)}
                        >
                          <Button.Text>
                            {step.eligibility.yesLabel ?? "Yes"}
                          </Button.Text>
                        </Button>
                        <Button
                          variant="secondary"
                          size="sm"
                          className="flex-1"
                          onClick={() =>
                            handleEligibilityNo(activePlatform, {
                              title: step.eligibility!.blockedTitle,
                              description: step.eligibility!.blockedDescription,
                            })
                          }
                        >
                          <Button.Text>
                            {step.eligibility.noLabel ?? "No"}
                          </Button.Text>
                        </Button>
                      </div>
                    </div>
                  )}

                  {needsKey && isPending && (
                    <div className="text-muted-foreground flex items-center gap-2 text-xs">
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      Generating API key…
                    </div>
                  )}

                  {needsKey && error && (
                    <div className="text-destructive bg-destructive/5 border-destructive/20 flex items-start gap-2 rounded-md border p-2.5 text-xs">
                      <AlertCircle className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
                      <div className="flex-1">
                        <p className="font-medium">
                          Couldn't generate an API key
                        </p>
                        <p className="text-muted-foreground mt-0.5">{error}</p>
                        <button
                          type="button"
                          className="text-foreground mt-1.5 underline underline-offset-2"
                          onClick={() => ensureApiKey(activePlatform)}
                        >
                          Retry
                        </button>
                      </div>
                    </div>
                  )}

                  {displayCode && codeBlockReady && (
                    <div className="overflow-hidden rounded-md bg-zinc-950">
                      <div className="flex items-center justify-between px-3 py-2.5">
                        <span className="text-[10px] tracking-wider text-zinc-500 uppercase">
                          {step.language ?? "shell"}
                        </span>
                        <button
                          type="button"
                          onClick={() => {
                            void copyToClipboard(
                              displayCode,
                              `${activePlatform.id}-${idx}`,
                            );
                          }}
                          className="flex items-center gap-1 rounded px-2 py-1 text-[11px] font-medium tracking-wider text-zinc-300 uppercase transition-colors hover:bg-zinc-800 hover:text-zinc-100"
                        >
                          {copiedField === `${activePlatform.id}-${idx}` ? (
                            <Check className="h-3 w-3" />
                          ) : (
                            <Copy className="h-3 w-3" />
                          )}
                          {copiedField === `${activePlatform.id}-${idx}`
                            ? "Copied"
                            : "Copy"}
                        </button>
                      </div>
                      <HighlightedCode
                        code={displayCode}
                        language={step.language}
                      />
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>

        {!isEligibilityStep && (
          <div className="border-border flex items-center justify-between border-t px-6 py-4">
            <Button
              variant="tertiary"
              size="sm"
              disabled={currentStepIdx === 0 && !hasPicker}
              onClick={goBackStep}
            >
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
