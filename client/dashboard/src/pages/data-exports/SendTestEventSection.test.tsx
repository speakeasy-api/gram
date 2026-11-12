import { createQueryClient } from "@/contexts/Sdk";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { QueryClientProvider, useMutation } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SendTestEventSection } from "./SendTestEventSection";

vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

type SignalResult = {
  signal: string;
  outcome: string;
  statusCode?: number;
  message?: string;
};

const mutate = vi.fn();
const mutation = {
  mutate,
  data: undefined as
    | { outcome: string; message: string; signals: SignalResult[] }
    | undefined,
  error: undefined as unknown,
  isPending: false,
  isError: false,
};

// When set, the hook is a real mutation that fails with this status.
const failWithStatus = { value: undefined as number | undefined };

vi.mock("@gram/client/react-query/testDataExportRoute.js", () => ({
  useTestDataExportRouteMutation: (options?: object) => {
    const status = failWithStatus.value;
    if (status === undefined) return mutation;
    // eslint-disable-next-line react-hooks/rules-of-hooks -- fixed per test
    return useMutation({
      mutationFn: () =>
        Promise.reject(
          new GramError("request failed", {
            response: new Response('{"name":"rate_limit_exceeded"}', {
              status,
            }),
            request: new Request("https://app.getgram.ai/rpc/example"),
            body: '{"name":"rate_limit_exceeded"}',
          }),
        ),
      ...options,
    });
  },
}));

const hasScope = { value: true };
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) =>
    hasScope.value ? children : null,
}));

