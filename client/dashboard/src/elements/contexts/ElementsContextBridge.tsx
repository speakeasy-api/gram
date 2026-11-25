import {
  type AssistantClient,
  AuiConfig,
  AuiProvider,
  useAui,
} from "@assistant-ui/react";
import {
  type ContextType,
  type ReactElement,
  type ReactNode,
  useContext,
  useLayoutEffect,
  useMemo,
  useSyncExternalStore,
} from "react";
import { ChatIdContext } from "./ChatIdContext";
import { ConnectionStatusContext } from "./ConnectionStatusContext";
import { ElementsContext, ToolApprovalContext } from "./contexts";
import { MarkdownLinkContext } from "./MarkdownLinkContext";
import { ThreadMetaContext } from "./ThreadMetaContext";
import { ToolExecutionContext } from "./ToolExecutionContext";

interface ElementsContextValues {
  aui: AssistantClient | null;
  chatId: ContextType<typeof ChatIdContext>;
  connectionStatus: ContextType<typeof ConnectionStatusContext>;
  elements: ContextType<typeof ElementsContext>;
  markdownLink: ContextType<typeof MarkdownLinkContext>;
  threadMeta: ContextType<typeof ThreadMetaContext>;
  toolApproval: ContextType<typeof ToolApprovalContext>;
  toolExecution: ContextType<typeof ToolExecutionContext>;
}

// The default value of each context, for a target with no mounted source.
const NO_VALUES: ElementsContextValues = {
  aui: null,
  chatId: null,
  connectionStatus: null,
  elements: undefined,
  markdownLink: {},
  threadMeta: {},
  toolApproval: null,
  toolExecution: null,
};

const NO_SCOPES = AuiConfig({});

interface Published {
  sourceId: string;
  values: ElementsContextValues;
}

export interface ElementsContextBridge {
  subscribe: (listener: () => void) => () => void;
  get: () => Published | null;
  set: (next: Published | null) => void;
}

export function createElementsContextBridge(): ElementsContextBridge {
  let published: Published | null = null;
  const listeners = new Set<() => void>();
  return {
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    get: () => published,
    set: (next) => {
      published = next;
      listeners.forEach((listener) => listener());
    },
  };
}

/**
 * Reports which source is currently published. Publishing happens in a layout
 * effect, so a caller that gates on this value binds to a newly mounted source
 * before the browser paints.
 */
export function usePublishedElementsSource(
  bridge: ElementsContextBridge,
): string | null {
  return useSyncExternalStore(
    bridge.subscribe,
    () => bridge.get()?.sourceId ?? null,
  );
}

/**
 * Publishes the surrounding ElementsProvider's contexts. Render it anywhere
 * inside the provider.
 */
export function ElementsContextSource({
  bridge,
  sourceId,
}: {
  bridge: ElementsContextBridge;
  sourceId: string;
}): null {
  const aui = useAui();
  const chatId = useContext(ChatIdContext);
  const connectionStatus = useContext(ConnectionStatusContext);
  const elements = useContext(ElementsContext);
  const markdownLink = useContext(MarkdownLinkContext);
  const threadMeta = useContext(ThreadMetaContext);
  const toolApproval = useContext(ToolApprovalContext);
  const toolExecution = useContext(ToolExecutionContext);

  const values = useMemo<ElementsContextValues>(
    () => ({
      aui,
      chatId,
      connectionStatus,
      elements,
      markdownLink,
      threadMeta,
      toolApproval,
      toolExecution,
    }),
    [
      aui,
      chatId,
      connectionStatus,
      elements,
      markdownLink,
      threadMeta,
      toolApproval,
      toolExecution,
    ],
  );

  useLayoutEffect(() => {
    bridge.set({ sourceId, values });
  }, [bridge, sourceId, values]);
  useLayoutEffect(() => () => bridge.set(null), [bridge]);

  return null;
}

/**
 * Gives `children` the contexts of the provider that holds the matching
 * ElementsContextSource, without being its descendant. The provider can then
 * mount, unmount and remount beside `children` without resetting their state,
 * and the tree here keeps its shape whether or not a source is published.
 *
 * Children still have to wait for a published source before they read the chat
 * runtime, as they would have to wait for the provider itself.
 */
export function ElementsContextTarget({
  bridge,
  active = true,
  children,
}: {
  bridge: ElementsContextBridge;
  active?: boolean;
  children: ReactNode;
}): ReactElement {
  const published = useSyncExternalStore(bridge.subscribe, bridge.get);
  // Stop inheriting the old runtime during render, before source cleanup runs.
  const values = active ? (published?.values ?? NO_VALUES) : NO_VALUES;

  return (
    <AuiProvider extends={values.aui} config={NO_SCOPES}>
      <ConnectionStatusContext.Provider value={values.connectionStatus}>
        <ToolApprovalContext.Provider value={values.toolApproval}>
          <MarkdownLinkContext.Provider value={values.markdownLink}>
            <ThreadMetaContext.Provider value={values.threadMeta}>
              <ChatIdContext.Provider value={values.chatId}>
                <ElementsContext.Provider value={values.elements}>
                  <ToolExecutionContext.Provider value={values.toolExecution}>
                    {children}
                  </ToolExecutionContext.Provider>
                </ElementsContext.Provider>
              </ChatIdContext.Provider>
            </ThreadMetaContext.Provider>
          </MarkdownLinkContext.Provider>
        </ToolApprovalContext.Provider>
      </ConnectionStatusContext.Provider>
    </AuiProvider>
  );
}
