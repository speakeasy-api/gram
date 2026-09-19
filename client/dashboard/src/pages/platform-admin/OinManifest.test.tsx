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
      xaa_audience: "https://auth.ready.example.test",
      client_id: "ready-client",
      registration: "static",
      scopes: ["read"],
      blockers: [],
    },
    {
      resource_name: "Orphan Resource",
      resource_as_issuer: "https://orphan.example.test",
      resource_identifier: "https://orphan.example.test",
      xaa_audience: null,
      client_id: null,
      registration: "none",
      scopes: ["read"],
      blockers: ["no global client registered", "audience unknown"],
    },
  ],
  summary: { registrations: 2, ready: 1, blocked: 1 },
};

const markdownBody =
  "# Speakeasy OIN Cross App Access manifest\n\nPreview body.\n";

const mocks = vi.hoisted(() => ({
  exportCalls: [] as Array<{ format: string; session?: string; url: string }>,
  fail: false,
}));

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => true,
  useSession: () => ({ session: "session-token" }),
}));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://server.test",
}));
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
const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
  const format = /format=(\w+)/.exec(url)?.[1] ?? "json";
  const headers = init?.headers as Record<string, string> | undefined;
  mocks.exportCalls.push({ format, session: headers?.["gram-session"], url });
  if (mocks.fail) {
    return new Response("nope", { status: 500 });
  }
  const body = format === "markdown" ? markdownBody : JSON.stringify(manifest);
  const extension = format === "markdown" ? "md" : "json";
  // Only the JSON reply exposes its filename here, so the Markdown download
  // has to fall back to the generation date; both must end up dated.
  return new Response(body, {
    status: 200,
    headers:
      format === "json"
        ? {
            "Content-Disposition": `attachment; filename="speakeasy-oin-xaa-manifest-2026-09-18.${extension}"`,
          }
        : {},
  });
});
vi.stubGlobal("fetch", fetchMock);

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

describe("PlatformAdminOinManifest", () => {
  afterEach(cleanup);
  beforeEach(() => {
    mocks.exportCalls = [];
    mocks.fail = false;
  });

  it("shows a loading state, then blockers, the registrations and the preview", async () => {
    renderPage();

    expect(screen.getByLabelText("Loading manifest")).toBeTruthy();

    await waitFor(() => expect(screen.getByLabelText("Blockers")).toBeTruthy());
    const blockers = within(screen.getByLabelText("Blockers"));
    expect(blockers.getAllByRole("listitem")).toHaveLength(1);
    expect(
      blockers.getByText(/no global client registered; audience unknown/),
    ).toBeTruthy();
    expect(blockers.getByText("Orphan Resource")).toBeTruthy();

    const ready = within(screen.getByTestId("https://ready.example.test"));
    expect(ready.getByText("Ready")).toBeTruthy();
    expect(ready.getByText("ready-client")).toBeTruthy();
    expect(ready.getByText("https://auth.ready.example.test")).toBeTruthy();
    const orphan = within(screen.getByTestId("https://orphan.example.test"));
    expect(orphan.getByText("Not ready")).toBeTruthy();
    expect(orphan.getAllByText("none")).toHaveLength(2);
    expect(orphan.getByText("unknown")).toBeTruthy();

    expect(screen.getByTestId("markdown").textContent).toBe(markdownBody);
    expect(screen.getAllByText("unset")).toHaveLength(2);
    expect(mocks.exportCalls).toEqual([
      {
        format: "json",
        session: "session-token",
        url: "https://server.test/rpc/oinManifest.export?format=json",
      },
      {
        format: "markdown",
        session: "session-token",
        url: "https://server.test/rpc/oinManifest.export?format=markdown",
      },
    ]);
  });

  it("downloads the fetched bodies under the server's filenames", async () => {
    const createObjectURL = vi.fn((_blob: Blob) => "blob:manifest");
    const revokeObjectURL = vi.fn();
    // jsdom has no object URLs; attach them without replacing the URL class.
    Object.assign(URL, { createObjectURL, revokeObjectURL });
    const clicks: Array<{ download: string; href: string }> = [];
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(function (this: HTMLAnchorElement) {
        clicks.push({ download: this.download, href: this.href });
      });

    renderPage();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Download JSON" }),
      ).toBeTruthy(),
    );

    fireEvent.click(screen.getByRole("button", { name: "Download JSON" }));
    fireEvent.click(screen.getByRole("button", { name: "Download Markdown" }));

    expect(clicks.map((c) => c.download)).toEqual([
      "speakeasy-oin-xaa-manifest-2026-09-18.json",
      "speakeasy-oin-xaa-manifest-2026-09-18.md",
    ]);
    const blobs = createObjectURL.mock.calls.map((call) => call[0]);
    expect(await blobs[0]?.text()).toBe(JSON.stringify(manifest));
    expect(await blobs[1]?.text()).toBe(markdownBody);
    // The export is not re-requested to download; the fetched body is reused.
    expect(mocks.exportCalls).toHaveLength(2);

    click.mockRestore();
  });

  it("reports an export failure instead of crashing", async () => {
    mocks.fail = true;
    renderPage();

    await waitFor(() =>
      expect(
        screen.getByText(
          "Failed to export the manifest: export failed with status 500",
        ),
      ).toBeTruthy(),
    );
  });
});
