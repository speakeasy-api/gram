import type { LiteLLMInstance } from "@gram/client/models/components/litellminstance.js";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import type { LitellmInstanceKeyResult } from "@gram/client/models/components/litellminstancekeyresult.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  CreateInstanceDialog,
  DiagnosticsPanel,
  HealthBadge,
  FailurePostureBadge,
  RevokeInstanceDialog,
  RotateKeyDialog,
  SetupContent,
} from "./litellm-integration-row";

const state = vi.hoisted(() => ({
  createOptions: undefined as
    | { onSuccess?: (data: LitellmInstanceKeyResult) => void }
    | undefined,
  createMutation: {
    data: undefined as LitellmInstanceKeyResult | undefined,
    error: null as Error | null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
  invalidate: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://api.example.com",
}));

vi.mock("@gram/client/react-query/createLiteLLMInstance.js", () => ({
  useCreateLiteLLMInstanceMutation: (options: typeof state.createOptions) => {
    state.createOptions = options;
    return state.createMutation;
  },
}));

vi.mock("@gram/client/react-query/liteLLMInstances.js", () => ({
  invalidateAllLiteLLMInstances: state.invalidate,
  useLiteLLMInstances: vi.fn(),
}));

vi.mock("@/components/code", () => ({
  CodeBlock: ({
    children,
    copyLabel = "code",
  }: {
    children: string;
    copyLabel?: string;
  }) => (
    <div>
      <button aria-label={`Copy ${copyLabel}`} />
      <pre>{children}</pre>
    </div>
  ),
}));

const instance: LiteLLMInstance = {
  active: true,
  createdAt: new Date("2026-08-03T12:00:00Z"),
  createdByUserId: "user-test",
  diagnostics: {
    lastGuardrailEventAt: new Date("2026-08-03T12:01:00Z"),
    lastOtelEventAt: new Date("2026-08-03T12:02:00Z"),
    platformUserPct24h: 75,
    reportedLitellmVersion: "1.95.0",
    status: "success",
    virtualKeyEmailPct24h: 80,
  },
  failurePosture: "fail_closed",
  id: "instance-test",
  keyPrefix: "gram_local_test",
  name: "Production",
  organizationId: "org-test",
  project: { id: "project-test", name: "Project", slug: "project" },
  updatedAt: new Date("2026-08-03T12:02:00Z"),
};

const result: LitellmInstanceKeyResult = {
  instance,
  key: "gram_local_one_time_key",
};

beforeEach(() => {
  state.createOptions = undefined;
  state.createMutation.data = undefined;
  state.createMutation.error = null;
  state.createMutation.isPending = false;
  state.createMutation.mutate.mockReset();
  state.createMutation.reset.mockReset();
  state.invalidate.mockReset();
});

afterEach(cleanup);

describe("LiteLLM integration dialogs", () => {
  it("keeps an unfamiliar last error visible as Unknown", () => {
    render(
      <DiagnosticsPanel
        instance={{
          ...instance,
          diagnostics: {
            ...instance.diagnostics,
            lastErrorKind: unrecognized("future_error"),
          },
        }}
      />,
    );
    expect(screen.getByText("Unknown")).toBeDefined();
    expect(screen.queryByText("future_error")).toBeNull();
  });
  it("shows connection health in diagnostics", () => {
    render(<DiagnosticsPanel instance={instance} />);

    expect(screen.getByText("Connection health")).toBeDefined();
    expect(screen.getByText("Connected")).toBeDefined();
  });

  it("keeps a rotated key visible until explicit acknowledgement", () => {
    const onClose = vi.fn<() => void>(() => {});
    render(
      <RotateKeyDialog
        target={instance}
        result={result}
        error={null}
        isPending={false}
        onConfirm={() => {}}
        onClose={onClose}
      />,
    );

    expect(screen.getByText(result.key)).toBeDefined();
    expect(
      screen.getByRole("button", { name: "Copy integration key" }),
    ).toBeDefined();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "I have saved the key" }),
    );
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("shows a created key before an unresolved list refresh and requires acknowledgement", () => {
    state.createMutation.data = result;
    const onOpenChange = vi.fn<(open: boolean) => void>(() => {});
    render(
      <CreateInstanceDialog
        open
        onOpenChange={onOpenChange}
        projects={[{ id: "project-test", name: "Project", slug: "project" }]}
        initialProjectSlug="project"
        onProjectCreated={() => {}}
      />,
    );

    expect(screen.getByText(result.key)).toBeDefined();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onOpenChange).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "I have saved the key" }),
    );
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(state.createMutation.reset).toHaveBeenCalledOnce();
  });

  it("does not await list invalidation before publishing create success", () => {
    state.invalidate.mockReturnValue(new Promise(() => {}));
    const onProjectCreated = vi.fn<(projectSlug: string) => void>(() => {});
    render(
      <CreateInstanceDialog
        open
        onOpenChange={() => {}}
        projects={[{ id: "project-test", name: "Project", slug: "project" }]}
        initialProjectSlug="project"
        onProjectCreated={onProjectCreated}
      />,
    );

    const callbackResult = state.createOptions?.onSuccess?.(result);

    expect(callbackResult).toBeUndefined();
    expect(state.invalidate).toHaveBeenCalledOnce();
    expect(onProjectCreated).toHaveBeenCalledWith("project");
  });

  it("prevents dismissal while revocation is pending and confirms explicitly", () => {
    const onClose = vi.fn<() => void>(() => {});
    const onConfirm = vi.fn<() => void>(() => {});
    const view = render(
      <RevokeInstanceDialog
        target={instance}
        isPending
        onConfirm={onConfirm}
        onClose={onClose}
      />,
    );

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).not.toHaveBeenCalled();
    expect(
      (screen.getByRole("button", { name: "Working…" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);

    view.rerender(
      <RevokeInstanceDialog
        target={instance}
        isPending={false}
        onConfirm={onConfirm}
        onClose={onClose}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Revoke integration" }));
    expect(onConfirm).toHaveBeenCalledOnce();
  });
});

describe("LiteLLM setup failure posture", () => {
  it("withholds guardrail configuration for an unknown posture while preserving a one-time key", () => {
    render(
      <SetupContent
        result={{
          ...result,
          instance: {
            ...instance,
            failurePosture: unrecognized("future_posture"),
          },
        }}
      />,
    );
    expect(
      screen.getByText(
        /Save the integration key securely before refreshing the page/,
      ),
    ).toBeTruthy();
    expect(
      screen.queryByText(/Refresh the page before configuring LiteLLM/),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Copy LiteLLM configuration" }),
    ).toBeNull();
    expect(screen.queryByText(/unreachable_fallback/)).toBeNull();
    expect(screen.queryByText(/future_posture/)).toBeNull();
    expect(
      screen.getByRole("button", { name: "Copy integration key" }),
    ).toBeTruthy();
    expect(screen.getByText(result.key)).toBeTruthy();
  });
  it.each(["fail_closed", "fail_open"] as const)(
    "keeps configuration copyable for %s",
    (posture) => {
      render(
        <SetupContent instance={{ ...instance, failurePosture: posture }} />,
      );
      expect(
        screen.getByRole("button", { name: "Copy LiteLLM configuration" }),
      ).toBeTruthy();
      expect(
        screen.getByText(new RegExp(`unreachable_fallback: ${posture}`)),
      ).toBeTruthy();
      expect(screen.queryByText(/unsupported failure posture/)).toBeNull();
    },
  );
});

it("can recommend refreshing existing-instance setup without a one-time key", () => {
  render(
    <SetupContent
      instance={{ ...instance, failurePosture: unrecognized("future_posture") }}
    />,
  );
  expect(
    screen.getByText(/Refresh the page before configuring LiteLLM/),
  ).toBeTruthy();
  expect(
    screen.queryByText(/Save the integration key securely before refreshing/),
  ).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Copy integration key" }),
  ).toBeNull();
});

