import { QueryClient, useQuery } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery } from "@/lib/adminQueries";
import { GramAdminError, type AdminOrganization } from "@/lib/gramAdminApi";
import { renderWithApp } from "@/test/harness";
import { WriteReportContext } from "@/pages/organizations/writeReport";

import { Overview } from "./Overview";

const mocks = vi.hoisted(() => ({
  getOrganization: vi.fn(),
  markEnterpriseTrialConverted: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getOrganization: mocks.getOrganization,
    markEnterpriseTrialConverted: mocks.markEnterpriseTrialConverted,
  };
});

// Radix Presence keeps a closing dialog mounted through its exit animation.
// FocusScope still owns focus during that interval, so an app timer can run too
// early. Animation end then models close-autofocus immediately before the
// focused dialog control disconnects. There is deliberately no DialogTrigger.
vi.mock("@/components/ui/dialog", async () => {
  const React = await import("react");
  const State = React.createContext<{
    open: boolean;
    close: () => void;
  }>({ open: false, close: () => {} });

  return {
    Dialog: ({
      children,
      open,
      onOpenChange,
    }: {
      children: React.ReactNode;
      open: boolean;
      onOpenChange?: (open: boolean) => void;
    }) => (
      <State.Provider value={{ open, close: () => onOpenChange?.(false) }}>
        {children}
      </State.Provider>
    ),
    DialogContent: ({
      children,
      onCloseAutoFocus,
    }: {
      children: React.ReactNode;
      onCloseAutoFocus?: (event: Event) => void;
    }) => {
      const state = React.useContext(State);
      const wasOpened = React.useRef(false);
      const [, redraw] = React.useReducer((n: number) => n + 1, 0);
      if (state.open) wasOpened.current = true;
      if (!state.open && !wasOpened.current) return null;

      return (
        <div
          role="dialog"
          data-state={state.open ? "open" : "closed"}
          onKeyDown={(event) => {
            if (event.key === "Escape") state.close();
          }}
          onAnimationEnd={() => {
            if (state.open) return;
            const event = new Event("closeAutoFocus", { cancelable: true });
            onCloseAutoFocus?.(event);
            wasOpened.current = false;
            redraw();
          }}
        >
          {children}
        </div>
      );
    },
    DialogDescription: ({ children }: { children: React.ReactNode }) => (
      <p>{children}</p>
    ),
    DialogFooter: ({ children }: { children: React.ReactNode }) => (
      <div>{children}</div>
    ),
    DialogHeader: ({ children }: { children: React.ReactNode }) => (
      <div>{children}</div>
    ),
    DialogTitle: ({ children }: { children: React.ReactNode }) => (
      <h2>{children}</h2>
    ),
  };
});

const ORG = {
  id: "org_placeholder_one",
  name: "Test Org",
  slug: "test-org",
  account_type: "pro",
  whitelisted: true,
  trial_state: "running",
  trial_tier: "enterprise",
  trial_ends_at: "2026-05-06T00:00:00Z",
  member_count: 3,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-07T00:00:00Z",
} satisfies AdminOrganization;

const CONVERTED = {
  ...ORG,
  account_type: "enterprise",
  trial_state: "converted",
  trial_converted_at: "2026-03-08T12:34:56Z",
} satisfies AdminOrganization;

function CachedOverview(): React.JSX.Element | null {
  const { data } = useQuery(organizationQuery(ORG.id));
  return data ? <Overview org={data} /> : null;
}

async function settleBrowserFocus(): Promise<void> {
  await new Promise<void>((resolve) => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        setTimeout(resolve, 200);
      });
    });
  });
}

async function finishClosingDialog(): Promise<void> {
  const dialog = await screen.findByRole("dialog");
  const focused = within(dialog).getAllByRole("button").at(-1);
  focused?.focus();
  fireEvent.animationEnd(dialog);
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
}

beforeEach(() => {
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(CONVERTED);
  mocks.markEnterpriseTrialConverted.mockReset();
  mocks.markEnterpriseTrialConverted.mockResolvedValue({
    organization_id: ORG.id,
    converted_at: CONVERTED.trial_converted_at,
  });
});

afterEach(cleanup);

describe("Overview conversion without DialogTrigger restoration", () => {
  it("focuses Details after canonical conversion unmounts the trial opener", async () => {
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    qc.setQueryData(organizationQuery(ORG.id).queryKey, ORG);
    const announce = vi.fn<(text: string) => void>(() => {});
    const showFailure = vi.fn<(text: string | null) => void>(() => {});
    await renderWithApp(
      <WriteReportContext.Provider value={{ announce, showFailure }}>
        <CachedOverview />
      </WriteReportContext.Provider>,
      { queryClient: qc },
    );

    fireEvent.click(
      screen.getByRole("button", { name: `Mark ${ORG.name} as converted` }),
    );
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Mark as converted",
      }),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("heading", { name: "Active trial" }),
      ).toBeNull(),
    );
    expect(announce).not.toHaveBeenCalled();

    await finishClosingDialog();
    await settleBrowserFocus();

    expect(document.activeElement).toBe(
      screen.getByRole("heading", { name: "Details" }),
    );
    expect(document.activeElement).not.toBe(document.body);
  });

  it("does not run a stale focus restore after the route unmounts", async () => {
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    qc.setQueryData(organizationQuery(ORG.id).queryKey, ORG);
    const mounted = await renderWithApp(<CachedOverview />, {
      queryClient: qc,
    });
    fireEvent.click(
      screen.getByRole("button", { name: `Mark ${ORG.name} as converted` }),
    );
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Mark as converted",
      }),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("heading", { name: "Active trial" }),
      ).toBeNull(),
    );

    mounted.unmount();
    const safeTarget = document.createElement("button");
    safeTarget.textContent = "Safe target";
    document.body.append(safeTarget);
    safeTarget.focus();
    await settleBrowserFocus();

    expect(document.activeElement).toBe(safeTarget);
    safeTarget.remove();
  });

  it.each(["cancel", "precommit error"] as const)(
    "restores the connected opener after %s",
    async (exit) => {
      mocks.getOrganization.mockResolvedValue(ORG);
      if (exit === "precommit error") {
        mocks.markEnterpriseTrialConverted.mockRejectedValue(
          new GramAdminError(404, { message: "Not Found" }, "gram admin 404"),
        );
      }
      const qc = new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      });
      qc.setQueryData(organizationQuery(ORG.id).queryKey, ORG);
      await renderWithApp(<CachedOverview />, { queryClient: qc });
      const opener = screen.getByRole("button", {
        name: `Mark ${ORG.name} as converted`,
      });
      opener.focus();
      fireEvent.click(opener);
      const dialog = await screen.findByRole("dialog");
      if (exit === "cancel") {
        fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
      } else {
        fireEvent.click(
          within(dialog).getByRole("button", { name: "Mark as converted" }),
        );
        await waitFor(() => expect(dialog.dataset.state).toBe("closed"));
      }

      await finishClosingDialog();

      expect(document.activeElement).toBe(opener);
      expect(document.activeElement).not.toBe(document.body);
    },
  );
});
