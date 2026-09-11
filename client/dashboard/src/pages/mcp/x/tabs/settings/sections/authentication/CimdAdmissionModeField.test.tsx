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
    organizationId: "org-1",
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
      screen
        .getByRole("radio", { name: "Any client" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      screen
        .getByRole("radio", { name: "Verified clients" })
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

    expect(screen.queryByText(/Nobody vets it first/i)).toBeNull();

    fireEvent.click(screen.getByRole("radio", { name: "Any client" }));

    // The caution rides with the option's own explanation, so it appears
    // only while that option is the selection.
    expect(screen.getByText(/Nobody vets it first/i)).toBeDefined();
  });

  it("saves a mode change directly for an already-configured issuer", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    fireEvent.click(screen.getByRole("radio", { name: "Off" }));
    fireEvent.click(screen.getByRole("button", { name: /Sav(e|ing)/ }));

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

    fireEvent.click(screen.getByRole("radio", { name: "Verified clients" }));
    fireEvent.click(screen.getByRole("button", { name: /Sav(e|ing)/ }));

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

  it("offers no save until the selection differs from the persisted mode", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    // A permanently mounted, permanently disabled Save reads as broken
    // chrome; the button exists only while there is a change to write.
    expect(screen.queryByRole("button", { name: /Sav(e|ing)/ })).toBeNull();

    fireEvent.click(screen.getByRole("radio", { name: "Any client" }));
    expect(screen.getByRole("button", { name: /Sav(e|ing)/ })).toHaveProperty(
      "disabled",
      false,
    );

    fireEvent.click(screen.getByRole("radio", { name: "Verified clients" }));
    expect(screen.queryByRole("button", { name: /Sav(e|ing)/ })).toBeNull();
  });

  it("disables Save without the project:write scope even when dirty", () => {
    testState.hasScope = false;
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    // Dirty the field first: without this the button is disabled anyway and
    // the assertion would pass with no scope gate at all.
    fireEvent.click(screen.getByRole("radio", { name: "Any client" }));

    expect(screen.getByRole("button", { name: /Sav(e|ing)/ })).toHaveProperty(
      "disabled",
      true,
    );

    fireEvent.click(screen.getByRole("button", { name: /Sav(e|ing)/ }));
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

    fireEvent.click(screen.getByRole("radio", { name: "Any client" }));

    expect(screen.getByRole("button", { name: /Sav(e|ing)/ })).toHaveProperty(
      "disabled",
      true,
    );
  });

  it("opens the allowed-clients table from the explanation line", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("presets")} />);

    // The catalog and the project's own URLs are one table inside the modal
    // now; this field only owns the affordance that opens it.
    fireEvent.click(
      screen.getByRole("button", { name: /Manage allowed clients/ }),
    );

    expect(
      screen.getByRole("dialog", { name: "Allowed clients" }),
    ).toBeDefined();
  });

  it("does not change the selection when the explainer is opened", () => {
    render(<CimdAdmissionModeField userSessionIssuer={issuer("disabled")} />);

    fireEvent.click(screen.getByRole("button", { name: /What is this/ }));

    // The explainer is a modal, so the radios leave the accessible tree
    // while it is open — query past that to prove reading what the setting
    // means never arms a different policy.
    expect(
      screen
        .getByRole("radio", { name: "Off", hidden: true })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      screen
        .getByRole("radio", { name: "Verified clients", hidden: true })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(testState.mutate).not.toHaveBeenCalled();
  });
});