describe("LiteLLM status badges", () => {
  it("does not report an unknown diagnostic status as connected", () => {
    render(
      <HealthBadge
        instance={{
          ...instance,
          diagnostics: {
            ...instance.diagnostics,
            status: unrecognized("future_status"),
          },
        }}
      />,
    );
    expect(screen.getByText("Unknown")).toBeTruthy();
    expect(screen.queryByText("Connected")).toBeNull();
    expect(screen.queryByText("Error")).toBeNull();
  });
  it.each([
    ["success", "Connected"],
    ["failed", "Error"],
    ["pending", "Waiting for traffic"],
  ] as const)("preserves the known health state %s", (status, label) => {
    render(
      <HealthBadge
        instance={{
          ...instance,
          diagnostics: { ...instance.diagnostics, status },
        }}
      />,
    );
    expect(screen.getByText(label)).toBeTruthy();
    expect(screen.queryByText("Unknown")).toBeNull();
  });
  it("does not present an unknown failure posture as fail open", () => {
    render(<FailurePostureBadge posture={unrecognized("future_posture")} />);
    expect(screen.getByText("Unknown")).toBeTruthy();
    expect(screen.queryByText("Fail open")).toBeNull();
    expect(screen.queryByText("Fail closed")).toBeNull();
  });
  it.each([
    ["fail_closed", "Fail closed"],
    ["fail_open", "Fail open"],
  ] as const)("preserves the known failure posture %s", (posture, label) => {
    render(<FailurePostureBadge posture={posture} />);
    expect(screen.getByText(label)).toBeTruthy();
    expect(screen.queryByText("Unknown")).toBeNull();
  });
});
