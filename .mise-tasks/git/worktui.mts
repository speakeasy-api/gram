#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Interactive TUI over `wt status`: live boot states, per-worktree logs, multi-select remove"
//MISE dir="{{ config_root }}"

//USAGE flag "--once" help="Print a single status frame and exit (no TTY needed)"

// Interactive wrapper around the same signals `git:workstatus` renders once:
// `wt list` JSON for the worktree table and port liveness, plus the
// `gram-stack-boot.{pid,failed}` markers `git:workboot` maintains in each
// worktree's git dir. On top of that it tails the worktree's post-start
// stack.log (the file `wt config state logs get` points at) for a live boot
// phase and a scrollable log view, and drives `wt remove` for selected rows.
//
// Zero dependencies beyond node builtins by design: raw-mode stdin + ANSI on
// an alternate screen buffer. mise tasks run through a PTY, so raw mode works.

import { execFile as execFileCb, spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { promisify } from "node:util";

const execFile = promisify(execFileCb);

type Row = {
  branch: string;
  path: string;
  isCurrent: boolean;
  url: string;
  urlActive: boolean;
  // Derived: booting | seeding | failed | up | down
  state: string;
  // Uncommitted changes (staged/modified/untracked) — removal needs --force
  dirty: boolean;
  // Lock owner when the worktree is `git worktree lock`ed (e.g. an agent
  // harness locking its checkout); removal must unlock first. "" = unlocked.
  lockedBy: string;
};

const ANSI_RE = new RegExp(
  String.raw`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07|[\r\x07]`,
  "g",
);
const strip = (s: string) => s.replace(ANSI_RE, "");

// OSC-8 terminal hyperlink: renders as `text`, clickable in iTerm2/WezTerm/
// Kitty/recent Terminal.app. Harmless plain text elsewhere.
const link = (text: string, url: string) =>
  `\x1b]8;;${url}\x07${text}\x1b]8;;\x07`;

// Open PR per branch, fetched from `gh pr list` — one call maps the whole
// repo, refreshed on a slow cadence (PRs churn far less than boot state).
type PrInfo = { number: number; url: string; state: string };
const prByBranch = new Map<string, PrInfo>();
let prLoaded = false;
// open > merged > closed when a branch has several PRs (gh lists newest
// first, so within the same state the newest wins).
const PR_RANK: Record<string, number> = {
  open: 0,
  draft: 0,
  merged: 1,
  closed: 2,
};
async function fetchPRs(): Promise<void> {
  try {
    const { stdout } = await execFile(
      "gh",
      [
        "pr",
        "list",
        "--state",
        "all",
        "--json",
        "number,url,headRefName,state,isDraft",
        "--limit",
        "200",
      ],
      { maxBuffer: 8 * 1024 * 1024 },
    );
    prByBranch.clear();
    for (const pr of JSON.parse(stdout)) {
      const state =
        pr.state === "OPEN"
          ? pr.isDraft
            ? "draft"
            : "open"
          : pr.state.toLowerCase();
      const existing = prByBranch.get(pr.headRefName);
      if (existing && PR_RANK[existing.state] <= PR_RANK[state]) continue;
      prByBranch.set(pr.headRefName, { number: pr.number, url: pr.url, state });
    }
  } catch {
    /* gh missing or offline — column stays empty */
  } finally {
    prLoaded = true; // stop the loader either way
  }
}

const c = {
  bold: (s: string) => `\x1b[1m${s}\x1b[22m`,
  dim: (s: string) => `\x1b[2m${s}\x1b[22m`,
  red: (s: string) => `\x1b[31m${s}\x1b[39m`,
  green: (s: string) => `\x1b[32m${s}\x1b[39m`,
  yellow: (s: string) => `\x1b[33m${s}\x1b[39m`,
  magenta: (s: string) => `\x1b[35m${s}\x1b[39m`,
  cyan: (s: string) => `\x1b[36m${s}\x1b[39m`,
  inverse: (s: string) => `\x1b[7m${s}\x1b[27m`,
};

// --- data collection ---------------------------------------------------------

const gitdirCache = new Map<string, string>();
async function gitDir(wtPath: string): Promise<string | null> {
  const hit = gitdirCache.get(wtPath);
  if (hit) return hit;
  try {
    const { stdout } = await execFile("git", [
      "-C",
      wtPath,
      "rev-parse",
      "--absolute-git-dir",
    ]);
    const dir = stdout.trim();
    gitdirCache.set(wtPath, dir);
    return dir;
  } catch {
    return null;
  }
}

let commonDir = "";
async function wtLogFile(branch: string): Promise<string | null> {
  if (!commonDir) {
    const { stdout } = await execFile("git", [
      "rev-parse",
      "--path-format=absolute",
      "--git-common-dir",
    ]);
    commonDir = stdout.trim();
  }
  const logsRoot = path.join(commonDir, "wt", "logs");
  const san = branch.replace(/\//g, "-");
  let dirs: fs.Dirent[];
  try {
    dirs = fs.readdirSync(logsRoot, { withFileTypes: true });
  } catch {
    return null;
  }
  // Worktrunk suffixes the branch with a short hash; several dirs can share a
  // branch prefix across recreations, so take the most recently modified.
  const candidates = dirs
    .filter(
      (d) =>
        d.isDirectory() &&
        new RegExp(
          `^${san.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}-[a-z0-9]{3}$`,
        ).test(d.name),
    )
    .map((d) =>
      path.join(logsRoot, d.name, "project", "post-start", "stack.log"),
    )
    .filter((f) => fs.existsSync(f))
    .sort((a, b) => fs.statSync(b).mtimeMs - fs.statSync(a).mtimeMs);
  return candidates[0] ?? null;
}

function tailFile(file: string, bytes: number): string {
  try {
    const size = fs.statSync(file).size;
    const fd = fs.openSync(file, "r");
    const len = Math.min(bytes, size);
    const buf = Buffer.alloc(len);
    fs.readSync(fd, buf, 0, len, size - len);
    fs.closeSync(fd);
    return buf.toString("utf8");
  } catch {
    return "";
  }
}

async function lockOwner(wtPath: string): Promise<string> {
  const dir = await gitDir(wtPath);
  if (!dir) return "";
  const lockFile = path.join(dir, "locked");
  if (!fs.existsSync(lockFile)) return "";
  const raw = fs.readFileSync(lockFile, "utf8").trim();
  try {
    // Tools lock with a JSON reason ({"owner":"supacode",...}); fall back to
    // the raw reason (or a generic marker) for plain `git worktree lock`.
    return JSON.parse(raw).owner || raw || "locked";
  } catch {
    return raw || "locked";
  }
}

async function bootState(wtPath: string): Promise<"booting" | "failed" | ""> {
  const dir = await gitDir(wtPath);
  if (!dir) return "";
  const pidFile = path.join(dir, "gram-stack-boot.pid");
  if (fs.existsSync(pidFile)) {
    const pid = fs.readFileSync(pidFile, "utf8").trim();
    try {
      // Same recycled-pid guard as git:workstatus: only trust the marker if
      // that pid is still the workboot script.
      const { stdout } = await execFile("ps", ["-o", "command=", "-p", pid]);
      if (stdout.includes("workboot")) return "booting";
    } catch {
      /* process gone */
    }
  }
  if (fs.existsSync(path.join(dir, "gram-stack-boot.failed"))) return "failed";
  return "";
}

async function collect(): Promise<Row[]> {
  const { stdout } = await execFile(
    "wt",
    ["--config-set", "list.json-schema=1", "list", "--format", "json"],
    {
      maxBuffer: 16 * 1024 * 1024,
    },
  );
  const list = JSON.parse(strip(stdout));
  const rows: Row[] = [];
  for (const item of list) {
    if (!item.url) continue;
    const boot = await bootState(item.path);
    let state: string;
    if (boot === "booting") state = item.url_active ? "seeding" : "booting";
    else if (boot === "failed") state = "failed";
    else state = item.url_active ? "up" : "down";
    rows.push({
      branch: item.branch,
      path: item.path,
      isCurrent: !!item.is_current,
      url: item.url,
      urlActive: !!item.url_active,
      state,
      dirty: !!(
        item.working_tree &&
        (item.working_tree.staged ||
          item.working_tree.modified ||
          item.working_tree.untracked ||
          item.working_tree.renamed ||
          item.working_tree.deleted)
      ),
      lockedBy: await lockOwner(item.path),
    });
  }
  return rows;
}

// --- rendering ---------------------------------------------------------------

const STATE_FMT: Record<string, string> = {
  up: c.green("● up     "),
  down: c.red("○ down   "),
  booting: c.yellow("◐ booting"),
  seeding: c.yellow("◐ seeding"),
  failed: c.red("✗ failed "),
};

function listFrame(
  rows: Row[],
  cursor: number,
  selected: Set<string>,
  width: number,
  height: number,
): string {
  const out: string[] = [];
  const help = `${c.bold("q")} quit  ${c.bold("↑↓")} move  ${c.bold("␣")} select  ${c.bold("↵")} logs  ${c.bold("d")} remove`;
  out.push(`  ${c.bold("Worktrees")}  ${c.dim(help)}`);
  out.push("");
  const bw = Math.max(...rows.map((r) => r.branch.length), 6);
  // bw + 2 in the header: every branch cell carries two marker columns
  // (dirty `*`, locked `!`) after the padded name.
  out.push(
    c.bold(
      `      ${"Branch".padEnd(bw + 2)}  ${"State".padEnd(9)}  ${"PR".padEnd(6)} ${"Status".padEnd(7)} URL`,
    ),
  );
  rows.forEach((r, i) => {
    const sel = selected.has(r.branch) ? c.cyan("✓") : " ";
    const cur = i === cursor ? c.cyan("▸") : " ";
    const mark = r.isCurrent ? "@" : "+";
    // `*` = dirty working tree (removal gets --force), `!` = locked (removal
    // unlocks first) — ASCII only: fancier glyphs (⚿) are ambiguous-width and
    // shift the columns in some terminals. Pad first, then append, so ANSI
    // bytes never skew the column width.
    const branchCell =
      r.branch.padEnd(bw) +
      (r.dirty ? c.yellow("*") : " ") +
      (r.lockedBy ? c.yellow("!") : " ");
    // Pad to the visible width before wrapping in the OSC-8 link so the
    // escape bytes don't skew the column.
    const pr = prByBranch.get(r.branch);
    const prCell = pr
      ? link(c.cyan(`#${pr.number}`.padEnd(6)), pr.url)
      : prLoaded
        ? "".padEnd(6)
        : c.dim("…".padEnd(6));
    const PR_STATE_COLOR: Record<string, (s: string) => string> = {
      open: c.green,
      draft: c.dim,
      merged: c.magenta,
      closed: c.red,
    };
    const statusCell = pr
      ? (PR_STATE_COLOR[pr.state] ?? c.dim)(pr.state.padEnd(7))
      : "".padEnd(7);
    const line = `${cur} ${sel} ${mark} ${branchCell}  ${STATE_FMT[r.state] ?? r.state}  ${prCell} ${statusCell} ${r.urlActive ? r.url : c.dim(r.url)}`;
    out.push(i === cursor ? line : line);
  });
  const failed = rows.filter((r) => r.state === "failed").length;
  if (failed > 0) {
    out.push("");
    out.push(
      c.red(
        `  ✗ ${failed} worktree${failed > 1 ? "s" : ""} failed to boot — ↵ shows the log; retry with \`mise run git:workboot\` there.`,
      ),
    );
  }
  return out
    .slice(0, height - 1)
    .map((l) => l.slice(0, width + 200))
    .join("\r\n");
}

function paneFrame(
  title: string,
  lines: string[],
  width: number,
  height: number,
  footer: string,
  // Lines scrolled up from the bottom; 0 = pinned to the tail (follow mode).
  scroll = 0,
): string {
  const visible = height - 3;
  const start = Math.max(0, lines.length - visible - scroll);
  const body = lines
    .slice(start, start + visible)
    .map((l) => l.slice(0, width - 1));
  const pos =
    scroll > 0
      ? c.yellow(
          `↑${scroll} (${start + 1}-${start + body.length}/${lines.length})  `,
        )
      : "";
  return [`  ${c.bold(title)}  ${pos}${c.dim(footer)}`, "", ...body].join(
    "\r\n",
  );
}

// --- main --------------------------------------------------------------------

async function run() {
  if (process.env["usage_once"] === "true" || !process.stdout.isTTY) {
    const [rows] = await Promise.all([collect(), fetchPRs()]);
    for (const r of rows) {
      const pr = prByBranch.get(r.branch);
      console.log(
        `${r.isCurrent ? "@" : "+"} ${r.branch}\t${r.state}\t${r.url}${pr ? `\t${pr.state} ${pr.url}` : ""}`,
      );
    }
    return;
  }

  // PRs load in the background — first frame renders without the column
  // filled, then it appears on the next draw.
  void fetchPRs();
  const prPoll = setInterval(() => void fetchPRs(), 60_000);
  prPoll.unref?.();

  let rows: Row[] = await collect();
  let cursor = 0;
  const selected = new Set<string>();
  let view: "list" | "logs" | "remove" = "list";
  let logTarget: Row | null = null;
  let logPath: string | null = null;
  // Log view has two sources: the one-shot wt boot log (stack.log) and the
  // live pitchfork daemon logs of the worktree's services. Default depends on
  // the worktree state; `t` toggles.
  let logMode: "boot" | "services" = "boot";
  let serviceLines: string[] = [];
  let serviceFetching = false;
  // Scrollback for the log view: lines up from the tail. 0 follows new
  // output; any other value freezes the window while logs keep growing.
  let logScroll = 0;
  let logPrevCount = 0;

  const fetchServiceLogs = async (row: Row) => {
    if (serviceFetching) return;
    serviceFetching = true;
    try {
      // Daemon ids are `<worktree-dir-basename>/<service>` (e.g.
      // `gram.chore-enter-the-demo/server`). Logs live in pitchfork's SQLite
      // store, so the CLI is the only way in.
      const prefix = `${path.basename(row.path)}/`;
      const { stdout: listOut } = await execFile("pitchfork", ["list"]);
      const ids = strip(listOut)
        .split("\n")
        .map((l) => l.trim().split(/\s+/)[0])
        .filter((id) => id && id.startsWith(prefix));
      if (ids.length === 0) {
        serviceLines = [
          `No pitchfork daemons for ${prefix}* — stack is not running.`,
        ];
      } else {
        const { stdout } = await execFile(
          "pitchfork",
          ["logs", ...ids, "-n", "400"],
          {
            maxBuffer: 8 * 1024 * 1024,
          },
        );
        serviceLines = strip(stdout)
          .split("\n")
          .filter((l) => l.trim() !== "");
      }
    } catch (e) {
      serviceLines = [
        c.red(`pitchfork logs failed: ${(e as Error).message.split("\n")[0]}`),
      ];
    } finally {
      serviceFetching = false;
    }
    draw();
  };
  let removeLines: string[] = [];
  let removeTail = "";
  let removeRunning = false;
  let confirmRemove: string[] | null = null;
  let notice = "";

  const out = process.stdout;
  out.write("\x1b[?1049h\x1b[?25l"); // alt screen, hide cursor
  const cleanup = () => {
    out.write("\x1b[?1049l\x1b[?25h");
    process.stdin.setRawMode?.(false);
  };
  process.on("exit", cleanup);

  const draw = () => {
    try {
      drawUnsafe();
    } catch (e) {
      // A render bug must not take down the whole TUI (an uncaught throw here
      // exits mid-raw-mode, which presents as a frozen terminal).
      out.write(
        `\x1b[H\x1b[2J  render error: ${(e as Error).message}\r\n  press q to quit`,
      );
    }
  };

  const drawUnsafe = () => {
    const { columns: w, rows: h } = out;
    out.write("\x1b[H\x1b[2J");
    if (view === "list") {
      let frame = listFrame(rows, cursor, selected, w, h);
      if (confirmRemove) {
        const list = confirmRemove.join(", ");
        const dirty = confirmRemove.filter(
          (b) => rows.find((r) => r.branch === b)?.dirty,
        );
        frame += `\r\n\r\n  ${c.inverse(c.red(` remove ${list}? (runs pre-remove nuke) `))} ${c.bold("y")}/${c.bold("n")}`;
        if (dirty.length > 0) {
          frame += `\r\n  ${c.red(c.bold(`⚠ dirty: ${dirty.join(", ")} — uncommitted changes, removal runs with --force and DELETES them`))}`;
        }
        const locked = confirmRemove
          .map((b) => rows.find((r) => r.branch === b))
          .filter((r): r is Row => !!r && r.lockedBy !== "");
        if (locked.length > 0) {
          frame += `\r\n  ${c.yellow(`⚠ locked: ${locked.map((r) => `${r.branch} (by ${r.lockedBy})`).join(", ")} — will \`git worktree unlock\` before removing`)}`;
        }
      } else if (notice) {
        frame += `\r\n\r\n  ${c.yellow(notice)}`;
      }
      out.write(frame);
    } else if (view === "logs" && logTarget) {
      let lines: string[];
      if (logMode === "services") {
        lines =
          serviceLines.length > 0
            ? serviceLines
            : [
                serviceFetching
                  ? "Loading pitchfork logs…"
                  : "No service logs.",
              ];
      } else if (logPath) {
        lines = strip(tailFile(logPath, 512 * 1024))
          .split("\n")
          .filter((l) => l.trim() !== "");
      } else {
        lines = [
          "No boot log for this worktree.",
          "",
          `Worktree: ${logTarget.path}`,
          "",
          "wt writes stack.log when its post-start hook boots the stack, so",
          "there is none if the worktree was created outside wt (plain",
          "`git worktree add`, an agent scratch checkout) or was never booted.",
          "",
          "Boot it from that worktree with: mise run git:workboot",
          "",
          "Press t for this worktree's live service (pitchfork) logs.",
        ];
      }
      // The offset is measured from the tail, so appended lines would slide a
      // scrolled window toward newer content — grow the offset by the growth
      // to keep the visible window anchored. Follow mode (0) stays 0.
      if (logScroll > 0 && lines.length > logPrevCount) {
        logScroll += lines.length - logPrevCount;
      }
      logPrevCount = lines.length;
      logScroll = Math.min(logScroll, Math.max(0, lines.length - (h - 3)));
      out.write(
        paneFrame(
          `${logTarget.branch} — ${logMode === "services" ? "service logs" : "stack.log"}`,
          lines,
          w,
          h,
          `↑↓ scroll  g/G top/bottom  t ${logMode === "services" ? "boot log" : "service logs"}  esc back  q quit`,
          logScroll,
        ),
      );
    } else if (view === "remove") {
      // Summarized: wt's own step lines (◎/✓/✗) and errors, with the latest
      // raw hook-output line dimmed underneath as a liveness signal.
      const lines = [...removeLines];
      if (removeRunning && removeTail) lines.push("", c.dim(removeTail));
      out.write(
        paneFrame(
          "wt remove",
          lines,
          w,
          h,
          removeRunning ? "removing…" : "esc back  q quit",
        ),
      );
    }
  };

  const refresh = async () => {
    try {
      rows = await collect();
      if (cursor >= rows.length) cursor = Math.max(0, rows.length - 1);
      for (const b of selected)
        if (!rows.some((r) => r.branch === b)) selected.delete(b);
    } catch {
      /* keep last good frame; wt can be busy during removals */
    }
    // Keep the services pane live: refetch on the same cadence as the list.
    if (view === "logs" && logMode === "services" && logTarget) {
      void fetchServiceLogs(logTarget);
    }
    draw();
  };

  const poll = setInterval(refresh, 2000);

  const startRemove = async (branches: string[]) => {
    view = "remove";
    // --force only when a target is actually dirty (the confirm prompt warned
    // about exactly those); --foreground so the pre-remove nuke output streams
    // into the pane instead of detaching; never -D from the TUI — unmerged
    // branches should fail loudly.
    const force = branches.some((b) => rows.find((r) => r.branch === b)?.dirty);
    const args = [
      "remove",
      "--foreground",
      ...(force ? ["--force"] : []),
      ...branches,
    ];
    removeLines = [`$ wt ${args.join(" ")}`, ""];
    removeTail = "";
    removeRunning = true;
    // Unlock locked targets first — `wt remove` refuses locked worktrees. The
    // confirm prompt already named these and their lock owners.
    for (const b of branches) {
      const row = rows.find((r) => r.branch === b);
      if (!row?.lockedBy) continue;
      try {
        await execFile("git", ["worktree", "unlock", row.path]);
        removeLines.push(`◎ Unlocked ${b} (was locked by ${row.lockedBy})`);
      } catch (e) {
        removeLines.push(
          c.red(`✗ unlock ${b} failed: ${(e as Error).message.split("\n")[0]}`),
        );
      }
    }
    draw();
    // stdin ignored so nothing inside the removal (hook prompts, gum) can
    // block waiting on interactive input we'll never deliver.
    const child = spawn("wt", args, {
      env: process.env,
      stdio: ["ignore", "pipe", "pipe"],
    });
    // Summarize rather than stream: the nuke hook dumps pages of pitchfork
    // and compose noise per worktree. Keep only wt's own step lines (◎ start,
    // ✓ done, ✗/error failures) in the pane; everything else just refreshes
    // the dimmed "latest activity" tail line.
    const feed = (buf: Buffer) => {
      for (const line of strip(buf.toString()).split("\n")) {
        const t = line.trim();
        if (t === "") continue;
        if (/^[◎✓✗]/.test(t) || /\bERROR\b|error:|task failed/i.test(t)) {
          removeLines.push(
            t.startsWith("✗") || /ERROR|error:|task failed/i.test(t)
              ? c.red(t)
              : t,
          );
        } else {
          removeTail = t;
        }
      }
      draw();
    };
    child.stdout.on("data", feed);
    child.stderr.on("data", feed);
    // `exit`, not `close`: hook children (nuke → docker) and wt's detached
    // cleanup can inherit the stdout/stderr pipes and hold them open after wt
    // itself exits — `close` waits for those fds and never fires, leaving the
    // pane stuck on "removing…" forever.
    child.on("exit", (code) => {
      removeRunning = false;
      // Sever the pipes: lingering hook grandchildren (a stuck pitchfork IPC,
      // wt's detached rm -rf) inherited them and would keep feeding the pane
      // after wt itself is gone.
      child.stdout.destroy();
      child.stderr.destroy();
      removeLines.push(
        "",
        code === 0
          ? c.green("✓ done")
          : c.red(`✗ exited ${code} — press any key to go back`),
      );
      selected.clear();
      // Success needs no reading — return to the list on the next refresh.
      // Failures stay up until a keypress so the error is actually seen.
      if (code === 0) {
        setTimeout(() => {
          if (view === "remove" && !removeRunning) view = "list";
          void refresh();
        }, 1500);
      } else {
        void refresh();
      }
    });
  };

  process.stdin.setRawMode(true);
  process.stdin.resume();
  process.stdin.on("data", (buf: Buffer) => {
    const k = buf.toString();
    if (k === "\x03" || (k === "q" && !confirmRemove)) {
      clearInterval(poll);
      process.exit(0);
    }
    if (confirmRemove) {
      if (k === "y") {
        const branches = confirmRemove;
        confirmRemove = null;
        void startRemove(branches);
      } else {
        confirmRemove = null;
        draw();
      }
      return;
    }
    if (view !== "list") {
      if (view === "remove") {
        if (removeRunning) return; // let it finish
        // Finished pane: ANY key returns — esc-only proved too easy to miss
        // (and some terminals mangle a bare \x1b).
        view = "list";
        draw();
        return;
      }
      if (k === "\x1b" || k === "\x1b\x1b") {
        view = "list";
        logTarget = null;
        draw();
        return;
      }
      const page = Math.max(1, out.rows - 4);
      switch (k) {
        case "t":
          if (logTarget) {
            logMode = logMode === "boot" ? "services" : "boot";
            logScroll = 0;
            if (logMode === "services") void fetchServiceLogs(logTarget);
          }
          break;
        case "\x1b[A":
        case "k":
          logScroll += 1; // clamped against line count in draw
          break;
        case "\x1b[B":
        case "j":
          logScroll = Math.max(0, logScroll - 1);
          break;
        case "\x1b[5~": // PgUp
          logScroll += page;
          break;
        case "\x1b[6~": // PgDn
          logScroll = Math.max(0, logScroll - page);
          break;
        case "g":
          logScroll = Number.MAX_SAFE_INTEGER; // clamped to top in draw
          break;
        case "G":
          logScroll = 0; // back to following the tail
          break;
        default:
          return;
      }
      draw();
      return;
    }
    switch (k) {
      case "\x1b[A":
      case "k":
        cursor = Math.max(0, cursor - 1);
        break;
      case "\x1b[B":
      case "j":
        cursor = Math.min(rows.length - 1, cursor + 1);
        break;
      case " ": {
        const b = rows[cursor]?.branch;
        if (b) selected.has(b) ? selected.delete(b) : selected.add(b);
        break;
      }
      case "\r":
      case "l": {
        logTarget = rows[cursor] ?? null;
        if (logTarget) {
          view = "logs";
          logPath = null;
          // Boot log is the interesting one while a boot is live or failed;
          // for a stack that's simply up (or has no boot log), the live
          // service logs are what you want to see.
          logMode = ["booting", "seeding", "failed"].includes(logTarget.state)
            ? "boot"
            : "services";
          logScroll = 0;
          serviceLines = [];
          if (logMode === "services") void fetchServiceLogs(logTarget);
          void wtLogFile(logTarget.branch).then((p) => {
            logPath = p;
            draw();
          });
        }
        break;
      }
      case "d":
      case "r": {
        const branches =
          selected.size > 0
            ? [...selected]
            : rows[cursor]
              ? [rows[cursor].branch]
              : [];
        notice = "";
        const current = rows.find((r) => r.isCurrent)?.branch;
        if (branches.length > 0 && current && branches.includes(current)) {
          // Removing the worktree this TUI runs from would yank the floor out.
          notice =
            "refusing to remove the current worktree from inside it — deselect it first";
          break;
        }
        if (branches.length > 0) confirmRemove = branches;
        break;
      }
    }
    draw();
  });

  draw();
}

run();
