import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  features: {
    data: undefined as undefined | Record<string, boolean>,
    isLoading: false,
    isFetching: false,
    error: null as unknown,
    refetch: vi.fn(),
  },
  access: {
    available: true,
    holdsRole: false,
    roleReadsSessions: false,
    canReadSessions: false,
    scimManaged: false,
    roleExists: false,
    isPending: false,
    grant: vi.fn(),
    ensureRole: vi.fn(),
    revoke: vi.fn(),
  },
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => mocks.features,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    logs: { href: () => "/org/logs" },
    identity: { href: () => "/org/identity" },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));
// The callout itself is under test here; only its data source is stubbed.
vi.mock("./session-audit-access", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./session-audit-access")>()),
  useSessionAuditAccess: () => mocks.access,
}));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
}));
vi.mock("@/components/observe/LoggingPageHeader", () => ({
  LogDataRetentionBanner: () => null,
}));
vi.mock("./enable-logging-and-session-capture-setting", () => ({
  EnableLoggingAndSessionCaptureSetting: () => <div>logging switch</div>,
}));

import { EnableLoggingSection } from "./enable-logging-section";

afterEach(cleanup);
beforeEach(() => {
  mocks.features.data = {
    logsEnabled: false,
    toolIoLogsEnabled: false,
    sessionCaptureEnabled: false,
  };
  mocks.features.error = null;
  mocks.features.isLoading = false;
  mocks.features.isFetching = false;
  Object.assign(mocks.access, {
    available: true,
    holdsRole: false,
    roleReadsSessions: false,
    canReadSessions: false,
    scimManaged: false,
    roleExists: false,
    isPending: false,
  });
  mocks.access.grant.mockReset();
  mocks.access.ensureRole.mockReset();
  mocks.access.revoke.mockReset();
});

function bundleOn() {
  mocks.features.data = {
    logsEnabled: true,
    toolIoLogsEnabled: true,
    sessionCaptureEnabled: true,
  };
}

describe("EnableLoggingSection", () => {
  it("stays incomplete until the whole bundle is on", () => {
    render(<EnableLoggingSection index={1} />);

    expect(screen.getByText("Enable logging")).toBeTruthy();
    expect(screen.getByText("logging switch")).toBeTruthy();
    // The number stands in for the check mark while the step is outstanding.
    expect(screen.getByText("1")).toBeTruthy();
  });

  it("completes once logs, tool I/O and session capture are all on", () => {
    mocks.features.data = {
      logsEnabled: true,
      toolIoLogsEnabled: true,
      sessionCaptureEnabled: true,
    };

    render(<EnableLoggingSection index={1} />);

    expect(screen.queryByText("1")).toBeNull();
  });

  it("clears the check when part of the bundle goes back off", () => {
    // The switch invalidates product features after every write, so a partial
    // disable — whether from a failed write or a change made elsewhere — has
    // to reopen the step rather than leave it checked.
    mocks.features.data = {
      logsEnabled: true,
      toolIoLogsEnabled: true,
      sessionCaptureEnabled: false,
    };

    render(<EnableLoggingSection index={1} />);

    expect(screen.getByText("1")).toBeTruthy();
  });

  it("offers a retry when the current setting cannot be read", () => {
    mocks.features.data = undefined;
    mocks.features.error = new Error("nope");

    render(<EnableLoggingSection index={1} />);

    expect(screen.getByRole("alert").textContent).toContain(
      "Couldn't load the current logging setting.",
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
  });
});

describe("EnableLoggingSection session audit callout", () => {
  it("stays hidden until the logging bundle is on", () => {
    render(<EnableLoggingSection index={1} />);

    expect(screen.queryByText("Sessions are private by default")).toBeNull();
  });

  it("offers the Session Auditor role once logging is on", () => {
    bundleOn();

    render(<EnableLoggingSection index={1} />);

    expect(screen.getByText("Sessions are private by default")).toBeTruthy();
    const button = screen.getByRole("button", {
      name: "Add me as Session Auditor",
    });
    fireEvent.click(button);
    expect(mocks.access.grant).toHaveBeenCalledTimes(1);
  });

  it("says nothing to an admin who can already read other members' sessions", () => {
    bundleOn();
    mocks.access.canReadSessions = true;

    render(<EnableLoggingSection index={1} />);

    expect(screen.queryByText("Sessions are private by default")).toBeNull();
  });

  it("collapses to a status line while the caller holds the role", () => {
    bundleOn();
    mocks.access.holdsRole = true;
    mocks.access.roleReadsSessions = true;
    // Holding the role is what grants chat:read, so both are true together.
    mocks.access.canReadSessions = true;

    render(<EnableLoggingSection index={1} />);

    expect(
      screen.getByText(
        "You hold Session Auditor access. Remove it after confirming traffic.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Remove my access" }));
    expect(mocks.access.revoke).toHaveBeenCalledTimes(1);
  });

  it("asks a directory-synced org to create the role and map a group", () => {
    bundleOn();
    mocks.access.scimManaged = true;
    mocks.access.available = false;

    render(<EnableLoggingSection index={1} />);

    expect(screen.getByText("Identity → SCIM → Configure")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Add me as Session Auditor" }),
    ).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Create Session Auditor role" }),
    );
    expect(mocks.access.ensureRole).toHaveBeenCalledTimes(1);
  });

  it("marks the role created rather than offering to create it twice", () => {
    bundleOn();
    mocks.access.scimManaged = true;
    mocks.access.available = false;
    mocks.access.roleExists = true;

    render(<EnableLoggingSection index={1} />);

    const button = screen.getByRole("button", { name: "Created" });
    expect(button.hasAttribute("disabled")).toBe(true);
  });

  it("keeps offering the role when the one held reads nothing", () => {
    // Its grants were edited away after the caller joined, so membership
    // proves nothing. Taking it again repairs the role.
    bundleOn();
    mocks.access.holdsRole = true;
    mocks.access.roleReadsSessions = false;

    render(<EnableLoggingSection index={1} />);

    expect(screen.getByText("Sessions are private by default")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Remove my access" }),
    ).toBeNull();
  });
});
