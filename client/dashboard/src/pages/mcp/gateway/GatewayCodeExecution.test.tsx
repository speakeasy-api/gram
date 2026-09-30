import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { GatewayCodeExecution } from "./GatewayCodeExecution";

vi.mock("@/components/ui/CodeSnippet", () => ({
  CodeSnippet: ({ code }: { code: string }) => <pre>{code}</pre>,
}));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
function show() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <GatewayCodeExecution
        connectUrl="https://example.com/gateway"
        headers={{ Authorization: "Bearer test-only" }}
      />
    </QueryClientProvider>,
  );
}
const response = () =>
  new Response(
    JSON.stringify({
      jsonrpc: "2.0",
      id: "response",
      result: {
        structuredContent: {
          value: { total: 42 },
          output: "calculated\n",
          output_truncated: false,
          tool_calls: [{ path: "support--list_tickets", outcome: "completed" }],
        },
      },
    }),
    { headers: { "content-type": "application/json" } },
  );
it("runs only on submission, preserves submitted code and results, and never reuses request IDs", async () => {
  const fetch = vi.fn().mockImplementation(async () => response());
  vi.stubGlobal("fetch", fetch);
  show();
  expect(fetch).not.toHaveBeenCalled();
  const source =
    'result = await tools.call("support--list_tickets", {})\nprint("calculated")\nresult';
  fireEvent.change(screen.getByLabelText("Python source"), {
    target: { value: source },
  });
  fireEvent.click(screen.getByRole("button", { name: "Execute Python" }));
  expect(
    await screen.findByText("support--list_tickets: completed"),
  ).toBeTruthy();
  expect(screen.getByText('"total": 42', { exact: false })).toBeTruthy();
  expect(screen.getByText("calculated")).toBeTruthy();
  const first = JSON.parse(fetch.mock.calls[0]?.[1].body);
  expect(first.params).toEqual({
    name: "execute",
    arguments: { code: source },
  });
  expect(fetch.mock.calls[0]?.[1].headers.Authorization).toBe(
    "Bearer test-only",
  );
  fireEvent.change(screen.getByLabelText("Python source"), {
    target: { value: "1 + 1" },
  });
  expect(
    screen.getByText(
      (_, element) =>
        element?.tagName === "PRE" && element.textContent === source,
    ),
  ).toBeTruthy();
  expect(fetch).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "Execute Python" }));
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  expect(JSON.parse(fetch.mock.calls[1]?.[1].body).id).not.toBe(first.id);
});
it("does not retry a failed execution with potentially completed writes", async () => {
  const fetch = vi.fn().mockRejectedValue(new Error("Connection lost"));
  vi.stubGlobal("fetch", fetch);
  show();
  fireEvent.click(screen.getByRole("button", { name: "Execute Python" }));
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Connection lost",
  );
  expect(fetch).toHaveBeenCalledTimes(1);
});
it("cancels the request and explains uncertain tool outcomes", async () => {
  const fetch = vi.fn(
    (_url, options) =>
      new Promise((_resolve, reject) => {
        options.signal.addEventListener("abort", () =>
          reject(new DOMException("Aborted", "AbortError")),
        );
      }),
  );
  vi.stubGlobal("fetch", fetch);
  show();
  fireEvent.click(screen.getByRole("button", { name: "Execute Python" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Cancel execution" }),
  );
  expect((await screen.findByRole("alert")).textContent).toContain(
    "may have completed",
  );
  expect(fetch.mock.calls[0]?.[1].signal.aborted).toBe(true);
  expect(fetch).toHaveBeenCalledTimes(2);
  const original = JSON.parse(fetch.mock.calls[0]?.[1].body);
  const cancel = JSON.parse(fetch.mock.calls[1]?.[1].body);
  expect(cancel).toEqual({
    jsonrpc: "2.0",
    method: "notifications/cancelled",
    params: { requestId: original.id },
  });
  expect(fetch.mock.calls[1]?.[1].headers.Authorization).toBe(
    "Bearer test-only",
  );
});
it("aborts on unmount so a reconnected inspector cannot leave code running", async () => {
  const fetch = vi.fn(
    (_url, options) =>
      new Promise((_resolve, reject) => {
        options.signal.addEventListener("abort", () =>
          reject(new DOMException("Aborted", "AbortError")),
        );
      }),
  );
  vi.stubGlobal("fetch", fetch);
  const { unmount } = show();
  fireEvent.click(screen.getByRole("button", { name: "Execute Python" }));
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
  unmount();
  expect(fetch.mock.calls[0]?.[1].signal.aborted).toBe(true);
  expect(fetch).toHaveBeenCalledTimes(2);
  const original = JSON.parse(fetch.mock.calls[0]?.[1].body);
  const cancel = JSON.parse(fetch.mock.calls[1]?.[1].body);
  expect(cancel).toEqual({
    jsonrpc: "2.0",
    method: "notifications/cancelled",
    params: { requestId: original.id },
  });
  expect(fetch.mock.calls[1]?.[1].headers.Authorization).toBe(
    "Bearer test-only",
  );
});
it("shows an admission refusal without claiming tools might have run", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          result: {
            isError: true,
            content: [{ type: "text", text: "No program was submitted." }],
          },
        }),
        { headers: { "content-type": "application/json" } },
      ),
    ),
  );
  show();
  fireEvent.click(screen.getByRole("button", { name: "Execute Python" }));
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toBe("No program was submitted.");
});
it("cancels with the submitted credential after headers refresh", async () => {
  const fetch = vi.fn(
    (_url, options) =>
      new Promise((_resolve, reject) => {
        options.signal.addEventListener("abort", () =>
          reject(new DOMException("Aborted", "AbortError")),
        );
      }),
  );
  vi.stubGlobal("fetch", fetch);
  const client = new QueryClient();
  const view = (token: string) => (
    <QueryClientProvider client={client}>
      <GatewayCodeExecution
        connectUrl="https://example.com/gateway"
        headers={{ Authorization: `Bearer ${token}` }}
      />
    </QueryClientProvider>
  );
  const { rerender } = render(view("original"));
  fireEvent.click(screen.getByRole("button", { name: "Execute Python" }));
  await screen.findByRole("button", { name: "Cancel execution" });
  rerender(view("refreshed"));
  fireEvent.click(screen.getByRole("button", { name: "Cancel execution" }));
  await screen.findByRole("alert");
  expect(fetch.mock.calls[1]?.[1].headers.Authorization).toBe(
    "Bearer original",
  );
});
