import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Column } from "@/components/ui/Table";
import type { OinRegistration } from "./OinManifest";

const manifest = {
  manifest_version: 1,
  generated_at: "2026-09-18T23:10:00Z",
  requesting_app: {
    name: "unset",
    org_domain: "unset",
    sso_mode: "saml",
    role: "requesting_app",
    redirect_uri: "https://gram.example.test/oauth/callback",
  },
  resource_registrations: [
    {
      resource_name: "Ready Resource",
      resource_as_issuer: "https://ready.example.test",
      resource_identifier: "https://ready.example.test/mcp",
      xaa_audience: "https://ready.example.test",
      client_id: "ready-client",
      registration: "static",
      scopes: ["read"],
      blockers: [],
    },
    {
      resource_name: "Orphan Resource",
      resource_as_issuer: "https://orphan.example.test",
      resource_identifier: "",
      xaa_audience: "https://orphan.example.test",
      client_id: null,
      registration: "none",
      scopes: ["read"],
      blockers: ["no global client registered", "resource identifier missing"],
    },
  ],
  summary: { registrations: 2, ready: 1, blocked: 1 },
};

const markdownBody =
  "# Speakeasy OIN Cross App Access manifest\n\nPreview body.\n";

const mocks = vi.hoisted(() => ({
  exportCalls: [] as Array<{ format: string }>,
  fail: false,
  readyOnly: false,
}));

vi.mock("@/contexts/Auth", () => ({ useIsPlatformAdmin: () => true }));
vi.mock("@/components/page-layout", () => {
  const Wrapper = ({ children }: { children: ReactNode }) => <>{children}</>;
  return {
    Page: Object.assign(Wrapper, {
      Header: Object.assign(Wrapper, { Breadcrumbs: Wrapper }),
      Body: Wrapper,
      Section: Object.assign(Wrapper, {
        Title: Wrapper,
        Description: Wrapper,
        Body: Wrapper,
      }),
    }),
  };
});
vi.mock("@/components/ui/Table", () => ({
  Table: ({
    columns,
    data,
  }: {
    columns: Column<OinRegistration>[];
    data: OinRegistration[];
  }) => (
    <div>
      {data.map((row) => (
        <div key={row.resource_as_issuer} data-testid={row.resource_as_issuer}>
          {columns.map((column) => (
            <div key={String(column.key)}>{column.render?.(row)}</div>
          ))}
        </div>
      ))}
    </div>
  ),
}));
vi.mock("react-markdown", () => ({
  default: ({ children }: { children: string }) => (
    <div data-testid="markdown">{children}</div>
  ),
}));
vi.mock("remark-gfm", () => ({ default: () => undefined }));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    oinManifest: {
      export: async (request: { format?: string }) => {
        const format = request.format ?? "json";
        mocks.exportCalls.push({ format });
        if (mocks.fail) throw new Error("export failed");
        const body =
          format === "markdown"
            ? markdownBody
            : JSON.stringify(
                mocks.readyOnly
                  ? {
                      ...manifest,
                      resource_registrations:
                        manifest.resource_registrations.slice(0, 1),
                      summary: { registrations: 1, ready: 1, blocked: 0 },
                    }
                  : manifest,
              );
        // Only the JSON reply exposes its filename here, so the Markdown
        // download has to fall back to the generation date; both must end
        // up dated.
        const headers: Record<string, string[]> =
          format === "json"
            ? {
                "content-disposition": [
                  'attachment; filename="speakeasy-oin-xaa-manifest-2026-09-18.json"',
                ],
              }
            : {};
        return { headers, result: new Blob([body]).stream() };
      },
    },
  }),
}));

import PlatformAdminOinManifest from "./OinManifest";

function renderPage(): QueryClient {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <PlatformAdminOinManifest />
    </QueryClientProvider>,
  );
  return queryClient;
}

// jsdom has no object URLs and no navigation on anchor clicks; both are
// stubbed per test and restored afterwards so a failed assertion cannot leak
// them into the next test.
const globals = {
  createObjectURL: vi.fn((_blob: Blob) => "blob:manifest"),
  revokeObjectURL: vi.fn(),
  clicks: [] as Array<{ download: string; href: string }>,
  click: undefined as { mockRestore: () => void } | undefined,
};
const objectURLMethods = ["createObjectURL", "revokeObjectURL"] as const;
const originalObjectURL = Object.fromEntries(
  objectURLMethods.map((name) => [
    name,
    Object.getOwnPropertyDescriptor(URL, name),
  ]),
);
function restoreObjectURL(): void {
  for (const name of objectURLMethods) {
    const descriptor = originalObjectURL[name];
    if (descriptor) Object.defineProperty(URL, name, descriptor);
    else delete (URL as unknown as Record<string, unknown>)[name];
  }
}

