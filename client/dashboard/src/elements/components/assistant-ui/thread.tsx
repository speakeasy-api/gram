import {
  ArrowDownIcon,
  ArrowUpIcon,
  AtSign,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CircleIcon,
  CopyIcon,
  DownloadIcon,
  PencilIcon,
  Search,
  Settings2,
  Square,
  Wrench,
} from "lucide-react";

import {
  ActionBarPrimitive,
  BranchPickerPrimitive,
  ComposerPrimitive,
  ErrorPrimitive,
  ImageMessagePartProps,
  MessagePrimitive,
  ThreadPrimitive,
  useAssistantApi,
  useAssistantState,
} from "@assistant-ui/react";

import {
  AnimatePresence,
  LazyMotion,
  MotionConfig,
  domAnimation,
} from "motion/react";
import * as m from "motion/react-m";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FC,
} from "react";

import {
  ComposerAddAttachment,
  ComposerAttachments,
  UserMessageAttachments,
} from "@/elements/components/assistant-ui/attachment";
import { FollowOnSuggestions } from "@/elements/components/assistant-ui/follow-on-suggestions";
import { MarkdownText } from "@/elements/components/assistant-ui/markdown-text";
import { MentionedToolsBadges } from "@/elements/components/assistant-ui/mentioned-tools-badges";
import { MessageFeedback } from "@/elements/components/assistant-ui/message-feedback";
import {
  Reasoning,
  ReasoningGroup,
} from "@/elements/components/assistant-ui/reasoning";
import { ThinkingIndicator } from "@/elements/components/assistant-ui/thinking-indicator";
import { ToolFallback } from "@/elements/components/assistant-ui/tool-fallback";
import { UserMessageText } from "@/elements/components/assistant-ui/user-message-text";
import { ToolMentionAutocomplete } from "@/elements/components/assistant-ui/tool-mention-autocomplete";
import { TooltipIconButton } from "@/elements/components/assistant-ui/tooltip-icon-button";
import {
  Avatar,
  AvatarFallback,
  AvatarImage,
} from "@/elements/components/ui/avatar";
import { Button } from "@/elements/components/ui/button";
import { useChatId } from "@/elements/contexts/ChatIdContext";
import { useReplayContext } from "@/elements/contexts/ReplayContext";
import { useThreadMeta } from "@/elements/contexts/ThreadMetaContext";
import { useAuth } from "@/elements/hooks/useAuth";
import { useDensity } from "@/elements/hooks/useDensity";
import { useElements } from "@/elements/hooks/useElements";
import { isLocalThreadId } from "@/elements/hooks/useGramThreadListAdapter";
import { useRadius } from "@/elements/hooks/useRadius";
import { useRecordCassette } from "@/elements/hooks/useRecordCassette";
import { useThemeProps } from "@/elements/hooks/useThemeProps";
import { useToolMentions } from "@/elements/hooks/useToolMentions";
import { getApiUrl } from "@/elements/lib/api";
import { EASE_OUT_QUINT } from "@/elements/lib/easing";
import { MODELS } from "@/elements/lib/models";
import {
  type MentionableTool,
  toolSetToMentionableTools,
} from "@/elements/lib/tool-mentions";
import { cn, initialsOf } from "@/elements/lib/utils";
import { Popover, PopoverContent, PopoverTrigger } from "../ui/popover";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "../ui/tooltip";
import { ConnectionStatusIndicatorSafe } from "./connection-status-indicator";
import { ToolGroup } from "./tool-group";

type Feedback = "success" | "failure";

// Context for chat resolution state
const ChatResolutionContext = createContext<{
  isResolved: boolean;
  feedbackHidden: boolean;
  setResolved: () => void;
  setUnresolved: () => void;
  resetFeedbackHidden: () => void;
  submitFeedback: (feedback: Feedback) => Promise<void>;
}>({
  isResolved: false,
  feedbackHidden: false,
  setResolved: () => {},
  setUnresolved: () => {},
  resetFeedbackHidden: () => {},
  submitFeedback: async () => {},
});

const useChatResolution = () => useContext(ChatResolutionContext);

const DangerousApiKeyWarning = () => (
  <div className="m-2 rounded-md border border-red-500 bg-red-100 px-4 py-3 text-sm text-red-800 dark:border-red-600 dark:bg-red-900/30 dark:text-red-200">
    <strong>Danger:</strong> You are using a Gram API key directly in the
    browser. This exposes your key to anyone who inspects this page. Do NOT use
    this in production.
  </div>
);

interface ThreadProps {
  className?: string;
}

