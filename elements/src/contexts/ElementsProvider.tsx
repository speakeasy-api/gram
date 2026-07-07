import { FrontendTools } from "@/components/FrontendTools";
import { ROOT_SELECTOR } from "@/constants/tailwind";
import {
  isLocalThreadId,
  useGramThreadListAdapter,
} from "@/hooks/useGramThreadListAdapter";
import { useMCPTools } from "@/hooks/useMCPTools";
import { useToolApproval } from "@/hooks/useToolApproval";
import { getApiUrl } from "@/lib/api";
import { initErrorTracking, trackError } from "@/lib/errorTracking";
import { MODELS } from "@/lib/models";
import {
  clearFrontendToolApprovalConfig,
  getEnabledTools,
  setFrontendToolApprovalConfig,
  toAISDKTools,
  wrapToolsWithApproval,
  wrapToolsWithByteCap,
  type ApprovalHelpers,
} from "@/lib/tools";
import { compactForModel } from "@/lib/contextCompaction";
import { describeStreamError } from "@/lib/streamErrorMessage";
import { cn } from "@/lib/utils";
import { recommended } from "@/plugins";
import { ElementsConfig, Model } from "@/types";
import { Plugin } from "@/types/plugins";
import {
  AssistantRuntimeProvider,
  AssistantTool,
  useAssistantState,
  unstable_useRemoteThreadListRuntime as useRemoteThreadListRuntime,
} from "@assistant-ui/react";
import {
  frontendTools as convertFrontendToolsToAISDKTools,
  useChatRuntime,
} from "@assistant-ui/react-ai-sdk";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  convertToModelMessages,
  createUIMessageStream,
  lastAssistantMessageIsCompleteWithToolCalls,
  LanguageModel,
  smoothStream,
  stepCountIs,
  streamText,
  ToolSet,
  type ChatTransport,
  type UIMessage,
} from "ai";

type UIMessagePart = UIMessage["parts"][number];
import {
  ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useAuth } from "../hooks/useAuth";
import { ChatIdContext } from "./ChatIdContext";
import {
  ConnectionStatusProvider,
  useConnectionStatusOptional,
} from "./ConnectionStatusContext";
import { ElementsContext } from "./contexts";
import { ToolApprovalProvider } from "./ToolApprovalContext";
import { ToolExecutionProvider } from "./ToolExecutionContext";

// Reads the active local thread id from the runtime's threads store. Goes
// through assistant-ui's public ThreadListRuntime.getState() API.
function getActiveLocalThreadId(
  runtimeRef: React.RefObject<ReturnType<typeof useChatRuntime> | null>,
): string | undefined {
  const threadsState = runtimeRef.current?.threads.getState();
  if (!threadsState) return undefined;
  // `mainThreadId` is always populated by the SDK; the secondary read is a
  // defensive fallback in case the SDK ever returns a state shape with an
  // older `threadIds` field instead. The cast widens to an indexable shape
  // because `ThreadListState` doesn't declare that historical field.
  const legacy = (threadsState as { threadIds?: readonly string[] }).threadIds;
  return threadsState.mainThreadId ?? legacy?.[0];
}

type ExecutableTool = {
  execute?: (args: unknown, options?: unknown) => Promise<unknown>;
};

/**
 * Extracts executable tools from frontend tool definitions.
 * Frontend tools created via defineFrontendTool have an unstable_tool property
 * that contains the tool definition with execute function.
 *
 * The AI SDK's `ToolExecuteFunction<INPUT, OUTPUT>` signature is too strict on
 * its second parameter (a typed `ToolCallOptions`) and too broad on its return
 * (`AsyncIterable | PromiseLike | OUTPUT`) to match `ExecutableTool.execute`
 * directly. The reference is copied as-is — no runtime wrapping — and only the
 * type surface is widened.
 */
function extractExecutableTools(
  frontendTools: Record<string, AssistantTool> | undefined,
): Record<string, ExecutableTool> {
  if (!frontendTools) return {};

  return Object.fromEntries(
    Object.entries(frontendTools).map(([name, tool]) => {
      const toolDef = tool.unstable_tool as {
        execute?: ExecutableTool["execute"];
      };
      return [name, { execute: toolDef.execute }];
    }),
  );
}