describe("PlatformAdminOinManifest", () => {
  beforeEach(() => {
    mocks.exportCalls = [];
    mocks.fail = false;
    mocks.readyOnly = false;
    globals.clicks = [];
    globals.createObjectURL.mockClear();
    globals.revokeObjectURL.mockClear();
    Object.assign(URL, {
      createObjectURL: globals.createObjectURL,
      revokeObjectURL: globals.revokeObjectURL,
    });
    globals.click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(function (this: HTMLAnchorElement) {
        globals.clicks.push({ download: this.download, href: this.href });
      });
  });
  afterEach(async () => {
    cleanup();
    // Drain downloadText's deferred revocations while URL mocks still exist,
    // even when a test assertion fails before it can await those timers.
    await new Promise<void>((resolve) => {
      setTimeout(resolve, 0);
    });
    globals.click?.mockRestore();
    restoreObjectURL();
  });

  it("shows a loading state, then blockers, the registrations and the preview", async () => {
    renderPage();

    expect(screen.getByLabelText("Loading manifest")).toBeTruthy();

    await waitFor(() => expect(screen.getByLabelText("Blockers")).toBeTruthy());
    const blockers = within(screen.getByLabelText("Blockers"));
    expect(blockers.getAllByRole("listitem")).toHaveLength(1);
    expect(
      blockers.getByText(
        /no global client registered; resource identifier missing/,
      ),
    ).toBeTruthy();
    expect(blockers.getByText("Orphan Resource")).toBeTruthy();

    const ready = within(screen.getByTestId("https://ready.example.test"));
    expect(ready.getByText("Catalog ready")).toBeTruthy();
    expect(ready.getByText("ready-client")).toBeTruthy();
    expect(ready.getAllByText("https://ready.example.test")).toHaveLength(2);
    const orphan = within(screen.getByTestId("https://orphan.example.test"));
    expect(orphan.getByText("Not ready")).toBeTruthy();
    expect(orphan.getAllByText("none")).toHaveLength(2);
    expect(orphan.getAllByText("https://orphan.example.test")).toHaveLength(2);

    expect(screen.getByTestId("markdown").textContent).toBe(markdownBody);
    expect(screen.getAllByText("unset")).toHaveLength(2);
    expect(mocks.exportCalls).toEqual([
      { format: "json" },
      { format: "markdown" },
    ]);
  });

  it("does not imply submission readiness when there are no catalog blockers", async () => {
    mocks.readyOnly = true;
    renderPage();

    await waitFor(() =>
      expect(screen.getByText("No catalog blockers found.")).toBeTruthy(),
    );
    expect(screen.getAllByText("Catalog ready")).toHaveLength(2);
    expect(screen.getByText(/Conformance not verified/).textContent).toContain(
      "passing conformance log generated within the previous 48 hours",
    );
    expect(
      screen.getByRole("link", { name: "OIN Wizard" }).getAttribute("href"),
    ).toBe(
      "https://developer.okta.com/docs/guides/submit-oin-app/scrossapp/main/",
    );
    expect(
      screen.getByText("No catalog blockers found.").closest('[role="alert"]')
        ?.textContent,
    ).toBe("No catalog blockers found.");
    expect(screen.queryByLabelText("Blockers")).toBeNull();
  });

  it("downloads the fetched bodies under the server's filenames", async () => {
    renderPage();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Download JSON" }),
      ).toBeTruthy(),
    );

    fireEvent.click(screen.getByRole("button", { name: "Download JSON" }));
    fireEvent.click(screen.getByRole("button", { name: "Download Markdown" }));

    expect(globals.clicks.map((c) => c.download)).toEqual([
      "speakeasy-oin-xaa-manifest-2026-09-18.json",
      "speakeasy-oin-xaa-manifest-2026-09-18.md",
    ]);
    const blobs = globals.createObjectURL.mock.calls.map((call) => call[0]);
    expect(await blobs[0]?.text()).toBe(JSON.stringify(manifest));
    expect(await blobs[1]?.text()).toBe(markdownBody);
    await waitFor(() =>
      expect(globals.revokeObjectURL).toHaveBeenCalledTimes(2),
    );
    expect(globals.revokeObjectURL.mock.calls).toEqual([
      ["blob:manifest"],
      ["blob:manifest"],
    ]);
    // The export is not re-requested to download; the fetched body is reused.
    expect(mocks.exportCalls).toHaveLength(2);
  });

  it("reports an export failure instead of crashing", async () => {
    mocks.fail = true;
    renderPage();

    await waitFor(() =>
      expect(
        screen.getByText("Failed to export the manifest: export failed"),
      ).toBeTruthy(),
    );
  });
});