export const Thread: FC<ThreadProps> = ({ className }) => {
  const themeProps = useThemeProps();
  const d = useDensity();
  const { config } = useElements();
  const components = config.components ?? {};
  const showDangerousApiKeyWarning =
    config.api && "dangerousApiKey" in config.api;
  const showFeedback = config.thread?.showFeedback ?? true;
  const [isResolved, setIsResolved] = useState(false);
  const [feedbackHidden, setFeedbackHidden] = useState(false);
  const chatId = useChatId();
  // Hidden rather than disabled: the backend rejects sends into a chat the
  // caller can view (e.g. via an admin-level read grant) but didn't create,
  // so there's no valid action to leave available.
  const composerHidden = useThreadMeta(chatId ?? undefined)?.readOnly ?? false;

  const apiUrl = getApiUrl(config);
  const auth = useAuth({
    auth: config.api,
    projectSlug: config.projectSlug,
  });

  const setResolved = () => setIsResolved(true);
  const setUnresolved = () => {
    setIsResolved(false);
    setFeedbackHidden(true);
  };
  const resetFeedbackHidden = () => setFeedbackHidden(false);

  // Submit feedback to the API
  const submitFeedback = useCallback(
    async (feedback: Feedback) => {
      if (!chatId) return;
      if (isLocalThreadId(chatId)) {
        console.error("Local thread ID, can't submit feedback");
        return;
      }

      try {
        const response = await fetch(`${apiUrl}/rpc/chat.submitFeedback`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            ...auth.headers,
          },
          body: JSON.stringify({
            id: chatId,
            feedback,
          }),
        });

        if (!response.ok) {
          console.error("Failed to submit feedback:", response.statusText);
        }
      } catch (error) {
        console.error("Failed to submit feedback:", error);
      }
    },
    [chatId, apiUrl, auth.headers],
  );

  return (
    <ChatResolutionContext.Provider
      value={{
        isResolved: showFeedback && isResolved,
        feedbackHidden,
        setResolved,
        setUnresolved,
        resetFeedbackHidden,
        submitFeedback,
      }}
    >
      <LazyMotion features={domAnimation}>
        <MotionConfig reducedMotion="user">
          <ThreadPrimitive.Root
            className={cn(
              "aui-root aui-thread-root @container relative flex h-full flex-col bg-background",
              themeProps.className,
              className,
            )}
          >
            <ConnectionStatusIndicatorSafe />
            <ThreadPrimitive.Viewport
              className={cn(
                "aui-thread-viewport relative mx-auto flex w-full flex-1 flex-col overflow-x-auto overflow-y-scroll pb-0!",
                d("p-lg"),
              )}
            >
              <ThreadPrimitive.If empty>
                {components.ThreadWelcome ? (
                  <components.ThreadWelcome />
                ) : (
                  <ThreadWelcome />
                )}
              </ThreadPrimitive.If>

              {showDangerousApiKeyWarning && <DangerousApiKeyWarning />}

              <ThreadPrimitive.Messages
                components={{
                  UserMessage: components.UserMessage ?? UserMessage,
                  EditComposer: components.EditComposer ?? EditComposer,
                  AssistantMessage:
                    components.AssistantMessage ?? AssistantMessage,
                }}
              />

              <ThreadPrimitive.If empty={false} running={false}>
                <FollowOnSuggestions />
              </ThreadPrimitive.If>

              <ThreadPrimitive.If empty={false}>
                <div className="aui-thread-viewport-spacer min-h-8 grow" />
              </ThreadPrimitive.If>

              {!composerHidden && <Composer showFeedback={showFeedback} />}
            </ThreadPrimitive.Viewport>

            {/* Resolution overlay - subtle readonly effect */}
            <AnimatePresence>
              {showFeedback && isResolved && (
                <m.div
                  className="pointer-events-none absolute inset-0 z-50 bg-background/40"
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={{ opacity: 0 }}
                  transition={{ duration: 0.3, ease: EASE_OUT_QUINT }}
                />
              )}
            </AnimatePresence>
          </ThreadPrimitive.Root>
        </MotionConfig>
      </LazyMotion>
    </ChatResolutionContext.Provider>
  );
};

const ThreadScrollToBottom: FC = () => {
  return (
    <ThreadPrimitive.ScrollToBottom asChild>
      <TooltipIconButton
        tooltip="Scroll to bottom"
        variant="outline"
        className="aui-thread-scroll-to-bottom absolute -top-12 z-10 self-center rounded-full p-4 disabled:invisible dark:bg-background dark:text-foreground dark:hover:bg-accent"
      >
        <ArrowDownIcon />
      </TooltipIconButton>
    </ThreadPrimitive.ScrollToBottom>
  );
};

const ThreadWelcome: FC = () => {
  const { config } = useElements();
  const d = useDensity();
  const { logo, title, subtitle } = config.welcome ?? {};
  const isStandalone = config.variant === "standalone";

  return (
    <div
      className={cn(
        "aui-thread-welcome-root my-auto flex w-full grow flex-col",
        isStandalone ? "items-center justify-center" : "",
        d("gap-lg"),
      )}
    >
      <div
        className={cn(
          "aui-thread-welcome-center flex w-full grow flex-col items-center justify-start",
        )}
      >
        <div
          className={cn(
            "aui-thread-welcome-message flex flex-col",
            isStandalone
              ? "items-center text-center"
              : "size-full justify-start",
            d("gap-sm"),
            !isStandalone && d("py-md"),
          )}
        >
          {logo && (
            <m.img
              src={logo}
              alt=""
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 10 }}
              transition={{ duration: 0.25, ease: EASE_OUT_QUINT }}
              className={cn(
                "aui-thread-welcome-logo mb-2 size-12 object-contain",
              )}
            />
          )}
          <m.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 10 }}
            transition={{ duration: 0.25, ease: EASE_OUT_QUINT }}
            className={cn(
              "aui-thread-welcome-message-motion-1 font-semibold text-foreground",
              d("text-title"),
            )}
          >
            {title}
          </m.div>
          <m.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 10 }}
            transition={{ duration: 0.25, delay: 0.05, ease: EASE_OUT_QUINT }}
            className={cn(
              "aui-thread-welcome-message-motion-2 text-muted-foreground/65",
              d("text-subtitle"),
            )}
          >
            {subtitle}
          </m.div>
        </div>
      </div>
      <ThreadSuggestions />
    </div>
  );
};

