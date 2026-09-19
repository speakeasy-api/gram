import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { ApplicationsTab } from "./ApplicationsTab";

const mocks = vi.hoisted(() => ({ applications: vi.fn() }));
vi.mock(
  "@gram/client/react-query/identityProviderConnectionApplications.js",
  () => ({ useIdentityProviderConnectionApplications: mocks.applications }),
);
vi.mock(
  "@gram/client/react-query/syncIdentityProviderConnectionApplications.js",
  () => ({
    useSyncIdentityProviderConnectionApplicationsMutation: () => ({
      isPending: false,
    }),
  }),
);
afterEach(cleanup);

function show(
  status: "running" | "failed" | "succeeded",
  count = 0,
  requestPending = false,
) {
  mocks.applications.mockReturnValue({
    data: {
      applications: Array.from({ length: count }, (_, index) => ({
        oktaAppId: `app-${index}`,
        label: `Application ${index + 1}`,
        name: `app-${index}`,
        signOnMode: "OPENID_CONNECT",
        status: "ACTIVE",
        userAssignments: 0,
        groupAssignments: 0,
        firstSeenAt: new Date("2026-01-01T00:00:00Z"),
        lastSeenAt: new Date("2026-01-01T00:00:00Z"),
      })),
      sync: {
        syncedAt: new Date("2026-01-01T00:00:00Z"),
        requestedAt: requestPending
          ? new Date("2026-01-02T00:00:00Z")
          : undefined,
      },
      lastRun: {
        status,
        startedAt: new Date("2026-01-01T00:00:00Z"),
        applicationsSeen: 0,
        applicationsAdded: 0,
        applicationsRemoved: 0,
        assignmentsAdded: 0,
        assignmentsRemoved: 0,
        skippedAppIds: [],
      },
    },
  });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <ApplicationsTab
          connection={
            {
              id: "connection",
              status: "verified",
            } as OktaIdentityProviderConnection
          }
          rolloutEnabled
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

it("prevents duplicate sync requests while the current run is running", () => {
  show("running");
  expect(screen.getByText("Application updates")).toBeTruthy();
  expect(screen.getByText(/The application list is updating/)).toBeTruthy();
  expect(
    screen
      .getByRole("button", { name: "Updating..." })
      .hasAttribute("disabled"),
  ).toBe(true);
  expect(
    screen.getByText(/Applications will appear here when it finishes/),
  ).toBeTruthy();
});

it("directs an empty failed snapshot toward recovery, not waiting", () => {
  show("failed");
  expect(
    screen.getByText(/The last update failed. Review the error above/),
  ).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Update now" }).hasAttribute("disabled"),
  ).toBe(false);
});

it("previews seven applications and allows expanding and collapsing the snapshot", () => {
  show("succeeded", 9);
  expect(screen.getByText("Application 7")).toBeTruthy();
  expect(screen.queryByText("Application 8")).toBeNull();
  expect(screen.queryByText(/Showing \d+ of \d+ applications/)).toBeNull();
  const toggle = screen.getByRole("button", {
    name: "Show all 9 applications",
  });
  expect(toggle.textContent).toContain("Show all 9 applications");
  expect(toggle.hasAttribute("aria-label")).toBe(false);
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
  expect(toggle.getAttribute("aria-controls")).toBe(
    screen.getByRole("region", { name: "Identity provider applications" }).id,
  );
  fireEvent.click(toggle);
  expect(screen.getByText("Application 9")).toBeTruthy();
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
  expect(toggle.textContent).toContain("Show fewer applications");
  expect(screen.getByRole("button", { name: "Show fewer applications" })).toBe(
    toggle,
  );
  fireEvent.click(toggle);
  expect(screen.queryByText("Application 8")).toBeNull();
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
});

it.each([0, 7])(
  "does not offer expansion for a snapshot with %i applications",
  (count) => {
    show("succeeded", count);
    expect(screen.queryByRole("button", { name: /Show all/ })).toBeNull();
  },
);

it("does not force an empty snapshot to table width", () => {
  show("succeeded");
  const table = screen
    .getByRole("region", { name: "Identity provider applications" })
    .querySelector("table");
  expect(table).not.toBeNull();
  expect(table?.closest(".min-w-\\[880px\\]")).toBeNull();
});

it("labels a queued update consistently and prevents duplicate requests", () => {
  show("succeeded", 0, true);
  expect(
    screen
      .getByRole("button", { name: "Update requested" })
      .hasAttribute("disabled"),
  ).toBe(true);
  expect(screen.getByText(/waiting to start/)).toBeTruthy();
  expect(screen.getByText(/The application list is updating/)).toBeTruthy();
});