export interface ElementsProviderProps {
  children: ReactNode;
  config: ElementsConfig;
}

const BASE_SYSTEM_PROMPT = `You are a helpful assistant that can answer questions and help with tasks.

Tool Result Display:
Some tools have custom visual components that automatically render their results (you'll see a rich card/widget appear). For these, do not repeat the data - just add brief context or a follow-up question if needed.

For tools WITHOUT custom components, you should present the data clearly - either as plain text for simple results, or using the UI code block format for structured data like lists of items, categories, or dashboards.

UI Widget Guidelines:
IMPORTANT: Only render ONE generative UI widget (chart, dashboard, visualization) per response. Never render multiple widgets in a single message - this causes layout shifts during streaming and overwhelms the user. If you have multiple visualizations to show, render the most important one and explicitly offer to show others as follow-ups (e.g., "Would you like to see a breakdown by status as well?").`;

function mergeInternalSystemPromptWith(
  userSystemPrompt: string | undefined,
  plugins: Plugin[],
  toolsWithCustomComponents: string[],
) {
  const customToolsSection =
    toolsWithCustomComponents.length > 0
      ? `\n\nTools with custom visual components (DO NOT render UI widgets for these - they already display rich visuals):\n${toolsWithCustomComponents.map((t) => `- ${t}`).join("\n")}`
      : "";

  return `
  ${BASE_SYSTEM_PROMPT}${customToolsSection}

  User-provided System Prompt:
  ${userSystemPrompt ?? "None provided"}

  Utilities:
  ${plugins.map((plugin) => `- ${plugin.language}: ${plugin.prompt}`).join("\n")}`;
}

/**
 * Cleans messages before sending to the model to work around an AI SDK bug.
 * Strips callProviderMetadata from all parts (AI SDK bug #9731)
 */
function cleanMessagesForModel(messages: UIMessage[]): UIMessage[] {
  return messages.map((message) => {
    const partsArray = message.parts;
    if (!Array.isArray(partsArray)) {
      return message;
    }

    // Process each part: strip providerOptions/providerMetadata and filter reasoning.
    // `callProviderMetadata` is not declared on `UIMessagePart`, so we widen the
    // part to an indexable record just for the destructure.
    const cleanedParts = partsArray.map((part) => {
      const { callProviderMetadata: _omit, ...cleanPart } =
        part as UIMessagePart & { callProviderMetadata?: unknown };
      void _omit;
      return cleanPart as UIMessagePart;
    });

    return {
      ...message,
      parts: cleanedParts,
    };
  });
}

/**
 * Main provider component that sets up auth, tools, and transport.
 * Delegates to either WithHistory or WithoutHistory based on config.
 */
