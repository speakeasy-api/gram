import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
import { PluginRepositoryMigrationsPage } from "./PluginRepositoryMigrations";

const first = "00000000-0000-4000-8000-000000000001";
const second = "00000000-0000-4000-8000-000000000002";
const entries = [
  {
    project_id: first,
    repository_id: "9007199254740993",
    evidence: "Provisioning record A",
  },
  {
    project_id: second,
    repository_id: "1234",
    evidence: "Provisioning record B",
  },
];
const items = [
  {
    project_id: first,
    organization_id: "org_fixture",
    repo_owner: "fixture",
    repo_name: "original",
    bound: false,
  },
  {
    project_id: second,
    organization_id: "org_fixture",
    repo_owner: "fixture",
    repo_name: "already-bound",
    bound: true,
  },
];
const fetch = vi.fn(async (request: Request) => {
  const url = new URL(request.url);
  switch (url.pathname) {
    case "/admin/plugin-repository-migrations":
      expect(request.method).toBe("GET");
      return Response.json({ items });
    case "/admin/plugin-repository-migrations/apply": {
      expect(request.method).toBe("POST");
      const body = (await request.clone().json()) as {
        entries: typeof entries;
        dry_run: boolean;
      };
      return Response.json({
        items: body.entries.map((entry) => ({
          project_id: entry.project_id,
          status:
            entry.project_id === second
              ? "conflict"
              : body.dry_run
                ? "verified"
                : "bound",
          message: "Fixture outcome",
        })),
      });
    }
    case "/admin/plugin-repository-migrations/reprovision":
      expect(request.method).toBe("POST");
      return Response.json({
        status: "publish_needed",
        message: "Replacement provisioned",
      });
    default:
      throw new Error(`Unexpected request: ${request.method} ${url.pathname}`);
  }
});

