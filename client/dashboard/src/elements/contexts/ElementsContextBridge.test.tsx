import {
  AssistantRuntimeProvider,
  type ChatModelAdapter,
  type RemoteThreadListAdapter,
  useAui,
  useAuiState,
  useLocalRuntime,
  useRemoteThreadListRuntime,
} from "@assistant-ui/react";
import { act, cleanup, render, screen } from "@testing-library/react";
import {
  type ContextType,
  type ReactNode,
  useContext,
  useEffect,
  useState,
} from "react";
import { afterEach, describe, expect, it } from "vitest";
import {
  createElementsContextBridge,
  ElementsContextSource,
  ElementsContextTarget,
  usePublishedElementsSource,
} from "./ElementsContextBridge";
import { ChatIdContext } from "./ChatIdContext";
import {
  ConnectionStatusContext,
  ConnectionStatusProvider,
} from "./ConnectionStatusContext";
import { ToolApprovalContext } from "./contexts";
import { MarkdownLinkContext } from "./MarkdownLinkContext";
import { ThreadMetaContext } from "./ThreadMetaContext";
import { ToolApprovalProvider } from "./ToolApprovalContext";
import {
  ToolExecutionContext,
  ToolExecutionProvider,
} from "./ToolExecutionContext";

afterEach(cleanup);

const model: ChatModelAdapter = {
  run: async () => ({ content: [{ type: "text", text: "reply" }] }),
};

function Source({
  bridge,
  sourceId,
  chatId,
}: {
  bridge: ReturnType<typeof createElementsContextBridge>;
  sourceId: string;
  chatId: string;
}): JSX.Element {
  const runtime = useLocalRuntime(model);
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ChatIdContext.Provider value={{ chatId }}>
        <ElementsContextSource bridge={bridge} sourceId={sourceId} />
      </ChatIdContext.Provider>
    </AssistantRuntimeProvider>
  );
}

function Consumer({ label }: { label: string }): JSX.Element {
  const aui = useAui();
  const text = useAuiState(({ composer }) => composer.text);
  const chat = useContext(ChatIdContext);
  return (
    <>
      <button onClick={() => aui.composer().setText(label)}>{label}</button>
      <output aria-label="composer">{text}</output>
      <output aria-label="chat">{chat?.chatId}</output>
    </>
  );
}

function Harness({
  sourceId,
  children,
}: {
  sourceId: string | null;
  children: (ready: boolean) => ReactNode;
}): JSX.Element {
  const [bridge] = useState(createElementsContextBridge);
  const published = usePublishedElementsSource(bridge);
  return (
    <>
      <ElementsContextTarget bridge={bridge}>
        {children(sourceId !== null && published === sourceId)}
      </ElementsContextTarget>
      {sourceId !== null && (
        <Source
          key={sourceId}
          bridge={bridge}
          sourceId={sourceId}
          chatId={`chat-${sourceId}`}
        />
      )}
    </>
  );
}

