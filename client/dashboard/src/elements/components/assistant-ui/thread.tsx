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
  Mic,
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
  TextMessagePartProvider,
  ThreadPrimitive,
  useAui,
  useAuiState,
  type TextMessagePartComponent,
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
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FC,
  type PropsWithChildren,
} from "react";

import {
  ComposerAddAttachment,
  ComposerAttachments,
  UserMessageAttachments,
} from "@/elements/components/assistant-ui/attachment";
import { AttachmentDropZone } from "@/elements/components/assistant-ui/attachment-dropzone";
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
import { useDictationLevels } from "@/elements/hooks/useDictationLevels";
import { useElements } from "@/elements/hooks/useElements";
import { isLocalThreadId } from "@/elements/hooks/useGramThreadListAdapter";
import { usePromptHistory } from "@/elements/hooks/usePromptHistory";
import { useRadius } from "@/elements/hooks/useRadius";
import { useRecordCassette } from "@/elements/hooks/useRecordCassette";
import { useThemeProps } from "@/elements/hooks/useThemeProps";
import { useToolMentions } from "@/elements/hooks/useToolMentions";
import { getApiUrl } from "@/elements/lib/api";
import { dictationAdapter } from "@/elements/lib/dictation";
import { EASE_OUT_QUINT } from "@/elements/lib/easing";
import { groupAssistantMessageParts } from "@/elements/lib/messagePartGrouping";
import {
  stripTrailingAnnotationLine,
  trailingAnnotationLine,
} from "@/elements/lib/toolCallAnnotation";
import { MODELS } from "@/elements/lib/models";
import type {
  ComposerSkill,
  ComposerSlashCommand,
  SkillContextConfig,
} from "@/elements/types";
import {
  type MentionableTool,
  toolSetToMentionableTools,
} from "@/elements/lib/tool-mentions";
import { cn, initialsOf } from "@/lib/utils";
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
    <strong>Danger:</strong> You are using a Speakeasy API key directly in the
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
  const isReplay = useReplayContext()?.isReplay ?? false;

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
          <AttachmentDropZone
            // Every state that takes the composer away also refuses drops,
            // or files queue into a composer the user cannot reach.
            disabled={
              composerHidden || (showFeedback && isResolved) || isReplay
            }
            className="flex h-full min-h-0 flex-1 flex-col"
          >
            <ThreadPrimitive.Root
              className={cn(
                "aui-root aui-thread-root @container relative flex h-full w-full flex-col bg-background",
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
          </AttachmentDropZone>
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
        className="aui-thread-scroll-to-bottom pointer-events-auto absolute bottom-full left-1/2 mb-2 -translate-x-1/2 rounded-full p-4 disabled:invisible dark:bg-background dark:text-foreground dark:hover:bg-accent"
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
              // z-10 keeps the pill (and its portalled tooltips) above the
              // scroll-to-bottom button that floats directly overhead.
              className="pointer-events-auto relative z-10"
            >
              <MessageFeedback
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
  /** Standalone hosts (entry-point composers with no message list above them)
   *  pass false: there is no viewport to scroll back down to, and the composer
   *  must not claim the run state of a conversation it doesn't own. */
  showThreadAffordances?: boolean;
  /** Grab focus on mount. True inside a thread, where typing is the only thing
   *  to do; false on landing pages, where stealing focus hijacks the scroll
   *  position and the keyboard from the rest of the page. */
  autoFocus?: boolean;
}

export const Composer: FC<ComposerProps> = ({
  showFeedback = false,
  showThreadAffordances = true,
  autoFocus = true,
}) => {
  const { config, mcpTools } = useElements();
  const { isResolved, setUnresolved } = useChatResolution();
  const r = useRadius();
  const d = useDensity();
  const replayCtx = useReplayContext();

  const isReplay = replayCtx?.isReplay ?? false;
  const isDictating = useAuiState(({ composer }) => composer.dictation != null);
  const isComposerEmpty = useAuiState(({ composer }) => composer.text === "");
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

  // Slash commands: typing `/` turns the draft into a command query. Picking
  // one REPLACES the draft and sends, so the raw "/…" text is never submitted.
  const aui = useAui();
  const composerText = useAuiState(({ composer }) => composer.text);
  const slashCommands = composerConfig.slashCommands ?? [];
  const slashQuery = composerText.startsWith("/")
    ? composerText.slice(1).trim().toLowerCase()
    : null;
  const slashMatches = useMemo(() => {
    if (slashQuery === null) return [];
    if (!slashQuery) return slashCommands;
    return slashCommands.filter(
      (command) =>
        command.title.toLowerCase().includes(slashQuery) ||
        (command.label?.toLowerCase().includes(slashQuery) ?? false),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps -- slashCommands is a config array, compared by content below
  }, [slashQuery, slashCommands]);
  const [activeSlashIndex, setActiveSlashIndex] = useState(0);
  const slashOpen = slashMatches.length > 0;

  useEffect(() => {
    setActiveSlashIndex(0);
  }, [slashQuery]);

  const runSlashCommand = (command: ComposerSlashCommand) => {
    const composer = aui.composer();
    composer.setText(command.prompt);
    composer.send();
  };

  // Terminal-style prompt recall. The draft text lives on the runtime, so the
  // ref is what the submit handler reads: the runtime clears the composer as
  // part of sending, and refs still hold the pre-send render's value there.
  const promptHistory = usePromptHistory(config.projectSlug);
  const composerTextRef = useRef(composerText);
  composerTextRef.current = composerText;

  const recallPrompt = (
    textarea: HTMLTextAreaElement,
    direction: "up" | "down",
  ) => {
    const recalled = promptHistory.navigate(direction, textarea.value);
    if (recalled === null) return false;
    aui.composer().setText(recalled);
    // The composer is controlled, so the caret can only be placed once the
    // recalled text has actually been painted.
    requestAnimationFrame(() => {
      textarea.setSelectionRange(recalled.length, recalled.length);
    });
    return true;
  };

  /**
   * Arrow keys belong to the textarea first: recall only takes over when the
   * caret has nowhere left to go in that direction (first line for Up, last
   * line for Down), or when the text on screen is one we just recalled — that
   * keeps a multi-line prompt from trapping the walk after one step.
   */
  const canRecall = (
    textarea: HTMLTextAreaElement,
    direction: "up" | "down",
  ) => {
    if (promptHistory.isShowingRecalled(textarea.value)) return true;
    const { value, selectionStart, selectionEnd } = textarea;
    if (selectionStart !== selectionEnd) return false;
    return direction === "up"
      ? !value.slice(0, selectionStart).includes("\n")
      : !value.slice(selectionEnd).includes("\n");
  };

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
      {/* Floating overlay above the opaque composer: keeps the message list
          scrolling all the way down to the composer instead of being cut off
          by a band of background behind the feedback pill. */}
      {showThreadAffordances && (
        <div className="aui-composer-overlay pointer-events-none absolute inset-x-0 bottom-full z-20 flex justify-center pb-3">
          {showFeedback && <ComposerFeedback />}
          <ThreadScrollToBottom />
        </div>
      )}
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
          // Capture: the menu owns Up/Down/Enter while it is open, before the
          // textarea inserts a newline or the composer sends the raw query.
          onSubmit={() => {
            promptHistory.record(composerTextRef.current);
          }}
          onKeyDownCapture={(event) => {
            if (!slashOpen) return;
            if (event.key === "ArrowDown") {
              event.preventDefault();
              setActiveSlashIndex((i) => (i + 1) % slashMatches.length);
            } else if (event.key === "ArrowUp") {
              event.preventDefault();
              setActiveSlashIndex(
                (i) => (i - 1 + slashMatches.length) % slashMatches.length,
              );
            } else if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              event.stopPropagation();
              const command = slashMatches[activeSlashIndex];
              if (command) runSlashCommand(command);
            } else if (event.key === "Escape") {
              event.preventDefault();
              aui.composer().setText("");
            }
          }}
          // Lets compact hosts (the docked pill) restyle the composer while
          // dictation is live — there is no room there for both the transcript
          // and the level trail.
          data-dictating={isDictating ? "true" : undefined}
          // Hosts that paint their own placeholder (the landing surfaces cycle
          // through example prompts) need to know when the draft is empty.
          data-empty={isComposerEmpty ? "true" : undefined}
          className={cn(
            "aui-composer-root group/input-group relative flex min-h-[118px] w-full flex-col border border-black/8 bg-background px-1.5 pt-3 shadow-[0_1px_2px_rgba(0,0,0,0.04),0_10px_28px_-16px_rgba(0,0,0,0.18)] transition-[color,border-color,box-shadow] outline-none has-[textarea:focus-visible]:border-black/15 dark:border-white/10 dark:bg-background dark:has-[textarea:focus-visible]:border-white/20",
            r("xl"),
            isReplay && "pointer-events-none opacity-50",
          )}
        >
          {(composerConfig.attachments ?? true) !== false && (
            <ComposerAttachments />
          )}

          {slashOpen && (
            <ComposerSlashCommandMenu
              commands={slashMatches}
              activeIndex={activeSlashIndex}
              onHover={setActiveSlashIndex}
              onSelect={runSlashCommand}
            />
          )}

          {toolMentionsEnabled && <ComposerToolMentions tools={mcpTools} />}

          <ComposerSkillContextBadges />

          {/* Speech lands in the input as the recognizer finalizes it, which
              reads as text writing itself. Hide the draft while the session is
              live and show a single "Listening…" label instead; the text is
              revealed intact the moment dictation stops. */}
          {isDictating && (
            <span
              aria-hidden="true"
              className={cn(
                "aui-composer-listening pointer-events-none absolute px-4 pt-0.5 text-muted-foreground",
                d("text-base"),
              )}
            >
              Listening…
            </span>
          )}
          <ComposerPrimitive.Input
            placeholder={composerConfig.placeholder}
            // Bubble phase, on the textarea itself: the slash menu (form,
            // capture) and the @-mention menu (textarea, capture + stopPropagation)
            // both get the arrow keys first, so recall only sees the ones nobody
            // else claimed.
            onKeyDown={(event) => {
              if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
              if (isDictating || isReplay) return;
              const textarea = event.currentTarget;
              const direction = event.key === "ArrowUp" ? "up" : "down";
              if (!canRecall(textarea, direction)) return;
              if (recallPrompt(textarea, direction)) event.preventDefault();
            }}
            className={cn(
              "aui-composer-input mb-1 max-h-32 w-full resize-none bg-transparent px-4 pt-0.5 pb-3 text-foreground outline-none placeholder:text-muted-foreground/70 focus-visible:ring-0",
              d("h-input"),
              d("text-base"),
              isDictating && "invisible",
            )}
            rows={1}
            autoFocus={autoFocus && !isReplay}
            disabled={isReplay}
            aria-label="Message input"
          />
          <ComposerAction showRunState={showThreadAffordances} />
        </ComposerPrimitive.Root>
      )}
    </div>
  );
};

