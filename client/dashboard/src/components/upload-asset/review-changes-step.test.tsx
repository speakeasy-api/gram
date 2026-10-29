import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import {
  cleanup,
  fireEvent,
  render as renderView,
  screen,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { OpenAPIv3Evaluation } from "@gram/client/models/components/openapiv3evaluation.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ReviewChangesStep } from "./review-changes-step";

const state = vi.hoisted(() => ({ evaluate: vi.fn() }));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    deployments: { evaluateOpenAPIv3: state.evaluate },
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    toolset: { href: (slug: string) => `/toolsets/${slug}` },
  }),
}));

const render = (children: ReactNode) => {
  const client = new QueryClient();
  return renderView(children, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <TooltipProvider>{children}</TooltipProvider>
        </MemoryRouter>
      </QueryClientProvider>
    ),
  });
};

const document = { name: "Todo API", slug: "todo" };

function evaluation(
  overrides: Partial<OpenAPIv3Evaluation> = {},
): OpenAPIv3Evaluation {
  return {
    slug: "todo",
    name: "Todo API",
    status: "changed",
    changes: [],
    breakingCount: 0,
    warningCount: 0,
    infoCount: 0,
    affectedToolsets: [],
    ...overrides,
  };
}

beforeEach(() => {
  state.evaluate.mockReset();
});
afterEach(cleanup);

it("evaluates the uploaded asset against the replaced document", async () => {
  state.evaluate.mockResolvedValue(evaluation({ status: "unchanged" }));
  render(
    <ReviewChangesStep
      existingDocument={document}
      assetId="asset"
      onContinue={vi.fn<() => void>()}
    />,
  );

  expect(
    await screen.findByText("No changes to tools, inputs or credentials."),
  ).toBeTruthy();
  expect(state.evaluate).toHaveBeenCalledWith({
    evaluateOpenAPIv3Form: { assetId: "asset", slug: "todo", name: "Todo API" },
  });
  expect(screen.getByText("DEPLOY NEW VERSION")).toBeTruthy();
});

it("leads with breaking changes and the servers they affect", async () => {
  state.evaluate.mockResolvedValue(
    evaluation({
      breakingCount: 1,
      infoCount: 1,
      changes: [
        {
          severity: "breaking",
          category: "tools",
          code: "tool_removed",
          title: "2 tools removed",
          details: [],
          tools: ["list_todos", "get_todo"],
        },
        {
          severity: "info",
          category: "tools",
          code: "tool_added",
          title: "1 tool added",
          details: [],
          tools: ["create_todo"],
        },
      ],
      affectedToolsets: [
        {
          id: "ts-1",
          name: "Todo server",
          slug: "todo-server",
          mcpEnabled: true,
          oauthConfigured: false,
          toolCount: 4,
          affectedToolCount: 2,
        },
        {
          id: "ts-2",
          name: "Untouched server",
          slug: "untouched",
          mcpEnabled: true,
          oauthConfigured: false,
          toolCount: 1,
          affectedToolCount: 0,
        },
      ],
    }),
  );
  const onContinue = vi.fn<() => void>();
  render(
    <ReviewChangesStep
      existingDocument={document}
      assetId="asset"
      onContinue={onContinue}
    />,
  );

  expect(
    await screen.findByText(
      "1 breaking change for servers that use this source.",
    ),
  ).toBeTruthy();
  expect(screen.getByText("2 tools removed")).toBeTruthy();
  expect(screen.getByText("Todo server")).toBeTruthy();
  expect(screen.queryByText("Untouched server")).toBeNull();
  // Informational changes stay folded until asked for.
  expect(screen.queryByText("1 tool added")).toBeNull();
  expect(screen.getByText("1 other change")).toBeTruthy();

  fireEvent.click(screen.getByText("DEPLOY ANYWAY"));
  expect(onContinue).toHaveBeenCalledOnce();
});

it("lets the user deploy without a review when the comparison fails", async () => {
  state.evaluate.mockRejectedValue(new Error("boom"));
  const onContinue = vi.fn<() => void>();
  render(
    <ReviewChangesStep
      existingDocument={document}
      assetId="asset"
      onContinue={onContinue}
    />,
  );

  expect(await screen.findByText("Try again")).toBeTruthy();
  fireEvent.click(screen.getByText("DEPLOY NEW VERSION"));
  expect(onContinue).toHaveBeenCalledOnce();
});
