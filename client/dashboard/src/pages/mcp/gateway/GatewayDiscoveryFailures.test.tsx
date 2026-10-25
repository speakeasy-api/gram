import type { ComponentProps } from "react";
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
import { discoveryChartData } from "./gatewayDiscoveryChart";

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
vi.mock("@/components/chart/StackedTimeBarChart", () => ({
  StackedTimeBarChart: ({
    onBucketHover,
    tooltipAfterBody,
  }: {
    onBucketHover: (index: number | null) => void;
    tooltipAfterBody: (index: number) => string[];
  }) => (
    <div>
      <button
        onMouseEnter={() => onBucketHover(0)}
        onMouseLeave={() => onBucketHover(null)}
      >
        First interval
      </button>
      <button onMouseEnter={() => onBucketHover(1)}>Second interval</button>
      <div>{tooltipAfterBody(0).join(" ")}</div>
      <div>{tooltipAfterBody(1).join(" ")}</div>
    </div>
  ),
}));
const from = new Date("2026-01-01T00:00:00Z");
const to = new Date("2026-01-01T02:00:00Z");
const failures = {
  intervalSeconds: 3600,
  buckets: [
    { bucketTimeUnixNano: "1767225600000000000", count: 25 },
    { bucketTimeUnixNano: "1767229200000000000", count: 1 },
  ],
};
const message = 'server "example" was rate limited';
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
function setup(
  overrides: Partial<ComponentProps<typeof GatewayDiscoveryFailures>> = {},
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const content = (
    props: Partial<ComponentProps<typeof GatewayDiscoveryFailures>>,
  ) => (
    <QueryClientProvider client={client}>
      <GatewayDiscoveryFailures
        metaMcpServerId="gateway-test"
        from={from}
        to={to}
        failures={failures}
        loading={false}
        error={false}
        expandedChart={null}
        onExpand={() => {}}
        onRangeSelect={() => {}}
        isZoomed={false}
        onResetZoom={() => {}}
        {...props}
      />
    </QueryClientProvider>
  );
  const view = render(content(overrides));
  return {
    ...view,
    update: (props: Partial<ComponentProps<typeof GatewayDiscoveryFailures>>) =>
      view.rerender(content(props)),
  };
}
beforeEach(() => {
  state.read = true;
  state.search.mockReset();
});
afterEach(cleanup);