const ThreadSuggestions: FC = () => {
  const { config } = useElements();
  const r = useRadius();
  const d = useDensity();
  const suggestions = config.welcome?.suggestions ?? [];
  const isStandalone = config.variant === "standalone";

  if (suggestions.length === 0) return null;

  return (
    <div
      className={cn(
        "aui-thread-welcome-suggestions w-full",
        d("gap-md"),
        d("py-lg"),
        isStandalone
          ? "flex flex-col @sm:flex-row @sm:flex-wrap @sm:items-center @sm:justify-center"
          : suggestions.length === 1
            ? "flex"
            : "grid max-w-fit @md:grid-cols-2",
      )}
    >
      {suggestions.map((suggestion, index) => (
        <m.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 20 }}
          transition={{
            duration: 0.25,
            delay: 0.03 * index,
            ease: EASE_OUT_QUINT,
          }}
          key={`suggested-action-${suggestion.title}-${index}`}
          className={cn(
            "aui-thread-welcome-suggestion-display",
            !isStandalone && "nth-[n+3]:hidden @md:nth-[n+3]:block",
          )}
        >
          <ThreadPrimitive.Suggestion prompt={suggestion.prompt} send asChild>
            <Button
              variant="ghost"
              className={cn(
                "aui-thread-welcome-suggestion h-auto w-full border text-left whitespace-break-spaces dark:hover:bg-accent/60",
                d("text-base"),
                isStandalone
                  ? `flex-col items-start @sm:flex-row @sm:items-center ${d("gap-sm")} ${d("px-lg")} ${d("py-sm")} ${r("full")}`
                  : `w-full flex-1 flex-col flex-wrap items-start justify-start ${d("gap-sm")} ${d("px-lg")} ${d("py-md")} ${r("xl")}`,
              )}
              aria-label={suggestion.prompt}
            >
              <span className="aui-thread-welcome-suggestion-text-1 text-sm font-medium text-foreground">
                {suggestion.title}
              </span>
              <span className="aui-thread-welcome-suggestion-text-2 text-sm text-muted-foreground">
                {suggestion.label}
              </span>
            </Button>
          </ThreadPrimitive.Suggestion>
        </m.div>
      ))}
    </div>
  );
};

/**
 * Component that handles tool mentions (@tool) in the composer.
 * Shows autocomplete dropdown and badges for mentioned tools.
 */
const ComposerToolMentions: FC<{
  tools: Record<string, unknown> | undefined;
}> = ({ tools }) => {
  const containerRef = useRef<HTMLDivElement>(null);

  const {
    mentionableTools,
    mentionedToolIds,
    value,
    cursorPosition,
    textareaRef,
    updateCursorPosition,
    handleAutocompleteChange,
    removeMention,
    isActive,
  } = useToolMentions({ tools });

  // Find and attach to the textarea within the composer.
  // Uses getRootNode() so it works inside Shadow DOM (where document.querySelector can't reach).
  useEffect(() => {
    if (!isActive) return;

    const rootNode = containerRef.current?.getRootNode() as
      | Document
      | ShadowRoot
      | undefined;
    if (!rootNode) return;

    const observeTarget =
      rootNode instanceof ShadowRoot ? rootNode : document.body;

    const findTextarea = () => {
      const textarea = rootNode.querySelector(
        ".aui-composer-input",
      ) as HTMLTextAreaElement | null;
      if (textarea && textareaRef.current !== textarea) {
        textareaRef.current = textarea;

        const handleSelectionChange = () => updateCursorPosition();
        textarea.addEventListener("click", handleSelectionChange);
        textarea.addEventListener("keyup", handleSelectionChange);
        textarea.addEventListener("input", handleSelectionChange);

        return () => {
          textarea.removeEventListener("click", handleSelectionChange);
          textarea.removeEventListener("keyup", handleSelectionChange);
          textarea.removeEventListener("input", handleSelectionChange);
        };
      }
    };

    const cleanup = findTextarea();

    const observer = new MutationObserver(() => {
      findTextarea();
    });

    observer.observe(observeTarget, {
      childList: true,
      subtree: true,
    });

    return () => {
      cleanup?.();
      observer.disconnect();
    };
  }, [isActive, textareaRef, updateCursorPosition]);

  if (!isActive) {
    return null;
  }

  return (
    <div ref={containerRef} className="aui-composer-tool-mentions relative">
      {/* Badges showing mentioned tools */}
      <MentionedToolsBadges
        mentionedToolIds={mentionedToolIds}
        tools={mentionableTools}
        onRemove={removeMention}
      />

      {/* Autocomplete dropdown */}
      <AnimatePresence>
        <ToolMentionAutocomplete
          tools={mentionableTools}
          value={value}
          cursorPosition={cursorPosition}
          onValueChange={handleAutocompleteChange}
          textareaRef={textareaRef}
        />
      </AnimatePresence>
    </div>
  );
};

// Resets feedbackHidden when a new message starts generating
const FeedbackHiddenResetter: FC = () => {
  const { resetFeedbackHidden } = useChatResolution();

  useEffect(() => {
    resetFeedbackHidden();
  }, [resetFeedbackHidden]);

  return null;
};

