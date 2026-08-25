import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CimdAdmissionModeField } from "./CimdAdmissionModeField";

const testState = vi.hoisted(() => ({
  hasScope: true,
  mutate: vi.fn(),
  updatePending: false,
  updateError: null as Error | null,
  presets: [] as {
    clientIdMetadataUri: string;
    displayName: string;
    enabled: boolean;
    isPattern: boolean;
    vendorKey: string;
  }[],
  presetsLoading: false,
  presetsError: false,
}));

// Mirrors the real RequireScope contract: it disables (rather than hides)
// component-level children and supports the render-function form.
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: ReactNode | ((props: { disabled: boolean }) => ReactNode);
  }) => (
    <>
      {typeof children === "function"
        ? children({ disabled: !testState.hasScope })
        : children}
    </>
  ),
}));

vi.mock("@gram/client/react-query/cimdClientPresets.js", () => ({
  useCimdClientPresets: () => ({
    data: { items: testState.presets },
    isLoading: testState.presetsLoading,
    isError: testState.presetsError,
  }),
}));

vi.mock("@gram/client/react-query/updateUserSessionIssuer.js", () => ({
  useUpdateUserSessionIssuerMutation: () => ({
    mutate: testState.mutate,
    isPending: testState.updatePending,
    isError: testState.updateError !== null,
    error: testState.updateError,
  }),
}));

vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  invalidateAllUserSessionIssuer: vi.fn(),
}));