beforeEach(() => {
  mutation.data = undefined;
  mutation.error = undefined;
  mutation.isPending = false;
  mutation.isError = false;
  failWithStatus.value = undefined;
  hasScope.value = true;
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderSection(unsavedChanges = false, sendsEverySignal = false) {
  render(
    <SendTestEventSection
      projectSlug="default"
      routeId="route-1"
      sendsEverySignal={sendsEverySignal}
      unsavedChanges={unsavedChanges}
    />,
  );
}

function logsResult(outcome: string, message: string) {
  return {
    outcome,
    message,
    signals: [{ signal: "logs", outcome, message }],
  };
}

describe("SendTestEventSection", () => {
  it("sends a test event for the saved route", () => {
    renderSection();

    fireEvent.click(screen.getByRole("button", { name: "Send test event" }));

    expect(mutate).toHaveBeenCalledWith({
      request: { id: "route-1", gramProject: "default" },
    });
  });

  it("says how to filter the test event out", () => {
    renderSection();

    expect(screen.getByText("speakeasy.test_event=true")).toBeTruthy();
    expect(screen.getByText(/Send one test event to check/)).toBeTruthy();
  });

  it("says product telemetry sends a log, a trace and a metric", () => {
    renderSection(false, true);

    expect(
      screen.getByText(/Send one test log, trace and metric to check/),
    ).toBeTruthy();
    expect(screen.getByText("speakeasy.test_event=true")).toBeTruthy();
  });

  it("asks for a save before testing changed settings", () => {
    renderSection(true);

    expect(
      screen.getByRole<HTMLButtonElement>("button", {
        name: "Send test event",
      }).disabled,
    ).toBe(true);
    expect(
      screen.getByText(
        "Save your changes to send a test event to this destination.",
      ),
    ).toBeTruthy();
  });

  it.each([
    ["accepted", "Test event accepted"],
    ["refused", "Test event refused"],
    ["not_found", "Logs endpoint not found"],
    ["unreachable", "Destination not reachable"],
    ["error", "Destination returned an error"],
  ])("shows the %s answer inline", (outcome, title) => {
    mutation.data = logsResult(outcome, `Message for ${outcome}.`);
    renderSection();

    expect(screen.getByText(title)).toBeTruthy();
    expect(screen.getByText(`Message for ${outcome}.`)).toBeTruthy();
    expect(screen.queryByRole("listitem")).toBeNull();
  });

  it("lists every signal when all were accepted", () => {
    mutation.data = {
      outcome: "accepted",
      message:
        "The destination accepted the test events for logs, traces and metrics.",
      signals: ["logs", "traces", "metrics"].map((signal) => ({
        signal,
        outcome: "accepted",
        statusCode: 200,
        message: "The destination accepted the test event.",
      })),
    };
    renderSection(false, true);

    expect(screen.getByText("Test event accepted")).toBeTruthy();
    const items = screen.getAllByRole("listitem");
    expect(items.map((item) => item.textContent)).toEqual([
      "Logs: Test event accepted. The destination accepted the test event.",
      "Traces: Test event accepted. The destination accepted the test event.",
      "Metrics: Test event accepted. The destination accepted the test event.",
    ]);
  });

  it("shows which signal failed when only some were accepted", () => {
    mutation.data = {
      outcome: "not_found",
      message:
        "The destination did not accept every test event. See each signal for details.",
      signals: [
        {
          signal: "logs",
          outcome: "accepted",
          statusCode: 200,
          message: "The destination accepted the test event.",
        },
        {
          signal: "traces",
          outcome: "not_found",
          statusCode: 404,
          message:
            "No traces endpoint at this address. Check the URL; Speakeasy adds /v1/traces.",
        },
      ],
    };
    renderSection(false, true);

    expect(screen.getByText("Not every test event was accepted")).toBeTruthy();
    expect(
      screen.getByText(
        "The destination did not accept every test event. See each signal for details.",
      ),
    ).toBeTruthy();
    expect(
      screen.getAllByRole("listitem").map((item) => item.textContent),
    ).toEqual([
      "Logs: Test event accepted. The destination accepted the test event.",
      "Traces: Endpoint not found. No traces endpoint at this address. Check the URL; Speakeasy adds /v1/traces.",
    ]);
  });

  it("says the result is unknown when the answer was lost", () => {
    mutation.isError = true;
    mutation.error = new Error("boom");
    renderSection();

    expect(screen.getByText("Test result unknown")).toBeTruthy();
    expect(
      screen.getByText(
        "Speakeasy could not confirm the result. The test event may have been sent, so check your destination before trying again.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("boom")).toBeNull();
  });

  it("says the event was not sent when the request was refused", () => {
    mutation.isError = true;
    mutation.error = new GramError("request failed", {
      response: new Response("{}", { status: 404 }),
      request: new Request("https://app.getgram.ai/rpc/example"),
      body: "{}",
    });
    renderSection();

    expect(screen.getByText("Test event not sent")).toBeTruthy();
    expect(
      screen.getByText("Speakeasy could not send the test event. Try again."),
    ).toBeTruthy();
  });

  it("hides an earlier answer once the settings change", () => {
    mutation.data = logsResult("accepted", "Accepted.");
    renderSection(true);

    expect(screen.queryByText("Test event accepted")).toBeNull();
    expect(screen.queryByText("Accepted.")).toBeNull();
  });

  it.each([
    [429, "Too many test events were sent just now. Try again in a minute."],
    [503, "Speakeasy can't send test events right now. Try again shortly."],
  ])(
    "shows a %i inline without the app-wide error toast",
    async (status, message) => {
      failWithStatus.value = status;
      render(
        <QueryClientProvider client={createQueryClient()}>
          <SendTestEventSection
            projectSlug="default"
            routeId="route-1"
            sendsEverySignal={false}
            unsavedChanges={false}
          />
        </QueryClientProvider>,
      );

      fireEvent.click(screen.getByRole("button", { name: "Send test event" }));

      await waitFor(() => expect(screen.getByText(message)).toBeTruthy());
      expect(toast.error).not.toHaveBeenCalled();
    },
  );

  it("is hidden from people who are not org admins", () => {
    hasScope.value = false;
    renderSection();

    expect(
      screen.queryByRole("button", { name: "Send test event" }),
    ).toBeNull();
  });
});