const ElementsProviderInner = ({ children, config }: ElementsProviderProps) => {
  const apiUrl = getApiUrl(config);
  const auth = useAuth({
    auth: config.api,
    projectSlug: config.projectSlug,
  });

  // Ref to access ensureValidHeaders in async transport without stale closures
  const ensureValidHeadersRef = useRef(auth.ensureValidHeaders);
  ensureValidHeadersRef.current = auth.ensureValidHeaders;
  // Stable async header resolution for the thread-list adapter: awaits the
  // session fetch when auth hasn't settled yet, so the history runtime can
  // mount before auth resolves.
  const getValidHeaders = useCallback(
    () => ensureValidHeadersRef.current(),
    [],
  );
  const toolApproval = useToolApproval();

  const [model, setModel] = useState<Model>(
    config.model?.defaultModel ?? MODELS[0],
  );
  const [isExpanded, setIsExpanded] = useState(
    config.modal?.defaultExpanded ?? false,
  );
  const [isOpen, setIsOpen] = useState(config.modal?.defaultOpen);

  const plugins = config.plugins ?? recommended;

  // Get list of tools that have custom components registered
  const toolsWithCustomComponents = Object.keys(config.tools?.components ?? {});

  const systemPrompt = mergeInternalSystemPromptWith(
    config.systemPrompt,
    plugins,
    toolsWithCustomComponents,
  );

  // Read inside `sendMessages` via ref so prompt changes don't churn the
  // transport useMemo identity. Same pattern as ensureValidHeadersRef /
  // approvalHelpersRef below.
  const systemPromptRef = useRef(systemPrompt);
  systemPromptRef.current = systemPrompt;

  // Initialize error tracking on mount
  useEffect(() => {
    initErrorTracking({
      enabled: config.errorTracking?.enabled,
      projectSlug: config.projectSlug,
      variant: config.variant,
    });
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- one-time init at mount; later config changes are intentionally ignored
  }, []);

  // Generate a stable chat ID for server-side persistence (when history is disabled)
  // When history is enabled, the thread adapter manages chat IDs instead
  const chatIdRef = useRef<string | null>(null);

  // State to expose the current chat ID via context
  const [currentChatId, setCurrentChatId] = useState<string | null>(null);

  const {
    data: mcpTools,
    mcpHeaders,
    isLoading: mcpQueryLoading,
  } = useMCPTools({
    auth,
    mcp: config.mcp,
    mcps: config.mcps,
    environment: config.environment ?? {},
    toolsToInclude: config.tools?.toolsToInclude,
    gramEnvironment: config.gramEnvironment,
  });
  // Treat auth-loading as "tools not yet resolved" too — the MCP query is
  // disabled (and so not "loading") until auth settles, so without this a
  // tool-list consumer would briefly see an empty, settled state before tools
  // arrive.
  const mcpToolsLoading = auth.isLoading || mcpQueryLoading;

  // Store approval helpers in ref so they can be used in async contexts
  const approvalHelpersRef = useRef<ApprovalHelpers>({
    requestApproval: toolApproval.requestApproval,
    isToolApproved: toolApproval.isToolApproved,
    whitelistTool: toolApproval.whitelistTool,
  });

  // Connection status for tracking network failures
  const connectionStatus = useConnectionStatusOptional();

  approvalHelpersRef.current = {
    requestApproval: toolApproval.requestApproval,
    isToolApproved: toolApproval.isToolApproved,
    whitelistTool: toolApproval.whitelistTool,
  };

  const getApprovalHelpers = useCallback((): ApprovalHelpers => {
    return {
      requestApproval: (...args) =>
        approvalHelpersRef.current.requestApproval(...args),
      isToolApproved: (...args) =>
        approvalHelpersRef.current.isToolApproved(...args),
      whitelistTool: (...args) =>
        approvalHelpersRef.current.whitelistTool(...args),
    };
  }, []);

  // Set up frontend tool approval config for runtime checking
  useEffect(() => {
    if (config.tools?.toolsRequiringApproval) {
      setFrontendToolApprovalConfig(
        getApprovalHelpers(),
        config.tools.toolsRequiringApproval,
      );
    }
    return () => {
      clearFrontendToolApprovalConfig();
    };
  }, [config.tools?.toolsRequiringApproval, getApprovalHelpers]);

  // Ref to access runtime from within transport's sendMessages.
  // This solves a circular dependency: transport needs runtime.thread.getModelContext(),
  // but runtime is created using transport. The ref gets populated after runtime creation.
  const runtimeRef = useRef<ReturnType<typeof useChatRuntime> | null>(null);

  // Map to share local thread IDs to UUIDs between adapter and transport (for history mode)
  const localIdToUuidMapRef = useRef(new Map<string, string>());

  // Ref to store the current thread's remoteId, synced from assistant-ui state.
  // This is needed because the runtime object doesn't expose threadListItem.remoteId
  // in a way that's accessible from the transport's sendMessages function.
  const currentRemoteIdRef = useRef<string | null>(null);

  // Create chat transport configuration. This is the built-in client-side
  // streaming transport; a consumer can override it via config.transport (see
  // below) to route the conversation through a server-side assistant instead.
  const defaultTransport = useMemo<ChatTransport<UIMessage>>(
    () => ({
      sendMessages: async ({ messages, abortSignal }) => {
        const usingCustomModel = !!config.languageModel;

        if (auth.isLoading) {
          throw new Error("Session is loading");
        }

        // Ensure the session token is still valid; refresh if expired
        const validHeaders = await ensureValidHeadersRef.current();

        // Get chat ID - use the synced remoteId ref first (history mode),
        // fall back to generated ID (non-history mode)
        let chatId = currentRemoteIdRef.current;

        // If we have a valid remoteId (not a local ID), use it directly
        if (chatId && !isLocalThreadId(chatId)) {
          // chatId is already set correctly from the synced ref
        } else if (isLocalThreadId(chatId) || !chatId) {
          // For local thread IDs or no ID, check/generate UUID mapping
          const localThreadId = getActiveLocalThreadId(runtimeRef);
          const lookupKey = chatId ?? localThreadId;
          if (lookupKey) {
            const existingUuid = localIdToUuidMapRef.current.get(lookupKey);
            if (existingUuid) {
              chatId = existingUuid;
            } else {
              // Generate a new UUID and store the mapping
              const newUuid = crypto.randomUUID();
              localIdToUuidMapRef.current.set(lookupKey, newUuid);
              chatId = newUuid;
            }
          }
        }

        if (!chatId) {
          // Non-history mode fallback - use stable chatIdRef
          if (!chatIdRef.current) {
            chatIdRef.current = crypto.randomUUID();
          }
          chatId = chatIdRef.current;
        }

        // Mutate the shared headers object so the MCP transport picks up the
        // chat ID on subsequent tool call requests.
        if (chatId) {
          mcpHeaders["Gram-Chat-ID"] = chatId;
          // Update the context state so consumers can access the current chat ID
          setCurrentChatId(chatId);
        }

        const context = runtimeRef.current?.thread.getModelContext();
        const frontendTools = toAISDKTools(
          getEnabledTools(context?.tools ?? {}),
        );

        // Include Gram-Chat-ID header for chat persistence and Gram-Environment for environment selection
        const headersWithChatId = {
          ...validHeaders,
          "Gram-Chat-ID": chatId,
          "X-Gram-Source": "elements",
          ...config.api?.headers, // We do this after X-Gram-Source so the playground can override it
          ...(config.gramEnvironment && {
            "Gram-Environment": config.gramEnvironment,
          }),
        };

        // Update MCP headers with the (possibly refreshed) session token
        // so mid-stream MCP tool calls use the fresh token
        const freshSession = validHeaders["Gram-Chat-Session"];
        if (freshSession) {
          mcpHeaders["Gram-Chat-Session"] = freshSession;
        }

        // Create OpenRouter model (only needed when not using custom model)
        const openRouterModel = usingCustomModel
          ? null
          : createOpenRouter({
              baseURL: apiUrl,
              apiKey: "unused, but must be set",
              headers: headersWithChatId,
            });

        if (config.languageModel) {
          console.log("Using custom language model", config.languageModel);
        }

        // Combine tools - MCP tools only available when not using custom model
        const combinedTools: ToolSet = {
          ...mcpTools,
          ...convertFrontendToolsToAISDKTools(frontendTools),
        } as ToolSet;

        // Wrap tools that require approval
        const approvedTools = wrapToolsWithApproval(
          combinedTools,
          config.tools?.toolsRequiringApproval,
          getApprovalHelpers(),
        );

        // Cap oversized tool results so one greedy tool call (e.g. a wide log
        // search) can't fill the context window in a single step.
        const tools = wrapToolsWithByteCap(
          approvedTools,
          config.tools?.maxOutputBytes,
        );

        // Stream the response
        const modelToUse = config.languageModel
          ? config.languageModel
          : (openRouterModel!.chat(model) as LanguageModel);

        try {
          // This works around AI SDK bug where these fields cause validation failures
          const cleanedMessages = cleanMessagesForModel(messages);
          // Filter out system messages from the UI state — the system prompt
          // is already provided via the `system:` parameter to streamText().
          // Without this, loaded chat history includes the system message which
          // gets sent alongside the `system:` param, causing duplication.
          const nonSystemMessages = cleanedMessages.filter(
            (m) => m.role !== "system",
          );
          const rawModelMessages =
            await convertToModelMessages(nonSystemMessages);

          // Auto-compact older turns if the estimated input is approaching
          // the model's context window. System prompt + last few turns are
          // always preserved. No-op when the conversation is small.
          const compaction = config.contextCompaction?.disabled
            ? {
                messages: rawModelMessages,
                droppedCount: 0,
                estimatedTokensBefore: 0,
                estimatedTokensAfter: 0,
              }
            : compactForModel(rawModelMessages, model, {
                maxTokens: config.contextCompaction?.maxTokens,
                compactAtFraction: config.contextCompaction?.compactAtFraction,
                keepRecent: config.contextCompaction?.keepRecent,
              });
          if (compaction.droppedCount > 0) {
            console.warn(
              `[elements] compacted ${compaction.droppedCount} older turn(s) from ${compaction.estimatedTokensBefore} → ${compaction.estimatedTokensAfter} est. tokens (model ${model})`,
            );
          }
          const modelMessages = compaction.messages;

          const result = streamText({
            system: systemPromptRef.current,
            model: modelToUse,
            messages: modelMessages,
            tools,
            stopWhen: stepCountIs(10),
            experimental_transform: smoothStream({ delayInMs: 15 }),
            abortSignal,
            onError: ({ error }) => {
              console.error("Stream error in onError callback:", error);
              trackError(error, { source: "streaming" });

              // Check if this is a network/connection error
              const isNetworkError =
                error instanceof TypeError ||
                (error instanceof Error &&
                  (error.message.includes("fetch") ||
                    error.message.includes("network") ||
                    error.message.includes("Failed to fetch") ||
                    error.message.includes("NetworkError") ||
                    error.message.includes("ECONNREFUSED") ||
                    error.message.includes("ETIMEDOUT")));

              if (isNetworkError) {
                connectionStatus?.markDisconnected();
              }
            },
          });

          // Mark as connected when stream starts successfully
          connectionStatus?.markConnected();

          // This weird construction is necessary to get errors to propagate properly to assistant-ui.
          // `originalMessages` is required: without it, `handleUIMessageStreamFinish` injects a
          // fresh random messageId into every `start` chunk. On auto-resume that mismatches the
          // prior assistant message's id, so useChat pushes a new UIMessage carrying the snapshot
          // of the prior turn's parts — duplicating text and tool_calls into storage.
          //
          // onError: AI SDK masks errors by default; surface the friendly
          // credits prompt for 402, otherwise keep the masking intact.
          return createUIMessageStream({
            execute: ({ writer }) => {
              writer.merge(result.toUIMessageStream());
            },
            originalMessages: messages,
            onError: (error) =>
              describeStreamError(error) ??
              "An error occurred while generating a response.",
          });
        } catch (error) {
          console.error("Error creating stream:", error);
          trackError(error, { source: "stream-creation" });

          // Check if this is a network/connection error
          const isNetworkError =
            error instanceof TypeError ||
            (error instanceof Error &&
              (error.message.includes("fetch") ||
                error.message.includes("network") ||
                error.message.includes("Failed to fetch") ||
                error.message.includes("NetworkError") ||
                error.message.includes("ECONNREFUSED") ||
                error.message.includes("ETIMEDOUT")));

          if (isNetworkError) {
            connectionStatus?.markDisconnected();
          }

          throw error;
        }
      },
      reconnectToStream: async () => {
        throw new Error("Stream reconnection not supported");
      },
    }),
    [
      config.languageModel,
      config.tools?.toolsRequiringApproval,
      config.tools?.maxOutputBytes,
      config.contextCompaction?.disabled,
      config.contextCompaction?.maxTokens,
      config.contextCompaction?.compactAtFraction,
      config.contextCompaction?.keepRecent,
      config.gramEnvironment,
      config.api?.headers,
      model,
      mcpTools,
      mcpHeaders,
      getApprovalHelpers,
      apiUrl,
      auth.isLoading,
      connectionStatus,
    ],
  );

  // A consumer-supplied transport (e.g. a server-side assistant transport) takes
  // precedence over the built-in client-side one. It may be a ChatTransport or a
  // factory: a factory is invoked here, inside the provider, with a getChatId()
  // sourced from the synced thread state, so the transport can read the active
  // chat id at send time without reaching into Elements internals. Local
  // (unpersisted) thread ids read as null so the transport can treat them as a
  // brand-new conversation.
  const getChatId = useCallback(() => {
    const id = currentRemoteIdRef.current;
    return id && !isLocalThreadId(id) ? id : null;
  }, []);
  // Capture the active local thread identity now and return a bind function
  // closing over it. Consumer transports call this at the start of
  // `sendMessages`; once a server-minted chat id is known, invoking the
  // returned function reconciles the captured thread to it — the same
  // reconciliation the built-in transport does inline when it generates an id.
  // Closing over the captured id (instead of re-reading active state at bind
  // time) is what makes a thread switch or a parallel send on another thread
  // during the round-trip safe.
  const adoptChatId = useCallback(() => {
    const capturedLocalThreadId = getActiveLocalThreadId(runtimeRef);
    return (chatId: string) => {
      if (capturedLocalThreadId) {
        localIdToUuidMapRef.current.set(capturedLocalThreadId, chatId);
      }
      currentRemoteIdRef.current = chatId;
      mcpHeaders["Gram-Chat-ID"] = chatId;
      setCurrentChatId(chatId);
    };
  }, [mcpHeaders, setCurrentChatId]);
  const configTransport = config.transport;
  // Resolved separately from `defaultTransport` so that churn in the default
  // transport's dependencies (MCP tool discovery settling, connection status,
  // auth refresh) cannot change the transport identity while a custom
  // transport is in use. Transport identity feeds the per-thread runtime hook
  // (`useChatRuntimeHook` → `setRuntimeHook`), and an identity change there
  // rebuilds the thread runtimes — wiping in-flight optimistic messages, e.g.
  // a message sent right after a cold open while MCP tools are still loading.
  const customTransport = useMemo<ChatTransport<UIMessage> | null>(() => {
    if (typeof configTransport === "function") {
      return configTransport({ getChatId, adoptChatId });
    }
    return configTransport ?? null;
  }, [configTransport, getChatId, adoptChatId]);
  const transport = customTransport ?? defaultTransport;

  const historyEnabled = config.history?.enabled ?? false;

  // Shared context value for ElementsContext
  const contextValue = useMemo(
    () => ({
      config,
      setModel,
      model,
      isExpanded,
      setIsExpanded,
      isOpen: isOpen ?? false,
      setIsOpen,
      plugins,
      mcpTools,
      mcpToolsLoading,
    }),
    [config, model, isExpanded, isOpen, plugins, mcpTools, mcpToolsLoading],
  );

  const frontendTools = config.tools?.frontendTools ?? {};

  // Create combined executable tools for direct tool execution (ActionButton)
  // Uses a simplified type that focuses on the execute function
  type ExecutableToolSet = Record<
    string,
    | { execute?: (args: unknown, options?: unknown) => Promise<unknown> }
    | undefined
  >;
  const executableTools = useMemo<ExecutableToolSet>(() => {
    const extractedFrontendTools = extractExecutableTools(
      config.tools?.frontendTools,
    );
    // MCP tools and extracted frontend tools both have execute functions
    return {
      ...mcpTools,
      ...extractedFrontendTools,
    } as ExecutableToolSet;
  }, [mcpTools, config.tools?.frontendTools]);

  // Render the appropriate runtime provider based on history config.
  // We use separate components to avoid conditional hook calls.
  //
  // The history branch must NOT wait for auth: gating it on `!auth.isLoading`
  // would mount the without-history runtime first and swap it for the history
  // one when auth settles — replacing the runtime and wiping any message sent
  // into the first one (e.g. a prompt queued before a cold open). Instead the
  // history runtime mounts immediately and its adapter awaits auth via
  // `getHeaders` before issuing requests.
  if (historyEnabled) {
    return (
      <ElementsProviderWithHistory
        transport={transport}
        apiUrl={apiUrl}
        headers={auth.headers ?? {}}
        getHeaders={getValidHeaders}
        contextValue={contextValue}
        runtimeRef={runtimeRef}
        frontendTools={frontendTools}
        localIdToUuidMap={localIdToUuidMapRef.current}
        currentRemoteIdRef={currentRemoteIdRef}
        executableTools={executableTools}
        currentChatId={currentChatId}
        setCurrentChatId={setCurrentChatId}
      >
        {children}
      </ElementsProviderWithHistory>
    );
  }

  return (
    <ElementsProviderWithoutHistory
      transport={transport}
      contextValue={contextValue}
      runtimeRef={runtimeRef}
      frontendTools={frontendTools}
      executableTools={executableTools}
      currentChatId={currentChatId}
    >
      {children}
    </ElementsProviderWithoutHistory>
  );
};

