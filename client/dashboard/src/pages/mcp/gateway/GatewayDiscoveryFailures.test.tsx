import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { GatewayDiscoveryFailures } from "./GatewayDiscoveryFailures";

const state = vi.hoisted(() => ({ read: true, search: vi.fn() }));
vi.mock("@/components/observe/observeDeepLink", () => ({
  useCanViewToolLogs: () => state.read,
}));
vi.mock("@gram/client/react-query/_context", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/funcs/telemetrySearchLogs", () => ({
  telemetrySearchLogs: state.search,
}));

const from = new Date("2026-01-01T00:00:00Z");
const to = new Date("2026-01-02T00:00:00Z");
const message =
  'server "example" was rate limited and was left out of the tool listing';
function result(attributes: unknown) {
  return {
    ok: true,
    value: {
      logs: [
        { id: "failure", attributes, timeUnixNano: "1767225600000000000" },
      ],
    },
  };
}
function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <GatewayDiscoveryFailures
        metaMcpServerId="gateway-test"
        from={from}
        to={to}
      />
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  state.read = true;
  state.search.mockReset();
});
afterEach(cleanup);

it.each([{ "error.message": message }, { error: { message } }])(
  "loads scoped diagnostics on demand and renders stored attributes",
  async (attributes) => {
    state.search.mockResolvedValue(result(attributes));
    setup();
    expect(state.search).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Discovery diagnostics" }),
    );
    expect(await screen.findByText(message)).toBeTruthy();
    expect(state.search).toHaveBeenCalledWith(
      {},
      {
        searchLogsPayload: {
          from,
          to,
          limit: 20,
          sort: "desc",
          filter: {
            eventSource: "meta_discovery",
            gramUrn: "metamcp:gateway-test:tools/list",
          },
        },
      },
    );
  },
);

it("does not fetch or expose diagnostics without log permission", () => {
  state.read = false;
  setup();
  expect(
    screen.queryByRole("button", { name: "Discovery diagnostics" }),
  ).toBeNull();
  expect(state.search).not.toHaveBeenCalled();
});

it("allows retry after a failed read and distinguishes an empty result", async () => {
  state.search.mockRejectedValueOnce(new Error("private error detail"));
  state.search.mockResolvedValue({ ok: true, value: { logs: [] } });
  setup();
  fireEvent.click(
    screen.getByRole("button", { name: "Discovery diagnostics" }),
  );
  expect(
    await screen.findByText("Could not load discovery diagnostics"),
  ).toBeTruthy();
  expect(screen.queryByText("private error detail")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await waitFor(() =>
    expect(
      screen.getByText("No omitted servers were recorded in this time range."),
    ).toBeTruthy(),
  );
});
