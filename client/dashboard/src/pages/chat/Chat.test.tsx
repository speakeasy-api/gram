import {
  act,
  cleanup,
  fireEvent,
  render as renderWithProviders,
  screen,
} from "@testing-library/react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { useEffect, useRef, type ReactElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ChatLanding } from "./Chat";

const runtime = vi.hoisted(() => {
  const listeners = new Set<() => void>();
  const state = { ready: false, loading: true, bound: false, text: "" };
  const emit = () => {
    for (const listener of listeners) listener();
  };
  const setText = vi.fn((text: string) => {
    if (!state.bound) return;
    state.text = text;
    emit();
  });
  const newConversation = vi.fn();
  const aui = {
    composer: () => ({ setText, getState: () => ({ text: state.text }) }),
    threads: () => ({ getState: () => ({ isLoading: state.loading }) }),
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
  return { state, listeners, emit, setText, newConversation, aui };
});

vi.mock("@assistant-ui/react", () => ({
  useAui: () => runtime.aui,
  useAuiState: (select: (state: unknown) => unknown) =>
    select({ thread: { isRunning: false } }),
}));
vi.mock("@/components/insights-context", () => ({
  useInsightsState: () => ({
    assistantReady: runtime.state.ready,
    assistantNeedsAdmin: false,
    newConversation: runtime.newConversation,
    sendPrompt: vi.fn(),
  }),
  useHideInsightsDock: vi.fn(),
}));
// Only the Elements boundary is stubbed: the real landing, fresh-conversation
// hook, draft handover, and focus helper run together. Model the asynchronous
// shadow-root mount without needing a transport or an LLM.
vi.mock("@/elements", () => ({
  ChatComposer: () => {
    const host = useRef<HTMLDivElement>(null);
    useEffect(() => {
      const root = host.current!.attachShadow({ mode: "open" });
      const input = document.createElement("textarea");
      input.className = "aui-composer-input";
      input.value = runtime.state.text;
      input.addEventListener("input", () => {
        runtime.state.text = input.value;
        runtime.emit();
      });
      const frame = requestAnimationFrame(() => root.append(input));
      return () => cancelAnimationFrame(frame);
    }, []);
    return <div ref={host} className="gram-elements-composer-host" />;
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { displayName: "Test User" } }),
  useProject: () => ({ id: "project" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ projectSlug: "project" }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ chat: { conversation: { href: () => "/chat/new" } } }),
}));
vi.mock("react-router", async (original) => ({
  ...(await original<typeof import("react-router")>()),
  useNavigate: () => vi.fn(),
}));
vi.mock("@/hooks/useServerAssistantTransport", () => ({
  useServerAssistantTransport: () => ({ assistantId: "", ready: false }),
}));
vi.mock("@gram/client/react-query/listChats.js", () => ({
  useListChats: () => ({ data: undefined, isPending: false }),
}));
vi.mock("@/lib/chat-launch", () => ({ useChatLaunch: () => vi.fn() }));
vi.mock("@/components/require-scope", () => ({ RequireScope: () => null }));
vi.mock("@/components/command-palette/recentlyVisited", () => ({
  useRecentLabelOverride: vi.fn(),
}));

beforeEach(() => {
  runtime.state.ready = false;
  runtime.state.loading = true;
  runtime.state.bound = false;
  runtime.state.text = "";
  runtime.setText.mockClear();
  runtime.newConversation.mockClear();
});
afterEach(cleanup);

function render(element: ReactElement) {
  return renderWithProviders(element, { wrapper: TooltipProvider });
}

function composerInput(): HTMLTextAreaElement {
  return document
    .querySelector(".gram-elements-composer-host")!
    .shadowRoot!.querySelector("textarea")!;
}

async function finishMount() {
  await act(async () => {
    await new Promise((resolve) => {
      requestAnimationFrame(resolve);
    });
  });
}

describe("ChatLanding fallback handover", () => {
  it("focuses the newly mounted composer even while the thread list is loading", async () => {
    const view = render(<ChatLanding autoFocusInput />);
    expect(document.activeElement).toBe(screen.getByRole("combobox"));
    expect(runtime.newConversation).toHaveBeenCalledTimes(1);
    runtime.state.ready = true;
    view.rerender(<ChatLanding autoFocusInput />);
    await finishMount();
    const input = composerInput();
    expect(input.getRootNode()).toHaveProperty("activeElement", input);
    expect(runtime.state.loading).toBe(true);
  });

  it("retains an editable fallback until binding accepts the latest draft", async () => {
    const view = render(<ChatLanding autoFocusInput />);
    const fallback = screen.getByRole("combobox");
    fireEvent.change(fallback, { target: { value: "draft" } });
    runtime.state.ready = true;
    view.rerender(<ChatLanding autoFocusInput />);
    expect(screen.getByRole("combobox")).toBe(fallback);
    expect(document.querySelector(".gram-elements-composer-host")).toBeNull();
    fireEvent.change(fallback, { target: { value: "draft plus new typing" } });
    act(() => {
      runtime.state.loading = false;
      runtime.emit();
    });
    expect(screen.getByRole("combobox")).toBe(fallback);
    act(() => {
      runtime.state.bound = true;
      runtime.emit();
    });
    await finishMount();
    const input = composerInput();
    expect(input.value).toBe("draft plus new typing");
    expect(input.getRootNode()).toHaveProperty("activeElement", input);
    input.value += " in composer";
    fireEvent.input(input);
    view.rerender(<ChatLanding autoFocusInput />);
    expect(runtime.state.text).toBe("draft plus new typing in composer");
  });

  it("does not reclaim focus moved elsewhere while the shadow input mounts", async () => {
    const view = render(
      <>
        <button>Elsewhere</button>
        <ChatLanding autoFocusInput />
      </>,
    );
    runtime.state.ready = true;
    view.rerender(
      <>
        <button>Elsewhere</button>
        <ChatLanding autoFocusInput />
      </>,
    );
    const elsewhere = screen.getByRole("button", { name: "Elsewhere" });
    elsewhere.focus();
    await finishMount();
    expect(composerInput()).not.toBeNull();
    expect(document.activeElement).toBe(elsewhere);
  });

  it("does not steal focus moved away from the fallback before readiness", async () => {
    const view = render(
      <>
        <button>Elsewhere</button>
        <ChatLanding autoFocusInput />
      </>,
    );
    fireEvent.change(screen.getByRole("combobox"), {
      target: { value: "draft" },
    });
    const elsewhere = screen.getByRole("button", { name: "Elsewhere" });
    elsewhere.focus();
    runtime.state.ready = true;
    view.rerender(
      <>
        <button>Elsewhere</button>
        <ChatLanding autoFocusInput />
      </>,
    );
    act(() => {
      runtime.state.loading = false;
      runtime.state.bound = true;
      runtime.emit();
    });
    await finishMount();
    expect(composerInput().value).toBe("draft");
    expect(document.activeElement).toBe(elsewhere);
  });
});
