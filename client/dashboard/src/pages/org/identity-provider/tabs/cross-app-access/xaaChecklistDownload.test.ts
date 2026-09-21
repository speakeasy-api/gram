import { beforeEach, describe, expect, it, vi } from "vitest";
import { GramCore } from "@gram/client/core.js";
import { HTTPClient } from "@gram/client/lib/http.js";

import { downloadXaaChecklist } from "./xaaChecklistDownload";

const mocks = vi.hoisted(() => ({ downloadBlob: vi.fn() }));

vi.mock("@/lib/download", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/download")>()),
  downloadBlob: mocks.downloadBlob,
}));

function clientReturning(response: () => Response): {
  client: GramCore;
  requests: Request[];
} {
  const requests: Request[] = [];
  const fetcher = (input: RequestInfo | URL, init?: RequestInit) => {
    requests.push(new Request(input, init));
    return Promise.resolve(response());
  };
  return {
    client: new GramCore({
      serverURL: "https://gram.test",
      httpClient: new HTTPClient({ fetcher }),
    }),
    requests,
  };
}

describe("downloadXaaChecklist", () => {
  beforeEach(() => mocks.downloadBlob.mockReset());

  it("streams a text/csv body through the SDK and names the file from the header", async () => {
    const { client, requests } = clientReturning(
      () =>
        new Response("server,resource\n", {
          status: 200,
          headers: {
            "content-type": "text/csv; charset=utf-8",
            "content-disposition": 'attachment; filename="xaa-checklist.csv"',
          },
        }),
    );

    await downloadXaaChecklist(client, "csv", true);

    expect(requests).toHaveLength(1);
    const url = new URL(requests[0]!.url);
    expect(url.pathname).toBe("/rpc/xaaReadiness.exportChecklist");
    expect(url.searchParams.get("format")).toBe("csv");
    expect(url.searchParams.get("include_all")).toBe("true");
    expect(mocks.downloadBlob).toHaveBeenCalledTimes(1);
    const [blob, filename] = mocks.downloadBlob.mock.calls[0] as [Blob, string];
    expect(filename).toBe("xaa-checklist.csv");
    expect(await blob.text()).toBe("server,resource\n");
  });

  it("accepts a markdown body and falls back to a default name", async () => {
    const { client } = clientReturning(
      () =>
        new Response("# Checklist\n", {
          status: 200,
          headers: { "content-type": "text/markdown" },
        }),
    );

    await downloadXaaChecklist(client, "markdown", false);

    expect(mocks.downloadBlob).toHaveBeenCalledWith(
      expect.any(Blob),
      "xaa-checklist.md",
    );
  });

  it("surfaces the API error instead of downloading", async () => {
    const { client } = clientReturning(
      () =>
        new Response(
          JSON.stringify({
            name: "forbidden",
            id: "x",
            message: "okta connections are not enabled for this organization",
            temporary: false,
            timeout: false,
            fault: false,
          }),
          { status: 403, headers: { "content-type": "application/json" } },
        ),
    );

    await expect(downloadXaaChecklist(client, "csv", false)).rejects.toThrow(
      /not enabled/,
    );
    expect(mocks.downloadBlob).not.toHaveBeenCalled();
  });
});
