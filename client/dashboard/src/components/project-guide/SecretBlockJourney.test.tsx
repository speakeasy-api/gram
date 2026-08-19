import {
  cleanup,
  fireEvent,
  render as baseRender,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConfigProvider } from "@/components/ui/context/ConfigContext";

const queries = vi.hoisted(() => ({
  policies: vi.fn(),
  traces: vi.fn(),
}));
const mutation = vi.hoisted(() => ({ mutateAsync: vi.fn() }));
const fetcher = vi.hoisted(() => ({ fetch: vi.fn() }));
const downloads = vi.hoisted(() => vi.fn());
const revokeObjectURL = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-guide-test",
}));

vi.mock("@/contexts/Fetcher", () => ({ useFetcher: () => fetcher }));

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: queries.policies,
}));

vi.mock("@gram/client/react-query/riskCreatePolicy.js", () => ({
  useRiskCreatePolicyMutation: () => mutation,
}));

vi.mock("@gram/client/react-query/listHooksTraces.js", () => ({
  useListHooksTraces: queries.traces,
}));

import { SecretBlockJourney } from "./SecretBlockJourney";

function render(ui: React.ReactNode) {
  return baseRender(ui, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={new QueryClient()}>
        <ConfigProvider theme="light" setTheme={() => {}}>
          {children}
        </ConfigProvider>
      </QueryClientProvider>
    ),
  });
}

function blockingPolicy() {
  return {
    action: "block",
    audiencePrincipalUrns: ["user:all"],
    audienceType: "everyone",
    autoName: true,
    createdAt: new Date(),
    enabled: true,
    id: "policy-test",
    name: "Secrets policy",
    policyType: "standard",
    projectId: "project-test",
    score: 5,
    sources: ["gitleaks"],
    updatedAt: new Date(),
    version: 1,
  };
}

beforeEach(() => {
  mutation.mutateAsync.mockReset().mockResolvedValue(blockingPolicy());
  fetcher.fetch.mockReset().mockResolvedValue(new Response("zip"));
  downloads.mockReset();
  revokeObjectURL.mockReset();
  queries.policies.mockReset().mockReturnValue({
    data: { policies: [] },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  });
  queries.traces.mockReset().mockReturnValue({
    data: { traces: [] },
    isError: false,
    isPending: false,
  });
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(
    function (this: HTMLAnchorElement) {
      downloads(this.download);
    },
  );
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:test");
  vi.spyOn(URL, "revokeObjectURL").mockImplementation((url) => {
    revokeObjectURL(url);
  });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("SecretBlockJourney", () => {
  it("skips creation when a matching blocking secrets policy already exists", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <SecretBlockJourney
        status="not-started"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(
      screen.getByRole("heading", {
        name: "Download the observability plugin",
      }),
    ).toBeTruthy();
    expect(mutation.mutateAsync).not.toHaveBeenCalled();
  });

  it("creates the blocking secrets policy with the required request", async () => {
    render(
      <SecretBlockJourney
        status="not-started"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Publish policy" }));

    await waitFor(() =>
      expect(mutation.mutateAsync).toHaveBeenCalledWith({
        request: {
          gramProject: "project-guide-test",
          createRiskPolicyRequestBody: {
            enabled: true,
            action: "block",
            sources: ["gitleaks"],
            messageTypes: ["tool_request", "tool_response"],
            audienceType: "everyone",
            policyType: "standard",
            autoName: true,
          },
        },
      }),
    );
  });

  it("keeps policy creation retryable after an error", async () => {
    mutation.mutateAsync.mockRejectedValueOnce(new Error("unavailable"));
    render(
      <SecretBlockJourney
        status="not-started"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Publish policy" }));

    await waitFor(() =>
      expect(
        screen.getByText("Could not publish the secrets policy."),
      ).toBeTruthy(),
    );
    expect(screen.getByRole("button", { name: "Publish policy" })).toBeTruthy();
    expect(
      screen.queryByRole("heading", {
        name: "Install the observability plugin",
      }),
    ).toBeNull();
  });

  it("downloads ZIPs only for supported client platforms and reports failures", async () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    fetcher.fetch
      .mockResolvedValueOnce(
        new Response("zip", {
          headers: {
            "Content-Disposition": 'attachment; filename="project-hooks.zip"',
          },
        }),
      )
      .mockResolvedValueOnce(new Response("zip"))
      .mockResolvedValueOnce(new Response("nope", { status: 500 }));
    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(
      screen.getByRole("heading", {
        name: "Download the observability plugin",
      }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Download ZIP" }));
    await waitFor(() =>
      expect(fetcher.fetch).toHaveBeenCalledWith(
        "/rpc/plugins.downloadObservabilityPlugin?platform=claude",
        {},
      ),
    );
    expect(downloads).toHaveBeenCalledWith("project-hooks.zip");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:test");
    expect(
      screen.getByRole("heading", { name: "Add it to your agent" }),
    ).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Download another ZIP" }),
    );
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Cursor" }), {
      button: 0,
    });
    await waitFor(() =>
      expect(
        screen.getByRole("tab", { name: "Cursor" }).getAttribute("data-state"),
      ).toBe("active"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Download ZIP" }));
    await waitFor(() =>
      expect(fetcher.fetch).toHaveBeenCalledWith(
        "/rpc/plugins.downloadObservabilityPlugin?platform=cursor",
        {},
      ),
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Download another ZIP" }),
    );
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Codex" }), {
      button: 0,
    });
    await waitFor(() =>
      expect(
        screen.getByRole("tab", { name: "Codex" }).getAttribute("data-state"),
      ).toBe("active"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Download ZIP" }));

    await waitFor(() =>
      expect(
        screen.getByText("Failed to download observability plugin."),
      ).toBeTruthy(),
    );
  });

  it("uses Radix client tabs with a panel", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(
      screen
        .getByRole("tab", { name: "Claude Code" })
        .getAttribute("data-state"),
    ).toBe("active");
    expect(screen.getByRole("tabpanel")).toBeTruthy();
  });

  it("explains installation without claiming a download proves it", async () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Download ZIP" }));

    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Add it to your agent" }),
      ).toBeTruthy(),
    );
    expect(screen.getByText(/Extract the ZIP in your agent/)).toBeTruthy();
    expect(screen.getByText(/Waiting for the first hook event/)).toBeTruthy();
    expect(screen.queryByText(/download confirms installation/i)).toBeNull();
  });

  it("resumes after reload when the policy and a hook trace already exist", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: {
        traces: [
          {
            gramUrn: "urn:gram:trace:test",
            logCount: 1,
            startTimeUnixNano: "1",
            traceId: "00000000000000000000000000000000",
          },
        ],
      },
      isError: false,
      isPending: false,
    });
    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(
      screen.getByText("Plugin connected. Send the synthetic secret prompt."),
    ).toBeTruthy();
    expect(screen.getByText(/AKIAIOSFODNN7EXAMPLE/)).toBeTruthy();
  });
});