const ComposerFeedback: FC = () => {
  const { isResolved, feedbackHidden, setResolved, submitFeedback } =
    useChatResolution();

  const handleFeedback = useCallback(
    async (type: "like" | "dislike") => {
      const feedback = type === "like" ? "success" : "failure";
      await submitFeedback(feedback);
    },
    [submitFeedback],
  );

  return (
    <ThreadPrimitive.If empty={false}>
      {/* Reset feedbackHidden when a new message starts generating */}
      <ThreadPrimitive.If running>
        <FeedbackHiddenResetter />
      </ThreadPrimitive.If>
      <ThreadPrimitive.If running={false}>
        <AnimatePresence>
          {!isResolved && !feedbackHidden && (
            <m.div
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 10 }}
              transition={{ duration: 0.2, ease: EASE_OUT_QUINT }}
              className="mb-3"
            >
              <MessageFeedback
                className="mx-auto"
                onResolved={setResolved}
                onFeedback={(type) => {
                  void handleFeedback(type);
                }}
              />
            </m.div>
          )}
        </AnimatePresence>
      </ThreadPrimitive.If>
    </ThreadPrimitive.If>
  );
};

interface ComposerProps {
  showFeedback?: boolean;
}

const Composer: FC<ComposerProps> = ({ showFeedback = false }) => {
  const { config, mcpTools } = useElements();
  const { isResolved, setUnresolved } = useChatResolution();
  const r = useRadius();
  const d = useDensity();
  const replayCtx = useReplayContext();

  const isReplay = replayCtx?.isReplay ?? false;
  const composerConfig = config.composer ?? {
    placeholder: "Send a message...",
    attachments: true,
  };
  const components = config.components ?? {};

  // Determine if tool mentions are enabled (default: true)
  const toolMentionsEnabled =
    composerConfig.toolMentions === undefined ||
    composerConfig.toolMentions === true ||
    (typeof composerConfig.toolMentions === "object" &&
      composerConfig.toolMentions.enabled !== false);

  const composerRootRef = useRef<HTMLFormElement>(null);

  if (components.Composer) {
    return <components.Composer />;
  }

  return (
    <div
      className={cn(
        "aui-composer-wrapper sticky bottom-0 z-[60] flex w-full flex-col overflow-visible bg-background",
        d("gap-md"),
        d("py-md"),
        r("xl"),
      )}
    >
      {showFeedback && <ComposerFeedback />}
      <ThreadScrollToBottom />
      {showFeedback && isResolved ? (
        <m.div
          className="aui-composer-resolved flex min-h-[118px] flex-col items-center justify-center gap-2 border-t border-input px-1"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.2, ease: EASE_OUT_QUINT }}
        >
          <span className="text-sm text-muted-foreground">
            This conversation has been resolved
          </span>
          <Button
            variant="outline"
            size="sm"
            className="text-foreground"
            onClick={setUnresolved}
          >
            Reopen conversation
          </Button>
        </m.div>
      ) : (
        <ComposerPrimitive.Root
          ref={composerRootRef}
          className={cn(
            "aui-composer-root group/input-group relative flex min-h-[118px] w-full flex-col border border-input bg-background px-1 pt-2 shadow-xs transition-[color,box-shadow] outline-none has-[textarea:focus-visible]:border-ring has-[textarea:focus-visible]:ring-1 has-[textarea:focus-visible]:ring-ring/5 dark:bg-background",
            r("xl"),
            isReplay && "pointer-events-none opacity-50",
          )}
        >
          {composerConfig.attachments && <ComposerAttachments />}

          {toolMentionsEnabled && <ComposerToolMentions tools={mcpTools} />}

          <ComposerPrimitive.Input
            placeholder={composerConfig.placeholder}
            className={cn(
              "aui-composer-input mb-1 max-h-32 w-full resize-none bg-transparent px-3.5 pt-1.5 pb-3 text-foreground outline-none placeholder:text-muted-foreground focus-visible:ring-0",
              d("h-input"),
              d("text-base"),
            )}
            rows={1}
            autoFocus={!isReplay}
            disabled={isReplay}
            aria-label="Message input"
          />
          <ComposerAction />
        </ComposerPrimitive.Root>
      )}
    </div>
  );
};