// Shared type for executable tools
type ExecutableToolSet = Record<
  string,
  | { execute?: (args: unknown, options?: unknown) => Promise<unknown> }
  | undefined
>;

// Separate component for history-enabled mode to avoid conditional hook calls
interface ElementsProviderWithHistoryProps {
  children: ReactNode;
  transport: ChatTransport<UIMessage>;
  apiUrl: string;
  headers: Record<string, string>;
  getHeaders: () => Promise<Record<string, string>>;
  contextValue: React.ContextType<typeof ElementsContext>;
  runtimeRef: React.RefObject<ReturnType<typeof useChatRuntime> | null>;
  frontendTools: Record<string, AssistantTool>;
  localIdToUuidMap: Map<string, string>;
  currentRemoteIdRef: React.RefObject<string | null>;
  executableTools: ExecutableToolSet;
  currentChatId: string | null;
  setCurrentChatId: (chatId: string | null) => void;
}

/**
 * Component that syncs the current thread's remoteId to a ref and updates the chat ID context.
 * Must be rendered inside AssistantRuntimeProvider to access the state.
 */
const ThreadIdSync = ({
  remoteIdRef,
  onChatIdChange,
}: {
  remoteIdRef: React.RefObject<string | null>;
  onChatIdChange: (chatId: string | null) => void;
}) => {
  const remoteId = useAssistantState(
    ({ threadListItem }) => threadListItem.remoteId ?? null,
  );
  useEffect(() => {
    remoteIdRef.current = remoteId;
    onChatIdChange(remoteId);
  }, [remoteId, remoteIdRef, onChatIdChange]);
  return null;
};

