import { type JSX } from "react";
import { QueryClient, useQuery } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type Mock,
} from "vitest";

import { organizationQuery } from "@/lib/adminQueries";
import { GramAdminError, type AdminOrganization } from "@/lib/gramAdminApi";
import { WriteReportContext } from "@/pages/organizations/writeReport";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

import { AdoptionDashboardLink } from "./AdoptionDashboardLink";

const mocks = vi.hoisted(() => ({
  getOrganization: vi.fn(),
  setAdoptionDashboardUrl: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  return {
    ...actual,
    getOrganization: mocks.getOrganization,
    setAdoptionDashboardUrl: mocks.setAdoptionDashboardUrl,
  };
});

const LINK = "https://example.com/adoption";

const ORG = anOrganization({
  id: "org_placeholder_one",
  name: "Example Org",
  slug: "example-org",
});

function queryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
}

// Reads the record from the cache like the overview does, so a saved link
// shows up through writeOrganizationToCache rather than a prop change.
function CachedLink(): JSX.Element | null {
  const { data } = useQuery(organizationQuery(ORG.id));
  return data ? <AdoptionDashboardLink org={data} /> : null;
}

async function renderLink(
  org: AdminOrganization = ORG,
): Promise<{ qc: QueryClient; announce: Mock }> {
  const qc = queryClient();
  qc.setQueryData(organizationQuery(org.id).queryKey, org);
  mocks.getOrganization.mockResolvedValue(org);
  const announce = vi.fn<(message: string) => void>();
  const showFailure = vi.fn<(message: string | null) => void>();
  await renderWithApp(
    <WriteReportContext.Provider value={{ announce, showFailure }}>
      <CachedLink />
    </WriteReportContext.Provider>,
    { queryClient: qc },
  );
  return { qc, announce };
}

function enterLink(value: string): void {
  fireEvent.change(
    screen.getByRole("textbox", { name: "Adoption Dashboard Link" }),
    { target: { value } },
  );
}

function save(): void {
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
}

beforeEach(() => {
  mocks.getOrganization.mockReset();
  mocks.setAdoptionDashboardUrl.mockReset();
});

afterEach(cleanup);

describe("AdoptionDashboardLink", () => {
  it("shows a dash and offers to add a link when none is set", async () => {
    await renderLink();

    expect(screen.getByText("-")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add link" })).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("shows the link and offers to edit it when one is set", async () => {
    await renderLink({ ...ORG, adoption_dashboard_url: LINK });

    const link = screen.getByRole("link", { name: LINK });
    expect(link.getAttribute("href")).toBe(LINK);
    expect(link.getAttribute("target")).toBe("_blank");
    expect(screen.getByRole("button", { name: "Edit" })).toBeTruthy();
  });

  it("shows the inline error and sends nothing for an invalid link", async () => {
    await renderLink();

    fireEvent.click(screen.getByRole("button", { name: "Add link" }));
    enterLink("http://example.com/adoption");
    save();

    expect(await screen.findByText("Enter a full https:// link.")).toBeTruthy();
    expect(
      screen
        .getByRole("textbox", { name: "Adoption Dashboard Link" })
        .getAttribute("aria-invalid"),
    ).toBe("true");
    expect(mocks.setAdoptionDashboardUrl).not.toHaveBeenCalled();
  });

  it("saves the trimmed link and shows it", async () => {
    const updated = { ...ORG, adoption_dashboard_url: LINK };
    mocks.setAdoptionDashboardUrl.mockResolvedValue(updated);
    const { announce } = await renderLink();
    // The save invalidates the record; the refetch answers with the new link.
    mocks.getOrganization.mockResolvedValue(updated);

    fireEvent.click(screen.getByRole("button", { name: "Add link" }));
    enterLink(`  ${LINK}  `);
    save();

    const link = await screen.findByRole("link", { name: LINK });
    expect(link.getAttribute("href")).toBe(LINK);
    expect(mocks.setAdoptionDashboardUrl).toHaveBeenCalledTimes(1);
    expect(mocks.setAdoptionDashboardUrl.mock.calls[0]?.[0]).toEqual({
      organization_id: ORG.id,
      adoption_dashboard_url: LINK,
    });
    expect(announce).toHaveBeenCalledWith("Adoption dashboard link saved");
  });

  it("clears the link when saved empty", async () => {
    mocks.setAdoptionDashboardUrl.mockResolvedValue(ORG);
    const { announce } = await renderLink({
      ...ORG,
      adoption_dashboard_url: LINK,
    });
    mocks.getOrganization.mockResolvedValue(ORG);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    enterLink("   ");
    save();

    expect(
      await screen.findByRole("button", { name: "Add link" }),
    ).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
    expect(mocks.setAdoptionDashboardUrl.mock.calls[0]?.[0]).toEqual({
      organization_id: ORG.id,
      adoption_dashboard_url: "",
    });
    expect(announce).toHaveBeenCalledWith("Adoption dashboard link removed");
  });

  it("refetches the organization when a failed save may have landed", async () => {
    mocks.setAdoptionDashboardUrl.mockRejectedValue(
      new GramAdminError(500, {}, "unexpected"),
    );
    const { qc } = await renderLink();
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    fireEvent.click(screen.getByRole("button", { name: "Add link" }));
    enterLink(LINK);
    save();

    expect(
      await screen.findByText(/Couldn't save the adoption dashboard link/),
    ).toBeTruthy();
    // The link itself is fine, so the field is not marked invalid.
    expect(
      screen
        .getByRole("textbox", { name: "Adoption Dashboard Link" })
        .hasAttribute("aria-invalid"),
    ).toBe(false);
    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith(
        expect.objectContaining({
          queryKey: organizationQuery(ORG.id).queryKey.slice(0, 1),
        }),
      );
    });
  });
});
