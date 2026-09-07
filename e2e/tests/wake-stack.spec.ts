import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

// The lifecycle a worktree is handed over in: its stack is built, then paused
// (`.config/wt.toml` -> `git:workboot` -> `mise run pause`), and the developer
// gets it back by opening the same dashboard URL as always and clicking a
// button. Nothing else in the repo exercises that handover, and every part of
// it fails quietly -- a parker that never binds looks like a dead port, a wake
// that half-runs looks like a broken app -- so this drives the whole thing:
//
//   pause -> containers stopped but NOT removed -> the URL still answers with
//   the resume page -> a plain GET leaves the stack paused -> Resume wakes it
//   -> the dashboard comes back -> login works against the woken server.
//
// Runs against whatever worktree it is invoked in, and leaves that worktree's
// stack running.

const repoRoot = path.resolve(import.meta.dirname, "..", "..");

function run(command: string, args: string[], timeoutMs = 15 * 60_000): string {
  return execFileSync(command, args, {
    cwd: repoRoot,
    encoding: "utf8",
    timeout: timeoutMs,
    stdio: ["ignore", "pipe", "inherit"],
  });
}

function mise(...args: string[]): string {
  return run("mise", ["run", ...args]);
}

const gitDir = run("git", ["rev-parse", "--absolute-git-dir"], 10_000).trim();
const marker = (name: string): string => path.join(gitDir, name);

type Container = { name: string; state: string };

// `-a`, because the point of `pause` is that the containers still exist: a
// `down` would satisfy "nothing is running" and break every promise the
// feature makes about waking being fast.
function containers(): Container[] {
  const out = run("docker", [
    "compose",
    "ps",
    "-a",
    "--format",
    "{{.Name}}\t{{.State}}",
  ]);
  return out
    .split("\n")
    .filter((line) => line.trim() !== "")
    .map((line) => {
      const [name = "", state = ""] = line.split("\t");
      return { name, state };
    });
}

function running(): string[] {
  return containers()
    .filter((c) => c.state === "running")
    .map((c) => c.name);
}

// The parker writes this only once it is actually listening, so its presence
// is the difference between "the stack is paused" and "the stack is paused and
// the URL is dead".
function parkerPid(): number | null {
  const raw = fs.readFileSync(marker("gram-stack-parked.pid"), "utf8").trim();
  const pid = Number(raw);
  return Number.isInteger(pid) && pid > 0 ? pid : null;
}

// `pause` and `wake` serialize behind this, and a wake started from the browser
// outlives the process that clicked the button -- so a previous run of this
// suite can still be finishing. Waiting turns a confusing "another pause or
// wake holds the lock" failure into either a pass or an honest timeout.
async function waitForNoStackLock(): Promise<void> {
  await expect
    .poll(
      () => {
        try {
          fs.lstatSync(marker("gram-stack-lock"));
          return true;
        } catch {
          return false;
        }
      },
      {
        timeout: 10 * 60_000,
        message: "a pause or wake from an earlier run still holds the lock",
      },
    )
    .toBe(false);
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

test.describe.configure({ mode: "serial" });

test("a paused worktree serves the resume page and comes back from it", async ({
  page,
}) => {
  await test.step("start from a booted stack", async () => {
    await waitForNoStackLock();

    // Wakes the stack if it is paused and boots it if it has never run, which
    // is also the "container gets created" half of the lifecycle: without
    // containers there is nothing for `pause` to keep.
    mise("ensure-stack");
    expect(
      containers().length,
      "the worktree has no containers",
    ).toBeGreaterThan(0);
  });

  await test.step("pause it", () => {
    mise("pause");

    expect(
      running(),
      "`pause` left containers running; it should stop every profile",
    ).toEqual([]);
    expect(
      containers().length,
      "`pause` removed the containers; it must stop them, not `down` them",
    ).toBeGreaterThan(0);
    expect(fs.existsSync(marker("gram-stack-paused"))).toBe(true);

    const pid = parkerPid();
    expect(pid, "no parker pid file, so the site port is dead").not.toBeNull();
    expect(alive(pid!), "the parker exited after writing its pid").toBe(true);
  });

  await test.step("the dashboard URL answers with the resume page", async () => {
    const response = await page.goto("/");

    // How the page itself tells the parker apart from the dashboard it is
    // waiting for; both answer the same origin.
    expect(response?.headers()["x-gram-parked"]).toBe("1");
    await expect(page).toHaveTitle("Stack paused");
    await expect(
      page.getByRole("heading", { name: "Stack paused" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Resume stack" }),
    ).toBeVisible();
  });

  await test.step("loading that page does not wake anything", async () => {
    // Prefetches, link previews and reconnecting tabs all hit this port. A
    // stack that wakes on a page load cannot be kept paused, so the wake is
    // behind the button and nothing else.
    await page.reload();
    expect(running(), "a GET on the parked port started the stack").toEqual([]);
  });

  const parked = parkerPid();

  await test.step("Resume starts the wake", async () => {
    await page.getByRole("button", { name: "Resume stack" }).click();

    await expect(
      page.getByRole("heading", { name: "Resuming stack" }),
    ).toBeVisible();
    await expect(page).toHaveTitle("Resuming stack…");
    // The page stays up for the whole wake -- reloads and second tabs during
    // it should get the page, not a connection error.
    await expect(
      page.getByText(/logs: pitchfork logs dashboard/),
    ).toBeVisible();
  });

  await test.step("the page reloads into the dashboard", async () => {
    // The parker holds the port until `wake` kills it immediately before vite
    // binds, and the page polls until something without the parker's header
    // answers. Reaching the real index.html is the whole handover working.
    await expect(page.locator("#root")).toBeAttached({ timeout: 10 * 60_000 });
    await expect(page).toHaveTitle("Speakeasy");

    expect(alive(parked!), "the parker outlived the wake").toBe(false);
    expect(running().length, "the wake started no containers").toBeGreaterThan(
      0,
    );

    // Polled rather than asserted outright: `wake` clears the marker only
    // after `mise run start` returns, and vite answers well before that -- so
    // the browser is back on the dashboard while the wake is still finishing.
    await expect
      .poll(() => fs.existsSync(marker("gram-stack-paused")), {
        timeout: 5 * 60_000,
        message: "the stack is up but still marked paused",
      })
      .toBe(false);
  });

  await test.step("login works against the woken stack", async () => {
    // Not a bonus assertion: the dashboard answering only proves vite is up.
    // Logging in goes through vite's proxy to the Go server and from there to
    // Postgres, which is the half of the stack the parker never stood in for.
    await page.getByRole("button", { name: "Log in" }).click();

    await page.waitForURL((url) => !url.pathname.startsWith("/login"), {
      timeout: 3 * 60_000,
    });
    await expect(page.getByRole("button", { name: "Log in" })).toHaveCount(0);
  });
});