vi.mock("@gram/client/react-query/userSessionIssuers.js", () => ({
  invalidateAllUserSessionIssuers: vi.fn(),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

const toastMock = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("sonner", () => ({ toast: toastMock }));

function issuer(
  mode: UserSessionIssuer["clientIdMetadataAdmissionMode"],
): UserSessionIssuer {
  return {
    authnChallengeMode: "chain",
    clientIdMetadataAdmissionMode: mode,
    createdAt: new Date(0),
    id: "issuer-1",
    projectId: "project-1",
    sessionDurationHours: 24,
    slug: "issuer",
    updatedAt: new Date(0),
  };
}

beforeEach(() => {
  testState.hasScope = true;
  testState.updatePending = false;
  testState.updateError = null;
  testState.presetsLoading = false;
  testState.presetsError = false;
  testState.presets = [
    {
      clientIdMetadataUri: "https://claude.ai/oauth/mcp-oauth-client-metadata",
      displayName: "Anthropic (Claude)",
      enabled: true,
      isPattern: false,
      vendorKey: "anthropic",
    },
    {
      clientIdMetadataUri: "https://retired.example.com/client.json",
      displayName: "Retired Vendor",
      enabled: false,
      isPattern: false,
      vendorKey: "retired",
    },
  ];
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("CimdAdmissionModeField", () => {
  it("renders the persisted mode as the selected option", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("open")} />);

    expect(
      screen.getByRole("radio", { name: "Open" }).getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      screen
        .getByRole("radio", { name: "Known clients (recommended)" })
        .getAttribute("aria-checked"),
    ).toBe("false");
  });

  it("leaves every option unselected while the issuer is unconfigured", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("reporting")} />);

    for (const radio of screen.getAllByRole("radio")) {
      expect(radio.getAttribute("aria-checked")).toBe("false");
    }
  });

  it("never renders the read-only reporting mode as a choice", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("reporting")} />);

    // "reporting" is returned by the read API but is not writable, so it must
    // not reach the control — an operator picking it would 400.
    expect(
      screen.queryByRole("radio", { name: /reporting|recording/i }),
    ).toBeNull();
  });

  it("warns about origin reach when Open is selected", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    expect(screen.queryByText(/any valid CIMD client/i)).toBeNull();

    fireEvent.click(screen.getByRole("radio", { name: "Open" }));

    expect(screen.getByText(/any valid CIMD client/i)).toBeDefined();
  });

  it("saves a mode change directly for an already-configured issuer", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    fireEvent.click(screen.getByRole("radio", { name: "Disabled" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        updateUserSessionIssuerForm: {
          id: "issuer-1",
          clientIdMetadataAdmissionMode: "disabled",
        },
      },
    });
  });

  it("saves the first explicit mode directly, with no confirmation step", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("reporting")} />);

    fireEvent.click(
      screen.getByRole("radio", { name: "Known clients (recommended)" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        updateUserSessionIssuerForm: {
          id: "issuer-1",
          clientIdMetadataAdmissionMode: "presets",
        },
      },
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps Save inert until the selection differs from the persisted mode", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toHaveProperty("disabled", true);

    fireEvent.click(screen.getByRole("radio", { name: "Open" }));
    expect(save).toHaveProperty("disabled", false);

    fireEvent.click(
      screen.getByRole("radio", { name: "Known clients (recommended)" }),
    );
    expect(save).toHaveProperty("disabled", true);
  });

  it("disables Save without the project:write scope even when dirty", () => {
    testState.hasScope = false;
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    // Dirty the field first: without this the button is disabled anyway and
    // the assertion would pass with no scope gate at all.
    fireEvent.click(screen.getByRole("radio", { name: "Open" }));

    expect(screen.getByRole("button", { name: "Save" })).toHaveProperty(
      "disabled",
      true,
    );

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(testState.mutate).not.toHaveBeenCalled();
  });

  it("offers exactly the three writable modes", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("reporting")} />);

    expect(screen.getAllByRole("radio")).toHaveLength(3);
    expect(screen.queryByRole("radio", { name: /recording/i })).toBeNull();
  });

  it("shows a save failure inline without a duplicate toast", () => {
    testState.updateError = new Error("issuer is locked");
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    expect(screen.getByText("issuer is locked")).toBeDefined();
    expect(toastMock.error).not.toHaveBeenCalled();
  });

  it("blocks a second submit while the save is in flight", () => {
    testState.updatePending = true;
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    fireEvent.click(screen.getByRole("radio", { name: "Open" }));

    expect(screen.getByRole("button", { name: "Save" })).toHaveProperty(
      "disabled",
      true,
    );
  });

  it("lists enabled presets from the API and hides disabled ones", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    fireEvent.click(screen.getByRole("button", { name: /What's included/ }));

    expect(screen.getByText("Anthropic (Claude)")).toBeDefined();
    expect(
      screen.getByText("https://claude.ai/oauth/mcp-oauth-client-metadata"),
    ).toBeDefined();
    expect(screen.queryByText("Retired Vendor")).toBeNull();
  });

  it("selects a mode when the card body is clicked, not just the radio", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    // The description is the largest part of the card and reads as part of
    // the target, so clicking it must select.
    fireEvent.click(screen.getByText(/Reject all CIMD clients/));

    expect(
      screen
        .getByRole("radio", { name: "Disabled" })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("does not select Known clients when opening its presets popover", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("disabled")} />);

    fireEvent.click(screen.getByRole("button", { name: /What's included/ }));

    // The trigger sits inside the Known clients card; arming that mode as a
    // side effect of reading the catalog would be a silent policy change.
    expect(
      screen
        .getByRole("radio", { name: "Known clients (recommended)" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen
        .getByRole("radio", { name: "Disabled" })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("reports a loading preset catalog", () => {
    testState.presetsLoading = true;
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    fireEvent.click(screen.getByRole("button", { name: /What's included/ }));

    expect(screen.getByText("Loading verified clients…")).toBeDefined();
    expect(screen.queryByText("Anthropic (Claude)")).toBeNull();
  });

  it("reports a failed preset catalog fetch", () => {
    testState.presetsError = true;
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    fireEvent.click(screen.getByRole("button", { name: /What's included/ }));

    expect(
      screen.getByText("Could not load the verified client list."),
    ).toBeDefined();
    expect(screen.queryByText("Anthropic (Claude)")).toBeNull();
  });

  it("explains an empty preset catalog rather than rendering nothing", () => {
    testState.presets = [];
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    fireEvent.click(screen.getByRole("button", { name: /What's included/ }));

    expect(
      screen.getByText("No verified clients are currently enabled."),
    ).toBeDefined();
  });
});
