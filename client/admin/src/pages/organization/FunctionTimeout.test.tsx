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

import { FunctionTimeout } from "./FunctionTimeout";

const mocks = vi.hoisted(() => ({
  getOrganization: vi.fn(),
  setFunctionTimeout: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  return {
    ...actual,
    getOrganization: mocks.getOrganization,
    setFunctionTimeout: mocks.setFunctionTimeout,
  };
});

const ORG = anOrganization({
  id: "org_placeholder_one",
  name: "Example Org",
  slug: "example-org",
  function_timeout_seconds: 60,
  function_timeout_is_default: true,
});

const RAISED: AdminOrganization = {
  ...ORG,
  function_timeout_seconds: 300,
  function_timeout_is_default: false,
};

function queryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
}

// Reads the record from the cache like the overview does, so a saved value
// shows up through writeOrganizationToCache rather than a prop change.
function CachedTimeout(): JSX.Element | null {
  const { data } = useQuery(organizationQuery(ORG.id));
  return data ? <FunctionTimeout org={data} /> : null;
}

async function renderTimeout(
  org: AdminOrganization = ORG,
): Promise<{ qc: QueryClient; announce: Mock }> {
  const qc = queryClient();
  qc.setQueryData(organizationQuery(org.id).queryKey, org);
  mocks.getOrganization.mockResolvedValue(org);
  const announce = vi.fn<(message: string) => void>();
  const showFailure = vi.fn<(message: string | null) => void>();
  await renderWithApp(
    <WriteReportContext.Provider value={{ announce, showFailure }}>
      <CachedTimeout />
    </WriteReportContext.Provider>,
    { queryClient: qc },
  );
  return { qc, announce };
}

function field(): HTMLElement {
  return screen.getByRole("spinbutton", {
    name: "Function timeout in seconds",
  });
}

function enterSeconds(value: string): void {
  fireEvent.change(field(), { target: { value } });
}

function save(): void {
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
}

beforeEach(() => {
  mocks.getOrganization.mockReset();
  mocks.setFunctionTimeout.mockReset();
});

afterEach(cleanup);

describe("FunctionTimeout", () => {
  it("shows the platform default", async () => {
    await renderTimeout();

    expect(screen.getByText("1 minute")).toBeTruthy();
    expect(screen.getByText("(default)")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Edit" })).toBeTruthy();
  });

  it("shows a staff-set limit without the default marker", async () => {
    await renderTimeout(RAISED);

    expect(screen.getByText("5 minutes")).toBeTruthy();
    expect(screen.queryByText("(default)")).toBeNull();
  });

  it.each(["29", "601", "45.5", ""])(
    "shows the inline error and sends nothing for %j",
    async (value) => {
      await renderTimeout();

      fireEvent.click(screen.getByRole("button", { name: "Edit" }));
      enterSeconds(value);
      save();

      expect(
        await screen.findByText(
          "Enter a whole number of seconds from 30 to 600.",
        ),
      ).toBeTruthy();
      expect(field().getAttribute("aria-invalid")).toBe("true");
      expect(mocks.setFunctionTimeout).not.toHaveBeenCalled();
    },
  );

  it("saves a new limit and shows it", async () => {
    mocks.setFunctionTimeout.mockResolvedValue(RAISED);
    const { announce } = await renderTimeout();
    // The save invalidates the record; the refetch answers with the new value.
    mocks.getOrganization.mockResolvedValue(RAISED);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.queryByRole("button", { name: "Use default" })).toBeNull();
    enterSeconds("300");
    save();

    expect(await screen.findByText("5 minutes")).toBeTruthy();
    expect(mocks.setFunctionTimeout).toHaveBeenCalledTimes(1);
    expect(mocks.setFunctionTimeout.mock.calls[0]?.[0]).toEqual({
      organization_id: ORG.id,
      function_timeout_seconds: 300,
    });
    expect(announce).toHaveBeenCalledWith("Function timeout saved");
  });

  it("restores the default without sending a value", async () => {
    mocks.setFunctionTimeout.mockResolvedValue(ORG);
    const { announce } = await renderTimeout(RAISED);
    mocks.getOrganization.mockResolvedValue(ORG);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(screen.getByRole("button", { name: "Use default" }));

    expect(await screen.findByText("(default)")).toBeTruthy();
    expect(mocks.setFunctionTimeout.mock.calls[0]?.[0]).toEqual({
      organization_id: ORG.id,
    });
    expect(announce).toHaveBeenCalledWith(
      "Function timeout reset to the default",
    );
  });

  it("leaves the saved value alone on cancel", async () => {
    await renderTimeout(RAISED);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    enterSeconds("45");
    fireEvent.keyDown(field(), { key: "Escape" });

    expect(screen.getByText("5 minutes")).toBeTruthy();
    expect(mocks.setFunctionTimeout).not.toHaveBeenCalled();
  });

  it("refetches the organization when a failed save may have landed", async () => {
    mocks.setFunctionTimeout.mockRejectedValue(
      new GramAdminError(500, {}, "unexpected"),
    );
    const { qc } = await renderTimeout();
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    enterSeconds("120");
    save();

    expect(
      await screen.findByText(/Couldn't save the function timeout/),
    ).toBeTruthy();
    // The value itself is fine, so the field is not marked invalid.
    expect(field().hasAttribute("aria-invalid")).toBe(false);
    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith(
        expect.objectContaining({
          queryKey: organizationQuery(ORG.id).queryKey.slice(0, 1),
        }),
      );
    });
  });
});