const ComposerModelPicker: FC = () => {
  const { model, setModel } = useElements();
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [tooltipOpen, setTooltipOpen] = useState(false);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const savedScrollPosition = useRef(0);
  const previousOpenRef = useRef(false);

  useEffect(() => {
    // Restore scroll position when opening
    if (popoverOpen && !previousOpenRef.current) {
      requestAnimationFrame(() => {
        const container = scrollContainerRef.current;
        if (container && container.scrollHeight > 0) {
          container.scrollTop = savedScrollPosition.current;
        }
      });
    }

    previousOpenRef.current = popoverOpen;
  }, [popoverOpen]);

  // Close tooltip when popover opens
  useEffect(() => {
    if (popoverOpen) {
      setTooltipOpen(false);
    }
  }, [popoverOpen]);

  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    savedScrollPosition.current = e.currentTarget.scrollTop;
  };

  return (
    <TooltipProvider>
      <Tooltip open={tooltipOpen && !popoverOpen} onOpenChange={setTooltipOpen}>
        <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
          <TooltipTrigger asChild>
            <PopoverTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                data-state={popoverOpen ? "open" : "closed"}
                className="aui-composer-model-picker flex w-fit items-center gap-2 rounded-full px-2.5 py-1 text-xs font-semibold data-[state=open]:bg-muted-foreground/15 dark:border-muted-foreground/15 dark:hover:bg-muted-foreground/30"
                aria-label="Model Settings"
              >
                <Settings2 className="aui-attachment-add-icon size-5 stroke-[1.5px]" />
              </Button>
            </PopoverTrigger>
          </TooltipTrigger>
          <PopoverContent
            side="top"
            align="start"
            className="w-min p-0 shadow-none"
          >
            <div
              ref={scrollContainerRef}
              className="max-h-48 overflow-y-auto"
              onScroll={handleScroll}
            >
              {MODELS.map((m) => (
                <Button
                  key={m}
                  onClick={() => {
                    setModel(m);
                  }}
                  variant="ghost"
                  className="w-full justify-start gap-2 rounded-none px-2"
                >
                  {m === model ? (
                    <div>
                      <CheckIcon className="size-4 text-emerald-500" />
                    </div>
                  ) : (
                    <div className="size-4">&nbsp;</div>
                  )}
                  {m}
                </Button>
              ))}
            </div>
          </PopoverContent>
        </Popover>
        <TooltipContent side="bottom" align="start">
          Model Settings
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
};

const CASSETTE_RECORDING_ENABLED =
  import.meta.env.VITE_ELEMENTS_ENABLE_CASSETTE_RECORDING === "true";