it.each([{ "error.message": message }, { error: { message } }])(
  "loads only the hovered interval and renders its events",
  async (attributes) => {
    state.search.mockResolvedValue(result(attributes));
    setup();
    expect(state.search).not.toHaveBeenCalled();
    fireEvent.mouseEnter(screen.getByText("First interval"));
    expect(await screen.findByText(new RegExp(message))).toBeTruthy();
    expect(screen.getByText(/Latest event/)).toBeTruthy();
    expect(state.search).toHaveBeenCalledWith(
      {},
      {
        searchLogsPayload: {
          from,
          to: new Date("2026-01-01T00:59:59.999Z"),
          limit: 1,
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
it("starts an aligned first interval's details at the selected range", async () => {
  state.search.mockResolvedValue(result({ "error.message": message }));
  const rangeFrom = new Date("2026-01-01T00:20:00Z");
  const rangeTo = new Date("2026-01-01T01:40:00Z");
  setup({ from: rangeFrom, to: rangeTo });
  fireEvent.mouseEnter(screen.getByText("First interval"));
  await screen.findByText(new RegExp(message));
  expect(state.search).toHaveBeenLastCalledWith(
    {},
    expect.objectContaining({
      searchLogsPayload: expect.objectContaining({
        from: rangeFrom,
        to: new Date("2026-01-01T00:59:59.999Z"),
      }),
    }),
  );
  fireEvent.mouseEnter(screen.getByText("Second interval"));
  await waitFor(() =>
    expect(state.search).toHaveBeenLastCalledWith(
      {},
      expect.objectContaining({
        searchLogsPayload: expect.objectContaining({
          from: new Date("2026-01-01T01:00:00Z"),
          to: new Date("2026-01-01T01:39:59.999Z"),
        }),
      }),
    ),
  );
});
it("does not fetch or expose diagnostics without log permission", () => {
  state.read = false;
  setup();
  expect(screen.queryByText("Discovery failures over time")).toBeNull();
  expect(state.search).not.toHaveBeenCalled();
});
it("does not show the previous interval's events while another interval loads", async () => {
  state.search.mockResolvedValueOnce(result({ "error.message": message }));
  state.search.mockImplementationOnce(() => new Promise(() => {}));
  setup();
  fireEvent.mouseEnter(screen.getByText("First interval"));
  await screen.findByText(new RegExp(message));
  fireEvent.mouseEnter(screen.getByText("Second interval"));
  await waitFor(() =>
    expect(screen.queryByText(new RegExp(message))).toBeNull(),
  );
  expect(screen.getByText("Loading event details…")).toBeTruthy();
});
it("shows a safe detail error and retries when the interval is hovered again", async () => {
  state.search.mockRejectedValueOnce(new Error("private error detail"));
  state.search.mockResolvedValue(result({ "error.message": message }));
  setup();
  fireEvent.mouseEnter(screen.getByText("First interval"));
  await screen.findByText(/Could not load event details/);
  expect(screen.queryByText(/private error detail/)).toBeNull();
  fireEvent.mouseLeave(screen.getByText("First interval"));
  fireEvent.mouseEnter(screen.getByText("First interval"));
  expect(await screen.findByText(new RegExp(message))).toBeTruthy();
});
it("plots aggregate counts beyond the detail limit and retains empty intervals", () => {
  const data = discoveryChartData(
    {
      ...failures,
      buckets: [
        ...failures.buckets,
        { bucketTimeUnixNano: "1767232800000000000", count: 0 },
      ],
    },
    from,
    new Date("2026-01-01T03:00:00Z"),
  );
  expect(data.counts).toEqual([25, 1, 0]);
  expect(data.timestamps).toHaveLength(3);
});

it("shows an empty chart without fetching event details", () => {
  setup({ failures: { intervalSeconds: 3600, buckets: [] } });
  expect(
    screen.getByText("No omitted servers were recorded in this time range"),
  ).toBeTruthy();
  expect(state.search).not.toHaveBeenCalled();
});
it("distinguishes an aggregate error from an empty range", () => {
  setup({ error: true, failures: undefined });
  expect(screen.getByRole("alert")).toBeTruthy();
  expect(
    screen.queryByText("No omitted servers were recorded in this time range"),
  ).toBeNull();
  expect(
    screen.queryByText(
      "Discovery failures are not available from this server yet",
    ),
  ).toBeNull();
  expect(state.search).not.toHaveBeenCalled();
});
it("reports a server without the series as unavailable, not as no omissions", () => {
  setup({ failures: undefined });
  expect(
    screen.getByText(
      "Discovery failures are not available from this server yet",
    ),
  ).toBeTruthy();
  expect(
    screen.queryByText("No omitted servers were recorded in this time range"),
  ).toBeNull();
  expect(screen.queryByText("First interval")).toBeNull();
  expect(state.search).not.toHaveBeenCalled();
});
it("shows neither empty message while the series loads", () => {
  setup({ loading: true, failures: undefined });
  expect(
    screen.queryByText(
      "Discovery failures are not available from this server yet",
    ),
  ).toBeNull();
  expect(
    screen.queryByText("No omitted servers were recorded in this time range"),
  ).toBeNull();
  expect(state.search).not.toHaveBeenCalled();
});

it("does not fetch stale buckets while a new range loads", async () => {
  state.search.mockResolvedValue(result({ "error.message": message }));
  const view = setup();
  fireEvent.mouseEnter(screen.getByText("First interval"));
  await screen.findByText(new RegExp(message));
  view.update({
    from: new Date("2026-01-02T00:00:00Z"),
    to: new Date("2026-01-03T00:00:00Z"),
    loading: true,
    failures,
  });
  expect(screen.queryByText("First interval")).toBeNull();
  expect(screen.queryByText(new RegExp(message))).toBeNull();
  expect(state.search).toHaveBeenCalledTimes(1);
});
it("excludes the selected end from a partial bucket's detail read", async () => {
  state.search.mockResolvedValue(result({ "error.message": message }));
  setup({ to: new Date("2026-01-01T00:30:00Z") });
  fireEvent.mouseEnter(screen.getByText("First interval"));
  await screen.findByText(new RegExp(message));
  expect(state.search.mock.calls[0]?.[1].searchLogsPayload.to).toEqual(
    new Date("2026-01-01T00:29:59.999Z"),
  );
});
