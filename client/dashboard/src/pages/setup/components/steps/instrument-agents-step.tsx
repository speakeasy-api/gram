import { useEffect, useState } from "react";
import {
  Terminal,
  Check,
  Copy,
  ChevronRight,
  ArrowLeft,
  Loader2,
  AlertCircle,
  Ban,
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
import { StepContainer } from "../step-container";
import { AGENT_PLATFORMS } from "../../setup-data";
import type { AgentPlatform, PlatformSetupStatus } from "../../types";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { Badge, Button, Link } from "@speakeasy-api/moonshine";
import { cn } from "@/lib/utils";

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
}) {
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

interface InstrumentAgentsStepProps {
  onComplete: () => void;
  onBack: () => void;
}

const PLATFORM_LOGOS: Record<string, string> = {
  claude: "/icons/platforms/claude.svg",
  "claude-cowork": "/icons/platforms/claude.svg",
  codex: "/icons/platforms/openai.svg",
  cursor: "/icons/platforms/cursor.svg",
};

export function InstrumentAgentsStep({
  onComplete,
  onBack,
}: InstrumentAgentsStepProps): JSX.Element {
  const [drawerPlatformId, setDrawerPlatformId] = useState<string | null>(null);
  const [platformStatus, setPlatformStatus] = useState<
    Record<string, PlatformSetupStatus>
  >(() =>
    Object.fromEntries(AGENT_PLATFORMS.map((p) => [p.id, "not_started"])),
  );
  const [activeStepIndex, setActiveStepIndex] = useState<
    Record<string, number>
  >(() => Object.fromEntries(AGENT_PLATFORMS.map((p) => [p.id, 0])));
  const [copiedField, setCopiedField] = useState<string | null>(null);
  const [apiKeys, setApiKeys] = useState<Record<string, string>>({});
  const [apiKeyPending, setApiKeyPending] = useState<Record<string, boolean>>(
    {},
  );
  const [apiKeyError, setApiKeyError] = useState<Record<string, string>>({});
  const [platformBlocked, setPlatformBlocked] = useState<
    Record<string, { title: string; description: string }>
  >({});

  const { data: publishStatus } = usePublishStatus();
  const { orgSlug = "" } = useSlugs();
  const repoOwner = publishStatus?.repoOwner ?? "";
  const repoName = publishStatus?.repoName ?? "";
  const marketplaceUrl = publishStatus?.marketplaceUrl ?? "";
  // Use server-provided repoUrl when present rather than reconstructing it
  // client-side — keeps the wizard in lock-step with whatever URL format the
  // publish path actually wrote (e.g. enterprise GitHub hosts).
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

  const availablePlatforms = AGENT_PLATFORMS.filter(
    (p) => p.available !== false,
  );
  const comingSoonPlatforms = AGENT_PLATFORMS.filter(
    (p) => p.available === false,
  );
  const completedCount = availablePlatforms.filter(
    (p) => platformStatus[p.id] === "complete",
  ).length;

  const activePlatform =
    AGENT_PLATFORMS.find((p) => p.id === drawerPlatformId) ?? null;

  const ensureApiKey = (platform: AgentPlatform) => {
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
    // wizard run. The timestamp suffix guarantees uniqueness and tells admins
    // which run produced each entry in the API Keys list.
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
            err instanceof Error ? err.message : "Failed to generate API key.";
          setApiKeyError((prev) => ({ ...prev, [platform.id]: msg }));
          toast.error(`Failed to generate API key: ${msg}`);
        },
      },
    );
  };

  const openDrawer = (platformId: string) => {
    setDrawerPlatformId(platformId);
    if (platformStatus[platformId] === "not_started") {
      setPlatformStatus((prev) => ({ ...prev, [platformId]: "in_progress" }));
    }
    const platform = AGENT_PLATFORMS.find((p) => p.id === platformId);
    if (!platform) return;
    // Skip key generation if an unresolved eligibility step gates the flow;
    // handleEligibilityYes triggers it after the user confirms.
    const firstStep = platform.setupSteps[0];
    if (
      firstStep?.eligibility &&
      platformStatus[platformId] === "not_started"
    ) {
      return;
    }
    ensureApiKey(platform);
  };

  const closeDrawer = () => {
    setDrawerPlatformId(null);
  };

  const advanceStep = (platform: AgentPlatform) => {
    const currentIdx = activeStepIndex[platform.id] ?? 0;
    if (currentIdx < platform.setupSteps.length - 1) {
      setActiveStepIndex((prev) => ({
        ...prev,
        [platform.id]: currentIdx + 1,
      }));
    } else {
      setPlatformStatus((prev) => ({ ...prev, [platform.id]: "complete" }));
      closeDrawer();
    }
  };

  const goBackStep = (platformId: string) => {
    const currentIdx = activeStepIndex[platformId] ?? 0;
    if (currentIdx > 0) {
      setActiveStepIndex((prev) => ({
        ...prev,
        [platformId]: currentIdx - 1,
      }));
    }
  };

  const copyToClipboard = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  const statusBadge = (status: PlatformSetupStatus) => {
    switch (status) {
      case "complete":
        return (
          <Badge variant="success" background>
            <Badge.Text>Complete</Badge.Text>
          </Badge>
        );
      case "in_progress":
        return (
          <Badge variant="neutral" background>
            <Badge.Text>In progress</Badge.Text>
          </Badge>
        );
      case "blocked":
        return (
          <Badge variant="destructive" background>
            <Badge.Text>Not eligible</Badge.Text>
          </Badge>
        );
      case "not_started":
      default:
        return null;
    }
  };

  const handleEligibilityNo = (
    platform: AgentPlatform,
    blocked: { title: string; description: string },
  ) => {
    setPlatformBlocked((prev) => ({ ...prev, [platform.id]: blocked }));
    setPlatformStatus((prev) => ({ ...prev, [platform.id]: "blocked" }));
  };

  const handleEligibilityYes = (platform: AgentPlatform) => {
    setPlatformBlocked((prev) => {
      const next = { ...prev };
      delete next[platform.id];
      return next;
    });
    if (platformStatus[platform.id] === "blocked") {
      setPlatformStatus((prev) => ({ ...prev, [platform.id]: "in_progress" }));
    }
    ensureApiKey(platform);
    advanceStep(platform);
  };

  const renderDrawerContent = () => {
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
            <Button variant="secondary" size="sm" onClick={closeDrawer}>
              <Button.Text>Close</Button.Text>
            </Button>
          </div>
        </>
      );
    }

    const currentStepIdx = activeStepIndex[activePlatform.id] ?? 0;
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

        <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
          {activePlatform.setupSteps.map((_, idx) => (
            <button
              key={idx}
              type="button"
              onClick={() => {
                void (
                  idx <= currentStepIdx &&
                  setActiveStepIndex((prev) => ({
                    ...prev,
                    [activePlatform.id]: idx,
                  }))
                );
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
            {currentStepIdx + 1}/{stepCount}
          </span>
        </div>

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
                    Step {idx + 1}
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
              disabled={currentStepIdx === 0}
              onClick={() => goBackStep(activePlatform.id)}
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
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Terminal className="text-foreground h-6 w-6" />
        </div>
      }
      title="Instrument agent platforms"
      description="Set up Speakeasy hooks for each AI coding assistant your team uses. Each platform has its own configuration steps."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground text-sm">
            {completedCount} of {availablePlatforms.length} platforms configured
          </span>
        </div>

        {availablePlatforms.map((platform) => {
          const status = platformStatus[platform.id] ?? "not_started";

          return (
            <button
              key={platform.id}
              type="button"
              onClick={() => openDrawer(platform.id)}
              className={cn(
                "flex w-full items-center gap-4 rounded-lg border p-4 text-left transition-all",
                status === "complete"
                  ? "border-foreground/10 bg-secondary/20"
                  : "border-border bg-card hover:border-foreground/20",
              )}
            >
              <div
                className={cn(
                  "flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg",
                  status === "complete" ? "bg-foreground/10" : "bg-secondary",
                )}
              >
                {PLATFORM_LOGOS[platform.id] ? (
                  <img
                    src={PLATFORM_LOGOS[platform.id]}
                    alt={platform.name}
                    className="h-5 w-5"
                  />
                ) : (
                  <span className="text-foreground text-sm font-semibold">
                    {platform.name.charAt(0)}
                  </span>
                )}
              </div>
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex items-center gap-2">
                  <p className="text-foreground text-sm font-medium">
                    {platform.name}
                  </p>
                  {statusBadge(status)}
                </div>
                <p className="text-muted-foreground text-xs">
                  {platform.description}
                </p>
              </div>
              <ChevronRight className="text-muted-foreground h-4 w-4 flex-shrink-0" />
            </button>
          );
        })}

        {comingSoonPlatforms.length > 0 && (
          <div className="pt-3">
            <p className="text-muted-foreground mb-2 text-[11px] font-medium tracking-wider uppercase">
              Coming soon
            </p>
            <div className="grid grid-cols-2 gap-2">
              {comingSoonPlatforms.map((platform) => (
                <div
                  key={platform.id}
                  aria-disabled
                  className="border-border bg-card flex cursor-not-allowed items-center gap-3 rounded-lg border p-3 opacity-50"
                >
                  <div className="bg-secondary flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md">
                    <HookSourceIcon source={platform.id} className="h-4 w-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="text-foreground truncate text-sm font-medium">
                      {platform.name}
                    </p>
                    <p className="text-muted-foreground truncate text-xs">
                      {platform.description}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      <Sheet
        open={!!drawerPlatformId}
        onOpenChange={(open) => {
          if (!open) closeDrawer();
        }}
      >
        <SheetContent
          side="right"
          className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
        >
          {renderDrawerContent()}
        </SheetContent>
      </Sheet>
    </StepContainer>
  );
}