const ComposerCassetteRecorder: FC = () => {
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [tooltipOpen, setTooltipOpen] = useState(false);
  const { isRecording, startRecording, stopRecording, download } =
    useRecordCassette();

  useEffect(() => {
    if (popoverOpen) setTooltipOpen(false);
  }, [popoverOpen]);

  return (
    <TooltipProvider>
      <Tooltip open={tooltipOpen && !popoverOpen} onOpenChange={setTooltipOpen}>
        <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
          <TooltipTrigger asChild>
            <PopoverTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                data-state={popoverOpen ? "open" : "closed"}
                className={cn(
                  "aui-composer-cassette-recorder flex w-fit items-center gap-2 rounded-full px-2.5 py-1 text-xs font-semibold data-[state=open]:bg-muted-foreground/15 dark:border-muted-foreground/15 dark:hover:bg-muted-foreground/30",
                  isRecording && "text-red-500",
                )}
                aria-label="Cassette Recorder"
              >
                <CircleIcon
                  className={cn(
                    "size-5 stroke-[1.5px]",
                    isRecording && "animate-pulse fill-red-500 text-red-500",
                  )}
                />
              </Button>
            </PopoverTrigger>
          </TooltipTrigger>
          <PopoverContent side="top" align="start" className="w-64 p-3">
            <div className="flex flex-col gap-3">
              <div className="text-sm font-medium">Cassette Recorder</div>
              {!isRecording ? (
                <Button
                  size="sm"
                  variant="outline"
                  className="w-full justify-start gap-2"
                  onClick={startRecording}
                >
                  <CircleIcon className="size-3 fill-red-500 text-red-500" />
                  Start Recording
                </Button>
              ) : (
                <Button
                  size="sm"
                  variant="outline"
                  className="w-full justify-start gap-2"
                  onClick={() => {
                    stopRecording();
                    download();
                    setPopoverOpen(false);
                  }}
                >
                  <DownloadIcon className="size-3" />
                  Stop &amp; Download
                </Button>
              )}
            </div>
          </PopoverContent>
        </Popover>
        <TooltipContent side="bottom" align="start">
          {isRecording ? "Recording…" : "Cassette Recorder"}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
};

// Sentinel for the "All" pseudo-category in the tool-mention picker.
const TOOL_MENTION_ALL_CATEGORY = "__all__";

function humanizeToolCategory(raw: string): string {
  const cleaned = raw.replace(/[-_]+/g, " ").trim();
  if (!cleaned) return "Tools";
  return cleaned.replace(/\b\w/g, (c) => c.toUpperCase());
}

// Derive a grouping label for a tool. Tools from multiple MCP servers are
// namespaced as `<server>__<tool>`; otherwise group by the first
// underscore-delimited segment (e.g. `platform_search_logs` -> "Platform"),
// falling back to a single "Tools" bucket.
function deriveToolCategory(name: string): string {
  const namespaceIdx = name.indexOf("__");
  if (namespaceIdx > 0)
    return humanizeToolCategory(name.slice(0, namespaceIdx));
  const underscoreIdx = name.indexOf("_");
  if (underscoreIdx > 0)
    return humanizeToolCategory(name.slice(0, underscoreIdx));
  return "Tools";
}

interface ToolCategory {
  name: string;
  tools: MentionableTool[];
}

// A discoverable counterpart to the type-`@` autocomplete: a composer button
// that opens a searchable, category-grouped picker of the available tools and
// inserts an @mention for the chosen one. Inserts through the composer runtime
// so it stays in sync with the autocomplete's own textarea handling. Hidden when
// tool mentions are disabled or there are no tools.
const ComposerToolMentionPicker: FC = () => {
  const { config, mcpTools, mcpToolsLoading } = useElements();
  const api = useAssistantApi();
  // Read the composer text from the same reactive source the tool-mention
  // badges parse, so an inserted mention renders a pill just like the type-`@`
  // autocomplete does.
  const composerText = useAssistantState(({ composer }) => composer.text);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeCategory, setActiveCategory] = useState(
    TOOL_MENTION_ALL_CATEGORY,
  );

  const composerConfig = config.composer;
  const toolMentionsEnabled =
    composerConfig?.toolMentions === undefined ||
    composerConfig.toolMentions === true ||
    (typeof composerConfig.toolMentions === "object" &&
      composerConfig.toolMentions.enabled !== false);

  const tools = useMemo(() => toolSetToMentionableTools(mcpTools), [mcpTools]);

  const categories = useMemo<ToolCategory[]>(() => {
    const grouped = new Map<string, MentionableTool[]>();
    for (const tool of tools) {
      const category = deriveToolCategory(tool.name);
      const existing = grouped.get(category);
      if (existing) {
        existing.push(tool);
      } else {
        grouped.set(category, [tool]);
      }
    }
    return [...grouped.entries()]
      .map(([name, categoryTools]) => ({ name, tools: categoryTools }))
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [tools]);

  // Show the button while tools are still loading (so it appears immediately
  // rather than popping in once the async MCP list resolves) or once there are
  // tools — but hide it when the list has loaded and is empty, so we don't
  // expose a dead-end control.
  if (!toolMentionsEnabled || (!mcpToolsLoading && tools.length === 0)) {
    return null;
  }

  const normalizedQuery = query.trim().toLowerCase();
  const inActiveCategory =
    activeCategory === TOOL_MENTION_ALL_CATEGORY
      ? tools
      : (categories.find((c) => c.name === activeCategory)?.tools ?? []);
  const visibleTools = normalizedQuery
    ? inActiveCategory.filter(
        (tool) =>
          tool.name.toLowerCase().includes(normalizedQuery) ||
          (tool.description?.toLowerCase().includes(normalizedQuery) ?? false),
      )
    : inActiveCategory;

  const insertMention = (toolName: string) => {
    const base =
      composerText && !/\s$/.test(composerText)
        ? `${composerText} `
        : composerText;
    api.composer().setText(`${base}@${toolName} `);
    setOpen(false);
    setQuery("");
  };

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setQuery("");
      setActiveCategory(TOOL_MENTION_ALL_CATEGORY);
    }
  };

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          data-state={open ? "open" : "closed"}
          className="aui-composer-tool-mention-picker flex w-fit items-center gap-2 rounded-full px-2.5 py-1 text-xs font-semibold data-[state=open]:bg-muted-foreground/15 dark:border-muted-foreground/15 dark:hover:bg-muted-foreground/30"
          aria-label="Mention a tool"
        >
          <AtSign className="size-5 stroke-[1.5px]" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        className="aui-composer-tool-mention-popover w-[420px] overflow-hidden p-0"
      >
        <div className="flex items-center gap-2 border-b border-input px-3 py-2">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search tools…"
            className="w-full bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground"
            aria-label="Search tools"
          />
        </div>
        <div className="flex h-72">
          <div className="w-36 shrink-0 overflow-y-auto border-r border-input p-2">
            <div className="px-2 pb-1 text-[10px] font-semibold tracking-wide text-muted-foreground uppercase">
              Categories
            </div>
            <button
              type="button"
              onClick={() => setActiveCategory(TOOL_MENTION_ALL_CATEGORY)}
              className={cn(
                "flex w-full items-center justify-between rounded px-2 py-1 text-left text-xs transition-colors",
                activeCategory === TOOL_MENTION_ALL_CATEGORY
                  ? "bg-muted font-medium text-foreground"
                  : "text-muted-foreground hover:bg-muted/60",
              )}
            >
              <span className="truncate">All</span>
              <span className="ml-2 shrink-0 tabular-nums opacity-60">
                {tools.length}
              </span>
            </button>
            {categories.map((category) => (
              <button
                key={category.name}
                type="button"
                onClick={() => setActiveCategory(category.name)}
                className={cn(
                  "flex w-full items-center justify-between rounded px-2 py-1 text-left text-xs transition-colors",
                  activeCategory === category.name
                    ? "bg-muted font-medium text-foreground"
                    : "text-muted-foreground hover:bg-muted/60",
                )}
              >
                <span className="truncate">{category.name}</span>
                <span className="ml-2 shrink-0 tabular-nums opacity-60">
                  {category.tools.length}
                </span>
              </button>
            ))}
          </div>
          <div className="min-w-0 flex-1 overflow-y-auto p-2">
            {visibleTools.length === 0 ? (
              <div className="px-2 py-6 text-center text-xs text-muted-foreground">
                {mcpToolsLoading ? "Loading tools…" : "No tools found"}
              </div>
            ) : (
              visibleTools.map((tool) => (
                <button
                  key={tool.id}
                  type="button"
                  onClick={() => insertMention(tool.name)}
                  className="flex w-full items-start gap-2 rounded px-2 py-1.5 text-left transition-colors hover:bg-muted"
                >
                  <Wrench className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium text-foreground">
                      {tool.name}
                    </span>
                    {tool.description && (
                      <span className="line-clamp-2 text-xs text-muted-foreground">
                        {tool.description}
                      </span>
                    )}
                  </span>
                </button>
              ))
            )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
};

