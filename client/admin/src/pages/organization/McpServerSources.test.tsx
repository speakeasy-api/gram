import { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
import { McpServerSources } from "./McpServerSources";

const target = {
  organizationId: "org_example",
  projectId: "proj_example",
  mcpServerId: "srv_example",
};
const fetchMock = vi.fn();
const click = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:source");
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    click(this.download, this.href);
  });
  click.mockReset();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("lists sources and downloads bytes using the exact scoped admin target", async () => {
  fetchMock.mockResolvedValueOnce(
    Response.json({
      sources: [
        {
          asset_id: "asset_example",
          name: "Example API",
          filename: "Example API.yaml",
        },
      ],
    }),
  );
  fetchMock.mockResolvedValueOnce(new Response("openapi: 3.1.0"));
  await renderWithApp(<McpServerSources {...target} />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Download Example API" }),
  );
  await waitFor(() =>
    expect(click).toHaveBeenCalledWith("Example API.yaml", "blob:source"),
  );
  expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:source");
  const [listPath, listInit] = fetchMock.mock.calls[0]!;
  expect(listPath).toBe(
    "/admin/mcpServerSources?organization_id=org_example&project_id=proj_example&mcp_server_id=srv_example",
  );
  expect(listInit.headers.get("Accept")).toBe("application/json");
  const [downloadPath, downloadInit] = fetchMock.mock.calls[1]!;
  expect(downloadPath).toBe(
    "/admin/mcpServerSourceDownload?organization_id=org_example&project_id=proj_example&mcp_server_id=srv_example&asset_id=asset_example",
  );
  expect(downloadInit.headers.get("Accept")).toBe("application/octet-stream");
  expect(listInit.credentials).not.toBe("omit");
  expect(downloadInit.credentials).not.toBe("omit");
});
it("shows an empty state", async () => {
  fetchMock.mockResolvedValue(Response.json({ sources: [] }));
  await renderWithApp(<McpServerSources {...target} />);
  expect(await screen.findByText("No OpenAPI sources.")).toBeTruthy();
});
it("announces loading", async () => {
  fetchMock.mockReturnValue(new Promise(() => {}));
  await renderWithApp(<McpServerSources {...target} />);
  expect(screen.getByRole("status").textContent).toBe("Loading sources…");
});
it("reports list failures and allows retry", async () => {
  fetchMock.mockResolvedValueOnce(new Response("", { status: 403 }));
  await renderWithApp(<McpServerSources {...target} />);
  expect(await screen.findByRole("alert")).toBeTruthy();
  fetchMock.mockResolvedValueOnce(Response.json({ sources: [] }));
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("No OpenAPI sources.")).toBeTruthy();
});
it("disables a pending download and reports failures without saving a file", async () => {
  fetchMock.mockResolvedValueOnce(
    Response.json({ sources: [{ asset_id: "a", name: "source.json" }] }),
  );
  let finish!: (response: Response) => void;
  fetchMock.mockReturnValueOnce(
    new Promise<Response>((resolve) => {
      finish = resolve;
    }),
  );
  await renderWithApp(<McpServerSources {...target} />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Download source.json" }),
  );
  await waitFor(() =>
    expect(
      screen
        .getByRole("button", { name: "Download source.json" })
        .hasAttribute("disabled"),
    ).toBe(true),
  );
  finish(new Response("", { status: 500 }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(click).not.toHaveBeenCalled();
  expect(
    screen
      .getByRole("button", { name: "Download source.json" })
      .hasAttribute("disabled"),
  ).toBe(false);
});

it("reuses a fresh list and keeps downloads available after a refresh fails", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  fetchMock.mockResolvedValueOnce(
    Response.json({
      sources: [
        { asset_id: "a", name: "Example API", filename: "example.yaml" },
      ],
    }),
  );
  const first = await renderWithApp(<McpServerSources {...target} />, {
    queryClient,
  });
  await screen.findByRole("button", { name: "Download Example API" });
  first.unmount();
  await renderWithApp(<McpServerSources {...target} />, { queryClient });
  expect(fetchMock).toHaveBeenCalledTimes(1);
  fetchMock.mockResolvedValueOnce(new Response("", { status: 500 }));
  await queryClient.invalidateQueries();
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Unable to refresh sources",
  );
  expect(
    screen
      .getByRole("button", { name: "Download Example API" })
      .hasAttribute("disabled"),
  ).toBe(false);
});