describe("ElementsContextBridge", () => {
  it("connects a sibling subtree to a live chat runtime", () => {
    render(
      <Harness sourceId="a">
        {(ready) => (ready ? <Consumer label="hello" /> : null)}
      </Harness>,
    );

    expect(screen.getByLabelText("chat").textContent).toBe("chat-a");
    act(() => screen.getByRole("button", { name: "hello" }).click());
    expect(screen.getByLabelText("composer").textContent).toBe("hello");
  });

  it("keeps the target's children mounted while the source comes and goes", () => {
    let mounts = 0;
    function Page(): null {
      useEffect(() => {
        mounts += 1;
      }, []);
      return null;
    }
    const tree = (sourceId: string | null) => (
      <Harness sourceId={sourceId}>
        {(ready) => (
          <>
            <Page />
            {ready && <Consumer label="hello" />}
          </>
        )}
      </Harness>
    );

    const { rerender } = render(tree(null));
    expect(screen.queryByRole("button")).toBeNull();

    rerender(tree("a"));
    expect(screen.getByLabelText("chat").textContent).toBe("chat-a");

    rerender(tree(null));
    expect(screen.queryByRole("button")).toBeNull();

    rerender(tree("b"));
    expect(screen.getByLabelText("chat").textContent).toBe("chat-b");
    expect(mounts).toBe(1);
  });

  it("bridges host contexts to sibling content and clears them when inactive", async () => {
    const bridge = createElementsContextBridge();
    const links = { resolveLink: () => ({ href: "/resolved" }) };
    const metadata = { "chat-shared": { readOnly: true } };
    const tools = { lookup: { execute: async () => "tool result" } };
    let observed: {
      connection: ContextType<typeof ConnectionStatusContext>;
      approval: ContextType<typeof ToolApprovalContext>;
      execution: ContextType<typeof ToolExecutionContext>;
    };
    function HostConsumer(): JSX.Element {
      const connection = useContext(ConnectionStatusContext);
      const approval = useContext(ToolApprovalContext);
      const execution = useContext(ToolExecutionContext);
      observed = { connection, approval, execution };
      const markdown = useContext(MarkdownLinkContext);
      const meta = useContext(ThreadMetaContext);
      return (
        <>
          <output aria-label="link">
            {markdown.resolveLink?.("/original")?.href}
          </output>
          <output aria-label="read only">
            {String(meta["chat-shared"]?.readOnly)}
          </output>
        </>
      );
    }
    const tree = (active: boolean) => (
      <>
        <ElementsContextTarget bridge={bridge} active={active}>
          <HostConsumer />
        </ElementsContextTarget>
        <ConnectionStatusProvider>
          <ToolApprovalProvider>
            <ToolExecutionProvider tools={tools}>
              <MarkdownLinkContext.Provider value={links}>
                <ThreadMetaContext.Provider value={metadata}>
                  <Source
                    bridge={bridge}
                    sourceId="shared"
                    chatId="chat-shared"
                  />
                </ThreadMetaContext.Provider>
              </MarkdownLinkContext.Provider>
            </ToolExecutionProvider>
          </ToolApprovalProvider>
        </ConnectionStatusProvider>
      </>
    );
    const { rerender } = render(tree(true));
    expect(screen.getByLabelText("link").textContent).toBe("/resolved");
    expect(screen.getByLabelText("read only").textContent).toBe("true");
    expect(observed!.connection?.state).toBe("connected");
    expect(observed!.execution?.isToolAvailable("lookup")).toBe(true);
    expect(await observed!.execution?.executeTool("lookup", {})).toEqual({
      success: true,
      result: "tool result",
    });
    act(() => observed!.approval?.whitelistTool("lookup"));
    expect(observed!.approval?.isToolApproved("lookup")).toBe(true);

    rerender(tree(false));
    expect(screen.getByLabelText("link").textContent).toBe("");
    expect(screen.getByLabelText("read only").textContent).toBe("undefined");
    expect(observed!).toEqual({
      connection: null,
      approval: null,
      execution: null,
    });
  });

  it("drops a still-published runtime synchronously when the target becomes inactive", async () => {
    const bridge = createElementsContextBridge();
    let mounts = 0;
    const adapter: RemoteThreadListAdapter = {
      list: async () => ({ threads: [] }),
      initialize: async (id) => ({ remoteId: id }),
      rename: async () => {},
      archive: async () => {},
      unarchive: async () => {},
      delete: async () => {},
      generateTitle: async () => {
        throw new Error("Unexpected title generation");
      },
      fetch: async (id) => ({ status: "regular", remoteId: id }),
    };
    function useThreadRuntime() {
      return useLocalRuntime(model);
    }
    function OwnRuntime(): JSX.Element {
      const runtime = useRemoteThreadListRuntime({
        adapter,
        runtimeHook: useThreadRuntime,
      });
      return (
        <AssistantRuntimeProvider runtime={runtime}>
          <Consumer label="own runtime" />
        </AssistantRuntimeProvider>
      );
    }
    function Page({ active }: { active: boolean }): JSX.Element {
      const chat = useContext(ChatIdContext);
      useEffect(() => {
        mounts += 1;
      }, []);
      return (
        <>
          <output aria-label="inherited chat">{chat?.chatId ?? "none"}</output>
          {!active && <OwnRuntime />}
        </>
      );
    }
    const tree = (active: boolean) => (
      <>
        <ElementsContextTarget bridge={bridge} active={active}>
          <Page active={active} />
        </ElementsContextTarget>
        <Source bridge={bridge} sourceId="shared" chatId="chat-shared" />
      </>
    );
    const { rerender } = render(tree(true));
    expect(screen.getByLabelText("inherited chat").textContent).toBe(
      "chat-shared",
    );
    expect(bridge.get()?.sourceId).toBe("shared");

    // Keep the source mounted: its layout-effect cleanup has not cleared it yet.
    await act(async () => rerender(tree(false)));

    expect(bridge.get()?.sourceId).toBe("shared");
    expect(screen.getByLabelText("inherited chat").textContent).toBe("none");
    act(() => screen.getByRole("button", { name: "own runtime" }).click());
    expect(screen.getByLabelText("composer").textContent).toBe("own runtime");
    expect(mounts).toBe(1);
  });

  it("binds consumers to a fresh runtime when the source restarts", () => {
    const tree = (sourceId: string) => (
      <Harness sourceId={sourceId}>
        {(ready) => (ready ? <Consumer label="hello" /> : null)}
      </Harness>
    );
    const { rerender } = render(tree("a"));
    act(() => screen.getByRole("button", { name: "hello" }).click());
    expect(screen.getByLabelText("composer").textContent).toBe("hello");

    rerender(tree("b"));

    expect(screen.getByLabelText("chat").textContent).toBe("chat-b");
    expect(screen.getByLabelText("composer").textContent).toBe("");
    act(() => screen.getByRole("button", { name: "hello" }).click());
    expect(screen.getByLabelText("composer").textContent).toBe("hello");
  });
});