const ElementsProviderWithHistory = ({
  children,
  transport,
  apiUrl,
  headers,
  getHeaders,
  contextValue,
  runtimeRef,
  frontendTools,
  localIdToUuidMap,
  currentRemoteIdRef,
  executableTools,
  currentChatId,
  setCurrentChatId,
}: ElementsProviderWithHistoryProps) => {
  const threadListAdapter = useGramThreadListAdapter({
    apiUrl,
    headers,
    getHeaders,
    localIdToUuidMap,
    threadListFilters: contextValue?.config.history?.threadListFilters,
    deferThreadIdMinting: contextValue?.config.history?.deferThreadIdMinting,
    transformChatMessage: contextValue?.config.history?.transformChatMessage,
    resolveCreator: contextValue?.config.history?.resolveCreator,
    isOwnChat: contextValue?.config.history?.isOwnChat,
  });
  const initialThreadId = contextValue?.config.history?.initialThreadId;

  // Without `sendAutomaticallyWhen`, client-side frontend tools leave the turn
  // half-finished: the tool-result is patched in but the agent never resumes,
  // so the next user message lands on top of an unresolved tool-call sequence.
  const useChatRuntimeHook = useCallback(() => {
    // oxlint-disable-next-line react-hooks/rules-of-hooks -- intentional: useChatRuntime is invoked by useRemoteThreadListRuntime as a hook for each thread
    return useChatRuntime({
      transport,
      sendAutomaticallyWhen: lastAssistantMessageIsCompleteWithToolCalls,
    });
  }, [transport]);

  const runtime = useRemoteThreadListRuntime({
    adapter: threadListAdapter,
    runtimeHook: useChatRuntimeHook,
  });

  // Populate runtimeRef so transport can access thread context
  useEffect(() => {
    runtimeRef.current = runtime as ReturnType<typeof useChatRuntime>;
  }, [runtime, runtimeRef]);

  // Switch to initial thread if provided (for shared chat URLs)
  const initialThreadSwitched = useRef(false);
  useEffect(() => {
    if (initialThreadId && !initialThreadSwitched.current) {
      initialThreadSwitched.current = true;
      // Use setTimeout to ensure runtime is fully initialized
      const timeoutId = setTimeout(() => {
        runtime.threads.switchToThread(initialThreadId).catch((error) => {
          console.error("Failed to switch to initial thread:", error);
        });
      }, 100);
      return () => clearTimeout(timeoutId);
    }
  }, [initialThreadId, runtime]);

  // Get the Provider from our adapter to wrap the content
  const HistoryProvider =
    threadListAdapter.unstable_Provider ??
    (({ children }: { children: React.ReactNode }) => <>{children}</>);

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadIdSync
        remoteIdRef={currentRemoteIdRef}
        onChatIdChange={setCurrentChatId}
      />
      <HistoryProvider>
        <ChatIdContext.Provider value={{ chatId: currentChatId }}>
          <ElementsContext.Provider value={contextValue}>
            <ToolExecutionProvider tools={executableTools}>
              <div
                className={cn(
                  ROOT_SELECTOR,
                  (contextValue?.config.variant === "standalone" ||
                    contextValue?.config.variant === "sidecar") &&
                    "h-full",
                )}
              >
                {children}
              </div>
              <FrontendTools tools={frontendTools} />
            </ToolExecutionProvider>
          </ElementsContext.Provider>
        </ChatIdContext.Provider>
      </HistoryProvider>
    </AssistantRuntimeProvider>
  );
};

