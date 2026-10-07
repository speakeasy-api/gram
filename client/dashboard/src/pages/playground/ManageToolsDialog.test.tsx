import { CommandPaletteProvider } from "@/contexts/CommandPaletteProvider";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { Tool, Toolset } from "@/lib/toolTypes";
import { PromptTemplateKind } from "@gram/client/models/components/prompttemplate.js";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import type { ToolUpdatePayload } from "./EditToolDialog";
import { ManageToolsDialog } from "./ManageToolsDialog";

const testState = vi.hoisted(() => ({
  listTools: {
    data: undefined as { tools: Tool[] } | undefined,
    isLoading: false,
  },
}));

vi.mock("@/hooks/toolTypes", () => ({
  useListTools: () => testState.listTools,
  useLatestDeployment: () => ({ data: undefined }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

const promptTool = {
  type: "prompt",
  canonicalName: "weather_lookup",
  createdAt: new Date("2026-01-01T00:00:00Z"),
  description: "Look up the weather",
  engine: "mustache",
  historyId: "history_id",
  id: "tool_id",
  kind: PromptTemplateKind.Prompt,
  name: "weather_lookup",
  projectId: "project_id",
  prompt: "Weather for {{city}}",
  schema: JSON.stringify({
    type: "object",
    properties: { city: { type: "string" } },
  }),
  toolUrn: "tools:weather_lookup",
  toolsHint: [],
  updatedAt: new Date("2026-01-01T00:00:00Z"),
} satisfies Tool;

const toolset = {
  accountType: "free",
  createdAt: new Date("2026-01-01T00:00:00Z"),
  id: "toolset_id",
  name: "Weather tools",
  oauthEnablementMetadata: { oauth2SecurityCount: 0 },
  organizationId: "organization_id",
  projectId: "project_id",
  promptTemplates: [],
  rawTools: [],
  resourceUrns: [],
  resources: [],
  slug: "weather-tools",
  toolSelectionMode: "manual",
  toolUrns: [],
  tools: [],
  toolsetVersion: 1,
  updatedAt: new Date("2026-01-01T00:00:00Z"),
} satisfies Toolset;

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T | PromiseLike<T>) => void;
  reject: (reason?: unknown) => void;
};

function deferred<T>(): Deferred<T> {
  let resolve!: Deferred<T>["resolve"];
  let reject!: Deferred<T>["reject"];
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function renderDialog({
  currentTools = [],
  initialGroup,
  onToolUpdate,
  onRemoveTools = vi.fn(),
}: {
  currentTools?: Tool[];
  initialGroup?: string;
  onToolUpdate: (tool: Tool, updates: ToolUpdatePayload) => Promise<void>;
  onRemoveTools?: (toolUrns: string[]) => void;
}) {
  const props = {
    open: true,
    onOpenChange: vi.fn(),
    toolset: { ...toolset, tools: currentTools },
    currentTools,
    onAddTools: vi.fn(),
    onRemoveTools,
    initialGroup,
    onToolUpdate,
  };

  render(
    <TooltipProvider>
      <CommandPaletteProvider>
        <ManageToolsDialog {...props} />
      </CommandPaletteProvider>
    </TooltipProvider>,
  );

  return { onRemoveTools };
}

beforeEach(() => {
  testState.listTools.data = { tools: [promptTool] };
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
});

afterEach(cleanup);

describe("ManageToolsDialog tool editing", () => {
  it("waits for the supplied update before reporting success and closing", async () => {
    const update = deferred<void>();
    const onToolUpdate = vi.fn(() => update.promise);

    renderDialog({ onToolUpdate });
    fireEvent.click(screen.getByText("weather_lookup"));
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Updated description" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Save/ }));

    expect(onToolUpdate).toHaveBeenCalledWith(promptTool, {
      name: "weather_lookup",
      description: "Updated description",
    });
    expect(toast.success).not.toHaveBeenCalled();

    update.resolve();

    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith("Tool updated");
    });
    expect(screen.queryByLabelText("Description")).toBeNull();
  });

  it("reports a rejected update and keeps the editor open", async () => {
    const onToolUpdate = vi.fn().mockRejectedValue(new Error("request failed"));

    renderDialog({ onToolUpdate });
    fireEvent.click(screen.getByText("weather_lookup"));
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Updated description" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Save/ }));

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith("Failed to update tool");
    });
    expect(screen.getByLabelText("Description")).toBeTruthy();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("submits only once when the save shortcut repeats while pending", () => {
    const update = deferred<void>();
    const onToolUpdate = vi.fn(() => update.promise);

    renderDialog({ onToolUpdate });
    fireEvent.click(screen.getByText("weather_lookup"));
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Updated description" },
    });

    fireEvent.keyDown(window, { key: "Enter", metaKey: true });
    fireEvent.keyDown(window, { key: "Enter", metaKey: true });

    expect(onToolUpdate).toHaveBeenCalledTimes(1);
  });

  it("keeps the active editor open while its save is pending", async () => {
    const update = deferred<void>();
    const onToolUpdate = vi.fn(() => update.promise);

    renderDialog({ onToolUpdate });
    fireEvent.click(screen.getByText("weather_lookup"));
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Updated description" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Save/ }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.getByLabelText("Description")).toBeTruthy();

    update.resolve();
    await waitFor(() => {
      expect(screen.queryByLabelText("Description")).toBeNull();
    });
  });

  it("hides Remove while editing a tool that is not in the toolset", () => {
    renderDialog({ currentTools: [], onToolUpdate: vi.fn() });
    fireEvent.click(screen.getByText("weather_lookup"));

    expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
  });

  it("offers Remove while editing a tool already in the toolset", () => {
    const { onRemoveTools } = renderDialog({
      currentTools: [promptTool],
      initialGroup: "Prompts",
      onToolUpdate: vi.fn(),
    });
    fireEvent.click(screen.getByText("weather_lookup"));
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));

    expect(onRemoveTools).toHaveBeenCalledWith(["tools:weather_lookup"]);
    expect(screen.queryByLabelText("Description")).toBeNull();
    expect(toast.success).not.toHaveBeenCalled();
  });
});