beforeEach(() => {
  fetch.mockClear();
  vi.stubGlobal("fetch", fetch);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function requestsFor(action: string) {
  return fetch.mock.calls
    .map(([request]) => request)
    .filter((request) => new URL(request.url).pathname.endsWith(`/${action}`));
}

async function open() {
  await renderWithApp(<PluginRepositoryMigrationsPage />);
  await screen.findByRole("heading", { name: "Legacy repository inventory" });
}

async function enterBatch(source: string) {
  fireEvent.change(screen.getByLabelText("Evidence JSON"), {
    target: { value: source },
  });
  fireEvent.click(screen.getByRole("button", { name: "Preview evidence" }));
}

async function resolveProject() {
  fireEvent.click(
    screen.getByRole("button", { name: `Resolve project ${first}` }),
  );
  return screen.findByRole("region", { name: "Resolve one project" });
}

describe("plugin repository migrations", () => {
  it("previews the batch, preserves decimal IDs, and applies only verified entries", async () => {
    await open();
    await enterBatch(JSON.stringify(entries));
    const apply = await screen.findByRole("button", {
      name: "Apply verified entries (1)",
    });
    expect(screen.getByText("conflict")).toBeTruthy();
    expect(requestsFor("apply")).toHaveLength(1);
    expect(await requestsFor("apply")[0]?.clone().json()).toEqual({
      dry_run: true,
      entries,
    });
    fireEvent.click(apply);
    await screen.findByRole("heading", { name: "Apply results" });
    expect(requestsFor("apply")).toHaveLength(2);
    expect(await requestsFor("apply")[1]?.clone().json()).toEqual({
      dry_run: false,
      entries: [entries[0]],
    });
    expect(
      (
        screen.getByRole("button", {
          name: "Apply verified entries",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("requires a new preview after editing and rejects oversized imports locally", async () => {
    await open();
    await enterBatch(JSON.stringify(entries));
    await screen.findByRole("button", { name: "Apply verified entries (1)" });
    fireEvent.change(screen.getByLabelText("Evidence JSON"), {
      target: { value: JSON.stringify([entries[1]]) },
    });
    expect(
      screen.queryByRole("heading", { name: "Preview results" }),
    ).toBeNull();
    expect(
      (
        screen.getByRole("button", {
          name: "Apply verified entries",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    await enterBatch(
      JSON.stringify(Array.from({ length: 51 }, () => entries[0])),
    );
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "Import 1–50 records at a time.",
    );
    expect(requestsFor("apply")).toHaveLength(1);
  });

  it("rejects duplicate repository IDs before previewing", async () => {
    await open();
    await enterBatch(
      JSON.stringify([
        entries[0],
        { ...entries[1], repository_id: entries[0]?.repository_id },
      ]),
    );
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "Record 2: Duplicate GitHub repository ID.",
    );
    expect(requestsFor("apply")).toHaveLength(0);
  });

  it("uses the exact unresolved project ID for manual verification", async () => {
    await open();
    expect(
      screen.queryByRole("button", { name: `Resolve project ${second}` }),
    ).toBeNull();
    const manual = within(await resolveProject());
    fireEvent.change(manual.getByLabelText("Historical GitHub repository ID"), {
      target: { value: "1234" },
    });
    fireEvent.change(manual.getByLabelText("Trusted historical reference"), {
      target: { value: "Historical record" },
    });
    fireEvent.click(
      manual.getByRole("button", { name: "Preview project binding" }),
    );
    await manual.findByText("verified");
    fireEvent.click(
      manual.getByRole("button", { name: "Apply verified binding" }),
    );
    await manual.findByRole("heading", { name: "Apply results" });
    const expected = [
      {
        project_id: first,
        repository_id: "1234",
        evidence: "Historical record",
      },
    ];
    expect(await requestsFor("apply")[0]?.clone().json()).toEqual({
      dry_run: true,
      entries: expected,
    });
    expect(await requestsFor("apply")[1]?.clone().json()).toEqual({
      dry_run: false,
      entries: expected,
    });
  });

  it("only replaces the reviewed project after explicit confirmation", async () => {
    await open();
    const manual = within(await resolveProject());
    fireEvent.change(manual.getByLabelText("Replacement reason"), {
      target: { value: "Unable to verify the historical repository" },
    });
    const review = manual.getByRole("button", { name: "Review replacement" });
    fireEvent.click(review);
    let dialog = within(await screen.findByRole("dialog"));
    expect(dialog.getByText(`Project: ${first}`)).toBeTruthy();
    fireEvent.click(dialog.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(requestsFor("reprovision")).toHaveLength(0);

    fireEvent.click(review);
    dialog = within(await screen.findByRole("dialog"));
    fireEvent.click(
      dialog.getByRole("button", { name: "Provision replacement" }),
    );
    expect(await manual.findByRole("status")).toHaveProperty(
      "textContent",
      "publish needed Replacement provisioned",
    );
    expect(await requestsFor("reprovision")[0]?.clone().json()).toEqual({
      project_id: first,
      reason: "Unable to verify the historical repository",
    });
  });

  it("paginates with the API cursor and returns to the first page", async () => {
    fetch.mockResolvedValueOnce(
      Response.json({ items: [items[0]], next_cursor: first }),
    );
    fetch.mockResolvedValueOnce(Response.json({ items: [items[1]] }));
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByText("fixture/already-bound");
    expect(
      new URL(fetch.mock.calls[1]?.[0].url ?? "").searchParams.get("after"),
    ).toBe(first);
    fireEvent.click(screen.getByRole("button", { name: "Previous" }));
    await screen.findByText("fixture/original");
    expect(screen.getByText("Page 1")).toBeTruthy();
  });

  it("lets staff preview but disables writes when the API denies platform admin access", async () => {
    await open();
    const manual = within(await resolveProject());
    fireEvent.change(manual.getByLabelText("Replacement reason"), {
      target: { value: "Historical record missing" },
    });
    expect(
      (
        manual.getByRole("button", {
          name: "Review replacement",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
    await enterBatch(JSON.stringify(entries));
    const apply = await screen.findByRole("button", {
      name: "Apply verified entries (1)",
    });
    fetch.mockResolvedValueOnce(
      Response.json({ message: "platform admin required" }, { status: 403 }),
    );
    fireEvent.click(apply);
    await screen.findByText(/You can still preview evidence/);
    expect(
      (
        manual.getByRole("button", {
          name: "Review replacement",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    await enterBatch(JSON.stringify(entries));
    expect(
      (
        (await screen.findByRole("button", {
          name: "Apply verified entries (1)",
        })) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("withholds migration controls when the admin inventory rejects the session", async () => {
    fetch.mockResolvedValueOnce(
      Response.json({ message: "Forbidden" }, { status: 403 }),
    );
    await renderWithApp(<PluginRepositoryMigrationsPage />);
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "You do not have access to plugin repository migrations.",
    );
    expect(
      screen.queryByRole("button", { name: "Preview evidence" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: `Resolve project ${first}` }),
    ).toBeNull();
  });
});
