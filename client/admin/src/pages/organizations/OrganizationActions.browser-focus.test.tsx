import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AdminOrganization } from "@/lib/gramAdminApi";
import { renderWithApp } from "@/test/harness";

import { OrganizationActions } from "./OrganizationActions";

// The browser smoke proved that Radix can remove this controlled dialog without
// calling DialogContent's close-autofocus hook. This deliberately small dialog
// stand-in represents that path: it moves focus into the dialog, disconnects
// that focused node on Cancel/Escape, and provides no DialogTrigger or implicit
// focus restoration for the component to accidentally rely on.
vi.mock("@/components/ui/dialog", async () => {
  const React = await import("react");
  const CloseDialog = React.createContext<(() => void) | undefined>(undefined);

  return {
    Dialog: ({
      children,
      onOpenChange,
    }: {
      children: React.ReactNode;
      onOpenChange?: (open: boolean) => void;
    }) => (
      <CloseDialog.Provider value={() => onOpenChange?.(false)}>
        {children}
      </CloseDialog.Provider>
    ),
    DialogContent: ({ children }: { children: React.ReactNode }) => {
      const close = React.useContext(CloseDialog);
      return (
        <div
          role="dialog"
          onKeyDown={(event) => {
            if (event.key === "Escape") close?.();
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

const ORG: AdminOrganization = {
  id: "org_placeholder_one",
  name: "Test Org",
  slug: "test-org",
  account_type: "enterprise",
  whitelisted: true,
  trial_state: "running",
  trial_ends_at: "2026-05-06T00:00:00Z",
  member_count: 3,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-07T00:00:00Z",
};

afterEach(cleanup);

describe("OrganizationActions without DialogTrigger restoration", () => {
  it.each(["Cancel", "Escape"] as const)(
    "restores the connected opener after %s disconnects the focused dialog control",
    async (exit) => {
      await renderWithApp(<OrganizationActions org={ORG} layout="buttons" />);
      const opener = screen.getByRole("button", {
        name: `Disable ${ORG.name}`,
      });
      opener.focus();
      fireEvent.click(opener);

      const dialog = await screen.findByRole("dialog");
      const cancel = screen.getByRole("button", { name: "Cancel" });
      cancel.focus();
      if (exit === "Cancel") fireEvent.click(cancel);
      else fireEvent.keyDown(dialog, { key: "Escape" });

      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      await waitFor(() => expect(document.activeElement).toBe(opener));
      expect(document.activeElement).not.toBe(document.body);
    },
  );

  it("uses action-specific accessible names rather than Save", async () => {
    await renderWithApp(<OrganizationActions org={ORG} layout="buttons" />);
    fireEvent.click(
      screen.getByRole("button", { name: `Disable ${ORG.name}` }),
    );
    expect(await screen.findByRole("button", { name: "Disable" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();

    cleanup();
    const demoted = {
      ...ORG,
      account_type: "free",
      trial_state: "demoted" as const,
      trial_ends_at: undefined,
    };
    await renderWithApp(<OrganizationActions org={demoted} layout="buttons" />);
    fireEvent.click(
      screen.getByRole("button", { name: `Re-arm trial for ${ORG.name}` }),
    );
    expect(await screen.findByRole("button", { name: "Re-arm" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();

    cleanup();
    await renderWithApp(
      <OrganizationActions
        org={{ ...ORG, disabled_at: "2026-03-04T00:00:00Z" }}
        layout="buttons"
      />,
    );
    expect(
      screen.getByRole("button", { name: `Re-enable ${ORG.name}` }),
    ).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