// Separate component for non-history mode to avoid conditional hook calls
interface ElementsProviderWithoutHistoryProps {
  children: ReactNode;
  transport: ChatTransport<UIMessage>;
  contextValue: React.ContextType<typeof ElementsContext>;
  runtimeRef: React.RefObject<ReturnType<typeof useChatRuntime> | null>;
  frontendTools: Record<string, AssistantTool>;
  executableTools: ExecutableToolSet;
  currentChatId: string | null;
}

const ElementsProviderWithoutHistory = ({
  children,
  transport,
  contextValue,
  runtimeRef,
  frontendTools,
  executableTools,
  currentChatId,
}: ElementsProviderWithoutHistoryProps) => {
  const runtime = useChatRuntime({
    transport,
    sendAutomaticallyWhen: lastAssistantMessageIsCompleteWithToolCalls,
  });

  // Populate runtimeRef so transport can access thread context
  useEffect(() => {
    runtimeRef.current = runtime;
  }, [runtime, runtimeRef]);

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ChatIdContext.Provider value={{ chatId: currentChatId }}>
        <ElementsContext.Provider value={contextValue}>
          <ToolExecutionProvider tools={executableTools}>
            <div
              className={cn(
                ROOT_SELECTOR,
                (contextValue?.config.variant === "standalone" ||
                  contextValue?.config.variant === "sidecar") &&
                  "h-full",
              )}
            >
              {children}
            </div>
            <FrontendTools tools={frontendTools} />
          </ToolExecutionProvider>
        </ElementsContext.Provider>
      </ChatIdContext.Provider>
    </AssistantRuntimeProvider>
  );
};

const queryClient = new QueryClient();

export const ElementsProvider = (
  props: ElementsProviderProps,
): React.JSX.Element => {
  return (
    <QueryClientProvider client={queryClient}>
      <ConnectionStatusProvider>
        <ToolApprovalProvider>
          <ElementsProviderInner {...props} />
        </ToolApprovalProvider>
      </ConnectionStatusProvider>
    </QueryClientProvider>
  );
};
