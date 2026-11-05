import {
  AssistantRuntimeProvider,
  type ChatModelAdapter,
  useAui,
  useAuiState,
  useLocalRuntime,
} from "@assistant-ui/react";
import { act, cleanup, render, screen } from "@testing-library/react";
import { type ReactNode, useContext, useEffect, useState } from "react";
import { afterEach, describe, expect, it } from "vitest";
import {
  createElementsContextBridge,
  ElementsContextSource,
  ElementsContextTarget,
  usePublishedElementsSource,
} from "./ElementsContextBridge";
import { ChatIdContext } from "./ChatIdContext";

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