const ComposerAction: FC = () => {
  const { config } = useElements();
  const r = useRadius();
  const composerConfig = config.composer ?? { attachments: true };
  return (
    <div className="aui-composer-action-wrapper relative mx-1 mt-2 mb-2 flex items-center justify-between">
      <div className="aui-composer-action-wrapper-inner flex items-center text-muted-foreground">
        {composerConfig.attachments ? (
          <ComposerAddAttachment />
        ) : (
          <div className="aui-composer-add-attachment-placeholder" />
        )}

        <ComposerToolMentionPicker />

        {config.model?.showModelPicker && !config.languageModel && (
          <ComposerModelPicker />
        )}

        {CASSETTE_RECORDING_ENABLED && <ComposerCassetteRecorder />}
      </div>

      <ThreadPrimitive.If running={false}>
        <ComposerPrimitive.Send asChild>
          <TooltipIconButton
            tooltip="Send message"
            side="bottom"
            type="submit"
            variant="default"
            size="icon"
            className={cn("aui-composer-send size-[34px] p-1", r("full"))}
            aria-label="Send message"
          >
            <ArrowUpIcon className="aui-composer-send-icon size-5" />
          </TooltipIconButton>
        </ComposerPrimitive.Send>
      </ThreadPrimitive.If>

      <ThreadPrimitive.If running>
        <ComposerPrimitive.Cancel asChild>
          <Button
            type="button"
            variant="default"
            size="icon"
            className={cn(
              "aui-composer-cancel size-[34px] border border-muted-foreground/60 hover:bg-primary/75 dark:border-muted-foreground/90",
              r("full"),
            )}
            aria-label="Stop generating"
          >
            <Square className="aui-composer-cancel-icon size-3.5 fill-white dark:fill-black" />
          </Button>
        </ComposerPrimitive.Cancel>
      </ThreadPrimitive.If>
    </div>
  );
};

const MessageError: FC = () => {
  return (
    <MessagePrimitive.Error>
      <ErrorPrimitive.Root className="aui-message-error-root mt-2 rounded-md border border-destructive bg-destructive/10 p-3 text-sm text-destructive dark:bg-destructive/5 dark:text-red-200">
        {/* No line-clamp — the credits-exhausted prompt must render in full. */}
        <ErrorPrimitive.Message className="aui-message-error-message whitespace-pre-wrap" />
      </ErrorPrimitive.Root>
    </MessagePrimitive.Error>
  );
};

const AssistantMessage: FC = () => {
  const { config } = useElements();
  const toolsConfig = config.tools ?? {};
  const components = config.components;
  const toolsComponents = toolsConfig.components;

  const partsComponents = useMemo(
    () => ({
      Text: components?.Text ?? MarkdownText,
      Image: components?.Image ?? Image,
      tools: {
        by_name: toolsComponents,
        Fallback: components?.ToolFallback ?? ToolFallback,
      },
      Reasoning: components?.Reasoning ?? Reasoning,
      ReasoningGroup: components?.ReasoningGroup ?? ReasoningGroup,
      ToolGroup: components?.ToolGroup ?? ToolGroup,
    }),
    [components, toolsComponents],
  );

  return (
    <MessagePrimitive.Root asChild>
      <div
        className="aui-assistant-message-root relative mx-auto w-full animate-in py-4 duration-150 ease-out fade-in slide-in-from-bottom-1 last:mb-24"
        data-role="assistant"
      >
        <div className="aui-assistant-message-content mx-2 leading-7 wrap-break-word text-foreground">
          <MessagePrimitive.Parts components={partsComponents} />
          <ThinkingIndicator />
          <MessageError />
        </div>

        <div className="aui-assistant-message-footer mt-2 ml-2 flex items-center gap-3">
          {/* <BranchPicker /> */}
          <AssistantActionBar />
        </div>
      </div>
    </MessagePrimitive.Root>
  );
};

const Image: FC<ImageMessagePartProps> = (props) => {
  return <img src={props.image} />;
};

const AssistantActionBar: FC = () => {
  // Only the message text is copyable, so a message made up solely of tool
  // calls (and/or reasoning) has nothing to copy — don't render the bar there.
  // Otherwise a lone Copy button hangs beneath every tool-only turn.
  const hasCopyableText = useAssistantState(({ message }) =>
    message.parts.some(
      (part) => part.type === "text" && part.text.trim().length > 0,
    ),
  );
  if (!hasCopyableText) return null;

  return (
    <ActionBarPrimitive.Root
      hideWhenRunning
      autohide="not-last"
      autohideFloat="single-branch"
      className="aui-assistant-action-bar-root col-start-3 row-start-2 -ml-1 flex gap-1 text-muted-foreground data-floating:absolute data-floating:rounded-md data-floating:border data-floating:bg-background data-floating:p-1 data-floating:shadow-sm"
    >
      <ActionBarPrimitive.Copy asChild>
        <TooltipIconButton tooltip="Copy">
          <MessagePrimitive.If copied>
            <CheckIcon />
          </MessagePrimitive.If>
          <MessagePrimitive.If copied={false}>
            <CopyIcon />
          </MessagePrimitive.If>
        </TooltipIconButton>
      </ActionBarPrimitive.Copy>
      {/* <ActionBarPrimitive.Reload asChild>
        <TooltipIconButton tooltip="Refresh">
          <RefreshCwIcon />
        </TooltipIconButton>
      </ActionBarPrimitive.Reload> */}
    </ActionBarPrimitive.Root>
  );
};