/**
 * Live feedback while dictating: finalized speech lands in the input, interim
 * words only exist in the transcript primitive until the recognizer commits.
 */
const DICTATION_BAR_COUNT = 28;

/**
 * The scrolling level trail shown left of the mic while dictating: newest
 * sample sits next to the button, so speech visibly flows into it.
 */
const ComposerDictationWave: FC = () => {
  // The recognizer's interim transcript is the speech signal — see
  // useDictationLevels for why this doesn't tap the microphone directly.
  const transcript = useAuiState(
    ({ composer }) => composer.dictation?.transcript,
  );
  const levels = useDictationLevels(transcript, DICTATION_BAR_COUNT);

  return (
    <div
      aria-hidden="true"
      className="aui-composer-dictation-wave mr-1 flex h-[34px] items-center gap-[3px]"
    >
      {levels.map((level, index) => (
        <span
          key={index}
          className="aui-composer-dictation-bar w-[2px] shrink-0 rounded-full bg-muted-foreground/60"
          // Floor of 2px keeps the row reading as a dotted line while silent,
          // exactly like the reference. No CSS transition: the value already
          // updates every frame, and a transition would only damp the peaks.
          style={{ height: `${(2 + level * 16).toFixed(1)}px` }}
        />
      ))}
    </div>
  );
};