const UserMessage: FC = () => {
  const r = useRadius();
  const { config } = useElements();
  const allowEdit = config.allowMessageEdit !== false;
  return (
    <MessagePrimitive.Root asChild>
      <div
        className="aui-user-message-root mx-auto grid w-full animate-in auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] gap-y-2 px-2 py-4 duration-150 ease-out fade-in slide-in-from-bottom-1 first:mt-3 last:mb-5 [&:where(>*)]:col-start-2"
        data-role="user"
      >
        <UserMessageAttachments />

        <div className="aui-user-message-content-wrapper relative col-start-2 min-w-0">
          <UserMessageHeader />
          <div
            className={cn(
              "aui-user-message-content ml-auto w-fit bg-blue-500 px-5 py-2.5 wrap-break-word text-white",
              r("xl"),
            )}
          >
            <MessagePrimitive.Parts components={{ Text: UserMessageText }} />
          </div>
          {allowEdit && (
            <div className="aui-user-action-bar-wrapper absolute top-1/2 left-0 -translate-x-full -translate-y-1/2 pr-2">
              <UserActionBar />
            </div>
          )}
        </div>

        <BranchPicker className="aui-user-branch-picker col-span-full col-start-1 row-start-3 -mr-1 justify-end" />
      </div>
    </MessagePrimitive.Root>
  );
};

/**
 * Avatar + name + timestamp above a user turn, identifying who sent it and
 * when — the name/avatar resolved via `history.resolveCreator` for the
 * message's thread. Renders nothing when unresolved (no `resolveCreator`
 * configured, or it returned nothing for this chat).
 */
const UserMessageHeader: FC = () => {
  const id = useAssistantState(
    ({ threadListItem }) =>
      threadListItem.remoteId ?? threadListItem.externalId,
  );
  const owner = useThreadMeta(id ?? undefined)?.owner;
  const createdAt = useAssistantState(({ message }) => message.createdAt);
  if (!owner) return null;

  const display = owner.name || owner.email;
  const time = createdAt.toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
  });
  return (
    <div className="aui-user-message-owner mb-1.5 flex items-center justify-end gap-2 pr-5 text-xs text-muted-foreground">
      <Avatar className="size-7">
        {owner.photoUrl ? (
          <AvatarImage src={owner.photoUrl} alt={display} />
        ) : null}
        <AvatarFallback className="text-xs font-medium">
          {initialsOf(display)}
        </AvatarFallback>
      </Avatar>
      <span className="font-medium text-foreground">{display}</span>
      <span className="h-3 w-px bg-border" aria-hidden="true" />
      <span>{time}</span>
    </div>
  );
};

const UserActionBar: FC = () => {
  return (
    <ActionBarPrimitive.Root
      hideWhenRunning
      autohide="not-last"
      className="aui-user-action-bar-root flex flex-col items-end"
    >
      <ActionBarPrimitive.Edit asChild>
        <TooltipIconButton tooltip="Edit" className="aui-user-action-edit p-4">
          <PencilIcon />
        </TooltipIconButton>
      </ActionBarPrimitive.Edit>
    </ActionBarPrimitive.Root>
  );
};

const EditComposer: FC = () => {
  return (
    <div className="aui-edit-composer-wrapper mx-auto flex w-full flex-col gap-4 px-2 first:mt-4">
      <ComposerPrimitive.Root className="aui-edit-composer-root ml-auto flex w-full max-w-7/8 flex-col rounded-xl bg-muted">
        <ComposerPrimitive.Input
          className="aui-edit-composer-input flex min-h-[60px] w-full resize-none bg-transparent p-4 text-foreground outline-none"
          autoFocus
        />

        <div className="aui-edit-composer-footer mx-3 mb-3 flex items-center justify-center gap-2 self-end">
          <ComposerPrimitive.Cancel asChild>
            <Button variant="ghost" size="sm" aria-label="Cancel edit">
              Cancel
            </Button>
          </ComposerPrimitive.Cancel>
          <ComposerPrimitive.Send asChild>
            <Button size="sm" aria-label="Update message">
              Update
            </Button>
          </ComposerPrimitive.Send>
        </div>
      </ComposerPrimitive.Root>
    </div>
  );
};

const BranchPicker: FC<BranchPickerPrimitive.Root.Props> = ({
  className,
  ...rest
}) => {
  return (
    <BranchPickerPrimitive.Root
      hideWhenSingleBranch
      className={cn(
        "aui-branch-picker-root mr-2 -ml-2 inline-flex items-center text-xs text-muted-foreground",
        className,
      )}
      {...rest}
    >
      <BranchPickerPrimitive.Previous asChild>
        <TooltipIconButton tooltip="Previous">
          <ChevronLeftIcon />
        </TooltipIconButton>
      </BranchPickerPrimitive.Previous>
      <span className="aui-branch-picker-state font-medium">
        <BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
      </span>
      <BranchPickerPrimitive.Next asChild>
        <TooltipIconButton tooltip="Next">
          <ChevronRightIcon />
        </TooltipIconButton>
      </BranchPickerPrimitive.Next>
    </BranchPickerPrimitive.Root>
  );
};