/**
 * Command list shown above the composer while the draft is a `/` query.
 * Selection is owned by the composer so Enter and click resolve to the same
 * row; rows use onMouseDown-prevent so clicking one doesn't blur the input
 * (which would clear the query before the click lands).
 */
const ComposerSlashCommandMenu: FC<{
  commands: ComposerSlashCommand[];
  activeIndex: number;
  onHover: (index: number) => void;
  onSelect: (command: ComposerSlashCommand) => void;
}> = ({ commands, activeIndex, onHover, onSelect }) => {
  const r = useRadius();
  return (
    <div
      role="listbox"
      className={cn(
        "aui-composer-slash-menu absolute bottom-full left-0 z-50 mb-2 max-h-64 w-full overflow-y-auto border border-input bg-background shadow-md",
        r("lg"),
      )}
    >
      {commands.map((command, index) => (
        <button
          key={command.title}
          type="button"
          role="option"
          aria-selected={index === activeIndex}
          onMouseDown={(event) => event.preventDefault()}
          onMouseEnter={() => onHover(index)}
          onClick={() => onSelect(command)}
          className={cn(
            "aui-composer-slash-item flex w-full items-baseline gap-2 px-3 py-2 text-left text-sm",
            index === activeIndex && "bg-muted",
          )}
        >
          <span className="truncate text-foreground">{command.title}</span>
          {command.label && (
            <span className="truncate text-xs text-muted-foreground">
              {command.label}
            </span>
          )}
        </button>
      ))}
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

/** Rail entry for the skills half of the context picker. */
const CONTEXT_SKILLS_SECTION = "__skills__";
/** Rail entry that lists every tool, ungrouped. */
const CONTEXT_ALL_TOOLS_SECTION = "__all_tools__";

/**
 * One "Add context" control over both things a message can carry: skills and
 * tool mentions.
 *
 * These were two adjacent buttons, both drawn as an `@`, which read as the
 * same affordance twice. They stay distinct underneath — picking a skill
 * toggles it on the composer's skill context, picking a tool writes an
 * `@mention` into the draft — but the user makes one trip to one list.
 */
const ComposerContextPicker: FC = () => {
  const { config, mcpTools, mcpToolsLoading } = useElements();
  const aui = useAui();
  // Read the composer text from the same reactive source the tool-mention
  // badges parse, so an inserted mention renders a pill just like the type-`@`
  // autocomplete does.
  const composerText = useAuiState(({ composer }) => composer.text);
  const skillContext = config.composer?.skillContext;
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  // Null until the user picks a rail entry, so the default can follow what
  // actually loaded rather than latching whatever was there on first render.
  const [section, setSection] = useState<string | null>(null);

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

  // Both halves stay visible while their source is still loading, so the
  // button appears immediately rather than popping in once the async list
  // resolves — but a half that loaded empty is dropped, and a button with
  // nothing behind it at all is not rendered.
  const hasSkills =
    !!skillContext && (skillContext.skills.length > 0 || skillContext.loading);
  const hasTools = toolMentionsEnabled && (tools.length > 0 || mcpToolsLoading);
  if (!hasSkills && !hasTools) {
    return null;
  }

  // A picked rail entry can stop existing under the user — tools refresh and
  // lose a category, or a half loads empty. Fall back rather than leave the
  // pane pointed at something that is no longer there.
  const sectionExists =
    section === CONTEXT_SKILLS_SECTION
      ? hasSkills
      : section === CONTEXT_ALL_TOOLS_SECTION
        ? hasTools
        : hasTools && categories.some((category) => category.name === section);
  const activeSection =
    section !== null && sectionExists
      ? section
      : hasSkills
        ? CONTEXT_SKILLS_SECTION
        : CONTEXT_ALL_TOOLS_SECTION;
  const normalizedQuery = deferredQuery.trim().toLowerCase();
  const searching = normalizedQuery !== "";

  const selectedIDs = new Set(skillContext?.selectedSkillIds ?? []);
  const maxSelected = skillContext?.maxSelected ?? 10;

  const matchingSkills = (skillContext?.skills ?? []).filter(
    (skill) =>
      !searching ||
      skill.displayName.toLowerCase().includes(normalizedQuery) ||
      skill.name.toLowerCase().includes(normalizedQuery) ||
      (skill.summary?.toLowerCase().includes(normalizedQuery) ?? false),
  );

  // A search spans both halves; the rail only narrows the browse view.
  const toolsInSection =
    searching || activeSection === CONTEXT_ALL_TOOLS_SECTION
      ? tools
      : (categories.find((c) => c.name === activeSection)?.tools ?? []);
  const matchingTools = toolsInSection.filter(
    (tool) =>
      !searching ||
      tool.name.toLowerCase().includes(normalizedQuery) ||
      (tool.description?.toLowerCase().includes(normalizedQuery) ?? false),
  );

  const reset = () => {
    setQuery("");
    setSection(null);
  };

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) {
      reset();
    }
  };

  const toggleSkill = (skillID: string) => {
    if (!skillContext) return;
    if (selectedIDs.has(skillID)) {
      skillContext.onSelectedSkillIdsChange(
        skillContext.selectedSkillIds.filter((id) => id !== skillID),
      );
      setOpen(false);
      reset();
      return;
    }
    if (skillContext.selectedSkillIds.length >= maxSelected) {
      return;
    }
    skillContext.onSelectedSkillIdsChange([
      ...skillContext.selectedSkillIds,
      skillID,
    ]);
    setOpen(false);
    reset();
  };

  const insertMention = (toolName: string) => {
    const base =
      composerText && !/\s$/.test(composerText)
        ? `${composerText} `
        : composerText;
    aui.composer().setText(`${base}@${toolName} `);
    setOpen(false);
    reset();
  };

  const showSkills = hasSkills && (!searching || matchingSkills.length > 0);
  const showTools = hasTools && (!searching || matchingTools.length > 0);

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          data-state={open ? "open" : "closed"}
          className="aui-composer-context-picker flex w-fit items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-semibold data-[state=open]:bg-muted-foreground/15 dark:border-muted-foreground/15 dark:hover:bg-muted-foreground/30"
          aria-label="Add context"
        >
          <AtSign className="size-4 stroke-[1.5px]" />
          <span className="aui-composer-context-picker-label">Add context</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        className="aui-composer-context-popover w-[420px] overflow-hidden p-0"
        onEscapeKeyDown={(event) => {
          if (query !== "") {
            event.preventDefault();
            setQuery("");
          }
        }}
      >
        <div className="flex items-center gap-2 border-b border-input px-3 py-2">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <input
            autoFocus
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={
              hasSkills && hasTools
                ? "Search skills and tools…"
                : hasSkills
                  ? "Search skills…"
                  : "Search tools…"
            }
            className="w-full bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground"
            aria-label="Search context"
          />
        </div>
        <div className="flex h-72">
          {/* The rail is a browse aid only; a search reaches across both
              halves, so it is hidden while one is running. */}
          {!searching && (
            <div className="w-36 shrink-0 overflow-y-auto border-r border-input p-2">
              {hasSkills && (
                <button
                  type="button"
                  onClick={() => setSection(CONTEXT_SKILLS_SECTION)}
                  className={cn(
                    "flex w-full items-center justify-between rounded px-2 py-1 text-left text-xs transition-colors",
                    activeSection === CONTEXT_SKILLS_SECTION
                      ? "bg-muted font-medium text-foreground"
                      : "text-muted-foreground hover:bg-muted/60",
                  )}
                >
                  <span className="truncate">Skills</span>
                  <span className="ml-2 shrink-0 tabular-nums opacity-60">
                    {skillContext?.skills.length ?? 0}
                  </span>
                </button>
              )}
              {hasTools && (
                <>
                  <div className="px-2 pt-2 pb-1 text-[10px] font-semibold tracking-wide text-muted-foreground uppercase">
                    Tools
                  </div>
                  <button
                    type="button"
                    onClick={() => setSection(CONTEXT_ALL_TOOLS_SECTION)}
                    className={cn(
                      "flex w-full items-center justify-between rounded px-2 py-1 text-left text-xs transition-colors",
                      activeSection === CONTEXT_ALL_TOOLS_SECTION
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
                      onClick={() => setSection(category.name)}
                      className={cn(
                        "flex w-full items-center justify-between rounded px-2 py-1 text-left text-xs transition-colors",
                        activeSection === category.name
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
                </>
              )}
            </div>
          )}
          <div className="min-w-0 flex-1 overflow-y-auto">
            {searching && !showSkills && !showTools && (
              <div className="px-2 py-6 text-center text-xs text-muted-foreground">
                {/* A search that outruns the fetch has nothing to match yet;
                    saying "nothing found" there reports absence when the
                    answer is simply not back. */}
                {skillContext?.loading || mcpToolsLoading
                  ? "Loading…"
                  : "Nothing found"}
              </div>
            )}
            {showSkills &&
              (searching || activeSection === CONTEXT_SKILLS_SECTION) && (
                <>
                  {searching && (
                    <div className="px-4 pt-3 text-[10px] font-semibold tracking-wide text-muted-foreground uppercase">
                      Skills
                    </div>
                  )}
                  <ContextSkillResults
                    skillContext={skillContext}
                    visibleSkills={matchingSkills}
                    selectedIDs={selectedIDs}
                    maxSelected={maxSelected}
                    onToggle={toggleSkill}
                  />
                </>
              )}
            {showTools &&
              (searching || activeSection !== CONTEXT_SKILLS_SECTION) && (
                <>
                  {searching && (
                    <div className="px-4 pt-3 text-[10px] font-semibold tracking-wide text-muted-foreground uppercase">
                      Tools
                    </div>
                  )}
                  <ContextToolResults
                    tools={matchingTools}
                    loading={mcpToolsLoading}
                    onSelect={insertMention}
                  />
                </>
              )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
};

function ContextToolResults({
  tools,
  loading,
  onSelect,
}: {
  tools: MentionableTool[];
  loading: boolean;
  onSelect: (toolName: string) => void;
}): React.ReactElement {
  if (tools.length === 0) {
    return (
      <div className="px-2 py-6 text-center text-xs text-muted-foreground">
        {loading ? "Loading tools…" : "No tools found"}
      </div>
    );
  }
  return (
    <div className="p-2">
      {tools.map((tool) => (
        <button
          key={tool.id}
          type="button"
          onClick={() => onSelect(tool.name)}
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
      ))}
    </div>
  );
}

const ComposerSkillContextBadges: FC = () => {
  const skillContext = useElements().config.composer?.skillContext;
  if (!skillContext || skillContext.selectedSkillIds.length === 0) {
    return null;
  }

  const selectedIDs = new Set(skillContext.selectedSkillIds);
  const selectedSkills = skillContext.skills.filter((skill) =>
    selectedIDs.has(skill.id),
  );

  return (
    <div className="aui-composer-skill-context-badges flex flex-wrap gap-1 px-3 pt-1">
      {selectedSkills.map((skill) => (
        <span
          key={skill.id}
          className="flex max-w-full items-center gap-1 rounded-md border border-input bg-muted px-2 py-1 text-xs text-foreground"
        >
          <AtSign className="size-3 shrink-0 text-muted-foreground" />
          <span className="truncate">{skill.displayName}</span>
          <button
            type="button"
            onClick={() =>
              skillContext.onSelectedSkillIdsChange(
                skillContext.selectedSkillIds.filter((id) => id !== skill.id),
              )
            }
            className="ml-0.5 shrink-0 text-muted-foreground hover:text-foreground"
            aria-label={`Remove ${skill.displayName} context`}
          >
            ×
          </button>
        </span>
      ))}
    </div>
  );
};

function ContextSkillResults({
  skillContext,
  visibleSkills,
  selectedIDs,
  maxSelected,
  onToggle,
}: {
  skillContext: SkillContextConfig | undefined;
  visibleSkills: ComposerSkill[];
  selectedIDs: Set<string>;
  maxSelected: number;
  onToggle: (skillID: string) => void;
}): React.ReactElement {
  if (skillContext?.loading) {
    return (
      <div className="px-3 py-8 text-center text-xs text-muted-foreground">
        Loading skills…
      </div>
    );
  }
  if (skillContext?.error) {
    return (
      <div className="px-3 py-8 text-center text-xs text-muted-foreground">
        Unable to load skills
      </div>
    );
  }
  if (visibleSkills.length === 0) {
    return (
      <div className="px-3 py-8 text-center text-xs text-muted-foreground">
        No skills found
      </div>
    );
  }

  const atLimit = selectedIDs.size >= maxSelected;
  return (
    <div className="max-h-72 overflow-y-auto p-2">
      {visibleSkills.map((skill) => {
        const selected = selectedIDs.has(skill.id);
        return (
          <button
            key={skill.id}
            type="button"
            onClick={() => onToggle(skill.id)}
            disabled={atLimit && !selected}
            aria-pressed={selected}
            className="flex w-full items-start gap-2 rounded px-2 py-2 text-left transition-colors hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span className="mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border border-input">
              {selected ? <CheckIcon className="size-3" /> : null}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium text-foreground">
                {skill.displayName}
              </span>
              <span className="block truncate font-mono text-[11px] text-muted-foreground">
                {skill.name}
              </span>
              {skill.summary ? (
                <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">
                  {skill.summary}
                </span>
              ) : null}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/**
 * Push-to-talk mic. Rendered only when the browser exposes the Web Speech API —
 * without an adapter the primitive's click handler is null, so the button would
 * look live but do nothing.
 */
const ComposerDictate: FC = () => {
  const r = useRadius();
  // `composer.dictation` holds the live session and is undefined otherwise.
  const isDictating = useAuiState(({ composer }) => composer.dictation != null);

  if (isDictating) {
    return (
      <>
        <ComposerDictationWave />
        <ComposerPrimitive.StopDictation asChild>
          <TooltipIconButton
            tooltip="Stop dictation"
            side="bottom"
            type="button"
            variant="ghost"
            size="icon"
            className={cn(
              "aui-composer-dictate size-[34px] bg-blue-600 p-1 text-white hover:bg-blue-600/85 hover:text-white",
              r("full"),
            )}
            aria-label="Stop dictation"
          >
            <Mic className="aui-composer-dictate-icon size-5 stroke-[1.5px]" />
          </TooltipIconButton>
        </ComposerPrimitive.StopDictation>
      </>
    );
  }

  return (
    <ComposerPrimitive.Dictate asChild>
      <TooltipIconButton
        tooltip="Dictate message"
        side="bottom"
        type="button"
        variant="ghost"
        size="icon"
        className={cn("aui-composer-dictate size-[34px] p-1", r("full"))}
        aria-label="Dictate message"
      >
        <Mic className="aui-composer-dictate-icon size-5 stroke-[1.5px]" />
      </TooltipIconButton>
    </ComposerPrimitive.Dictate>
  );
};

const ComposerAction: FC<{ showRunState?: boolean }> = ({
  showRunState = true,
}) => {
  const { config } = useElements();
  const r = useRadius();
  const composerConfig = config.composer ?? {};
  // `?? true`, not a default object: a config that sets any other composer key
  // (placeholder, skillContext) would otherwise leave `attachments` undefined
  // and silently drop the attach button.
  const attachmentsEnabled = composerConfig.attachments ?? true;
  return (
    <div className="aui-composer-action-wrapper relative mx-1.5 mt-1 mb-2 flex items-center justify-between">
      <div className="aui-composer-action-wrapper-inner flex items-center gap-0.5 text-muted-foreground">
        {attachmentsEnabled ? (
          <ComposerAddAttachment />
        ) : (
          <div className="aui-composer-add-attachment-placeholder" />
        )}

        <ComposerContextPicker />

        {CASSETTE_RECORDING_ENABLED && <ComposerCassetteRecorder />}
      </div>

      {/* Claude's ordering: composition tools on the left, model + voice +
          send on the right, closest to where the eye lands after typing. */}
      <div className="aui-composer-action-send-group flex items-center gap-1.5">
        {config.model?.showModelPicker && !config.languageModel && (
          <ComposerModelPicker />
        )}

        {dictationAdapter && <ComposerDictate />}

        {/* A standalone entry-point composer (chat home, project home, the
            docked pill) shares the runtime with whatever conversation is
            already streaming, but it does not OWN that run: showing its stop
            button there offers to cancel a turn the user cannot even see. It
            always shows send, and starts a fresh thread instead. */}
        {!showRunState && (
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
        )}

        {showRunState && (
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
        )}

        {showRunState && (
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
        )}
      </div>
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

// The trailing terse line of a text part immediately followed by tool calls
// is the group's annotation — ToolGroup renders it as the group heading, so
// the prose render here drops it to avoid showing it twice. A pure annotation
// part renders nothing; a mixed prose+annotation part renders the prose only.
const withToolCallAnnotationSuppression = (
  Inner: TextMessagePartComponent,
): TextMessagePartComponent => {
  const AssistantText: TextMessagePartComponent = (props) => {
    const aui = useAui();
    const partQuery = aui.part.query;
    const partIndex = partQuery?.type === "index" ? partQuery.index : undefined;
    const followedByToolCall = useAuiState(
      ({ message }) =>
        partIndex !== undefined &&
        message.parts[partIndex + 1]?.type === "tool-call",
    );
    if (!followedByToolCall || !trailingAnnotationLine(props.text)) {
      return <Inner {...props} />;
    }
    const remainder = stripTrailingAnnotationLine(props.text);
    if (!remainder) return null;
    // MarkdownText reads its text from part context, not props — override the
    // context so the annotation line disappears from the prose render.
    return (
      <TextMessagePartProvider
        text={remainder}
        isRunning={props.status?.type === "running"}
      >
        <Inner {...props} text={remainder} />
      </TextMessagePartProvider>
    );
  };
  return AssistantText;
};

const AssistantMessage: FC = () => {
  const { config } = useElements();
  const toolsConfig = config.tools ?? {};
  const components = config.components;
  const toolsComponents = toolsConfig.components;

  const partsComponents = useMemo(() => {
    const ToolGroupComponent = components?.ToolGroup ?? ToolGroup;
    const ReasoningGroupComponent =
      components?.ReasoningGroup ?? ReasoningGroup;
    // Dispatches each cluster from groupAssistantMessageParts: tool runs get
    // the ToolGroup treatment, reasoning runs the ReasoningGroup one, and
    // ungrouped parts render bare.
    const Group: FC<
      PropsWithChildren<{ groupKey: string | undefined; indices: number[] }>
    > = ({ groupKey, indices, children }) => {
      if (groupKey?.startsWith("tools-")) {
        return (
          <ToolGroupComponent indices={indices}>{children}</ToolGroupComponent>
        );
      }
      if (groupKey?.startsWith("reasoning-")) {
        return (
          <ReasoningGroupComponent
            startIndex={indices[0] ?? 0}
            endIndex={indices[indices.length - 1] ?? 0}
          >
            {children}
          </ReasoningGroupComponent>
        );
      }
      return children;
    };
    return {
      Text: withToolCallAnnotationSuppression(components?.Text ?? MarkdownText),
      Image: components?.Image ?? Image,
      tools: {
        by_name: toolsComponents,
        Fallback: components?.ToolFallback ?? ToolFallback,
      },
      Reasoning: components?.Reasoning ?? Reasoning,
      Group,
    };
  }, [components, toolsComponents]);

  return (
    <MessagePrimitive.Root asChild>
      <div
        className="aui-assistant-message-root relative mx-auto w-full animate-in py-4 duration-150 ease-out fade-in slide-in-from-bottom-1 last:mb-24"
        data-role="assistant"
      >
        <div className="aui-assistant-message-content mx-2 leading-7 wrap-break-word text-foreground">
          <MessagePrimitive.Unstable_PartsGrouped
            groupingFunction={groupAssistantMessageParts}
            components={partsComponents}
          />
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
  const hasCopyableText = useAuiState(({ message }) =>
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
  // An attachment-only turn carries no text part (or an empty one). Without
  // this the bubble still renders as an empty coloured pill under the file,
  // and the edit affordance offers to edit nothing.
  const hasText = useAuiState(({ message }) =>
    message.parts.some(
      (part) => part.type === "text" && part.text.trim() !== "",
    ),
  );
  return (
    <MessagePrimitive.Root asChild>
      <div
        className="aui-user-message-root mx-auto grid w-full animate-in auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] gap-y-2 px-2 py-4 duration-150 ease-out fade-in slide-in-from-bottom-1 first:mt-3 last:mb-5 [&:where(>*)]:col-start-2"
        data-role="user"
      >
        <UserMessageAttachments />

        <div className="aui-user-message-content-wrapper relative col-start-2 min-w-0">
          <UserMessageHeader />
          {hasText && (
            <div
              className={cn(
                "aui-user-message-content bg-primary text-primary-foreground ml-auto w-fit px-5 py-2.5 wrap-break-word",
                r("xl"),
              )}
            >
              <MessagePrimitive.Parts components={{ Text: UserMessageText }} />
            </div>
          )}
          {allowEdit && hasText && (
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
  const id = useAuiState(
    ({ threadListItem }) =>
      threadListItem.remoteId ?? threadListItem.externalId,
  );
  const owner = useThreadMeta(id ?? undefined)?.owner;
  const createdAt = useAuiState(({ message }) => message.createdAt);
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
