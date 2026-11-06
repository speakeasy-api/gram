import assert from "node:assert/strict";
import test from "node:test";
import { selectCacheSource } from "./vitest-cache-source.mjs";

const sha = "a".repeat(40);
const repository = "owner/project";
const prefix = "vitest-module-cache-";
const base = `/repos/${repository}`;

function fixture({ runId = 101, jobId = 201, artifactId = 301 } = {}) {
  const run = {
    id: runId,
    workflow_id: 11,
    path: ".github/workflows/pr.yaml",
    event: "merge_group",
    status: "completed",
    conclusion: "success",
    head_sha: sha,
    head_branch: "gh-readonly-queue/main/pr-42-example",
    run_attempt: 2,
    repository: { id: 7, full_name: repository },
    head_repository: { id: 7, full_name: repository },
  };
  const job = {
    id: jobId,
    run_id: runId,
    run_attempt: 2,
    head_sha: sha,
    name: "client-test",
    status: "completed",
    conclusion: "success",
    steps: ["Test", "Test admin", "Test registry editor in Chrome"].map(
      (name) => ({ name, status: "completed", conclusion: "success" }),
    ),
  };
  const artifact = {
    id: artifactId,
    name: `${prefix}${sha}-attempt-2`,
    expired: false,
    expires_at: "2100-01-01T00:00:00Z",
    workflow_run: {
      id: runId,
      head_sha: sha,
      repository_id: 7,
      head_repository_id: 7,
    },
  };
  const api = async (path) => {
    const url = new URL(path, "https://api.github.com");
    if (url.pathname === base) return { id: 7, full_name: repository };
    if (url.pathname === `${base}/actions/workflows/pr.yaml`)
      return { id: 11, path: ".github/workflows/pr.yaml" };
    if (url.pathname.endsWith("/actions/workflows/pr.yaml/runs"))
      return { workflow_runs: [run] };
    if (url.pathname.endsWith(`/runs/${runId}/attempts/2/jobs`))
      return { jobs: [job] };
    if (url.pathname.endsWith(`/runs/${runId}/artifacts`))
      return { artifacts: [artifact] };
    throw new Error(`Unexpected API request: ${path}`);
  };
  return { run, job, artifact, api };
}

const select = (api) => selectCacheSource({ api, repository, sha, prefix });

for (const [reason, change] of [
  [
    "wrong SHA",
    (f) => {
      f.run.head_sha = "b".repeat(40);
    },
  ],
  [
    "wrong event",
    (f) => {
      f.run.event = "pull_request";
    },
  ],
  [
    "wrong workflow ID",
    (f) => {
      f.run.workflow_id = 12;
    },
  ],
  [
    "wrong workflow path",
    (f) => {
      f.run.path = ".github/workflows/other.yaml";
    },
  ],
  [
    "wrong queue ref",
    (f) => {
      f.run.head_branch = "gh-readonly-queue/other/pr-42-example";
    },
  ],
  [
    "missing queue ref",
    (f) => {
      delete f.run.head_branch;
    },
  ],
  [
    "wrong repository",
    (f) => {
      f.run.repository.id = 8;
    },
  ],
  [
    "wrong repository name",
    (f) => {
      f.run.repository.full_name = "other/project";
    },
  ],
  [
    "fork head repository",
    (f) => {
      f.run.head_repository.id = 8;
    },
  ],
  [
    "fork head repository name",
    (f) => {
      f.run.head_repository.full_name = "other/project";
    },
  ],
  [
    "incomplete run",
    (f) => {
      f.run.status = "in_progress";
    },
  ],
  [
    "failed run",
    (f) => {
      f.run.conclusion = "failure";
    },
  ],
  [
    "invalid attempt",
    (f) => {
      f.run.run_attempt = 0;
    },
  ],
  [
    "wrong job",
    (f) => {
      f.job.name = "other-test";
    },
  ],
  [
    "failed job",
    (f) => {
      f.job.conclusion = "failure";
    },
  ],
  [
    "incomplete job",
    (f) => {
      f.job.status = "in_progress";
    },
  ],
  [
    "wrong job run",
    (f) => {
      f.job.run_id = 102;
    },
  ],
  [
    "wrong job attempt",
    (f) => {
      f.job.run_attempt = 1;
    },
  ],
  [
    "wrong job SHA",
    (f) => {
      f.job.head_sha = "b".repeat(40);
    },
  ],
  [
    "missing test step",
    (f) => {
      f.job.steps.pop();
    },
  ],
  [
    "missing steps",
    (f) => {
      delete f.job.steps;
    },
  ],
  [
    "duplicate test step",
    (f) => {
      f.job.steps.push(f.job.steps[0]);
    },
  ],
  [
    "expired artifact",
    (f) => {
      f.artifact.expired = true;
    },
  ],
  [
    "past expiration date",
    (f) => {
      f.artifact.expires_at = "2000-01-01T00:00:00Z";
    },
  ],
  [
    "missing expiration date",
    (f) => {
      delete f.artifact.expires_at;
    },
  ],
  [
    "wrong artifact attempt",
    (f) => {
      f.artifact.name = `${prefix}${sha}-attempt-1`;
    },
  ],
  [
    "wrong compatibility prefix",
    (f) => {
      f.artifact.name = `different-config-${sha}-attempt-2`;
    },
  ],
  [
    "wrong provenance run",
    (f) => {
      f.artifact.workflow_run.id = 102;
    },
  ],
  [
    "wrong provenance SHA",
    (f) => {
      f.artifact.workflow_run.head_sha = "b".repeat(40);
    },
  ],
  [
    "wrong provenance repository",
    (f) => {
      f.artifact.workflow_run.repository_id = 8;
    },
  ],
  [
    "wrong provenance head repository",
    (f) => {
      f.artifact.workflow_run.head_repository_id = 8;
    },
  ],
  [
    "missing provenance",
    (f) => {
      delete f.artifact.workflow_run;
    },
  ],
]) {
  test(`rejects ${reason}`, async () => {
    const f = fixture();
    change(f);
    assert.equal(await select(f.api), null);
  });
}

for (const [name, conclusion] of [
  ["Test", "failure"],
  ["Test admin", "failure"],
  ["Test registry editor in Chrome", "failure"],
  ["Test", "skipped"],
  ["Test", "cancelled"],
]) {
  test(`rejects ${conclusion} step: ${name}`, async () => {
    const f = fixture();
    f.job.steps.find((step) => step.name === name).conclusion = conclusion;
    assert.equal(await select(f.api), null);
  });
}

for (const key of ["jobs", "artifacts"]) {
  test(`rejects ambiguous ${key}`, async () => {
    const f = fixture();
    assert.equal(
      await select(async (path) => {
        const response = await f.api(path);
        return response[key]
          ? { [key]: [...response[key], ...response[key]] }
          : response;
      }),
      null,
    );
  });
}

test("paginates runs, attempt jobs, and artifacts", async () => {
  const f = fixture();
  const selected = await select(async (path) => {
    const response = await f.api(path);
    const key = ["workflow_runs", "jobs", "artifacts"].find(
      (key) => key in response,
    );
    if (!key) return response;
    const page = new URL(path, "https://api.github.com").searchParams.get(
      "page",
    );
    if (page === "1") return { [key]: Array.from({ length: 100 }, () => ({})) };
    return response;
  });
  assert.equal(selected.artifactId, 301);
});

test("no matching runs is a normal cache miss", async () => {
  const f = fixture();
  assert.equal(
    await select(async (path) =>
      path.includes("/pr.yaml/runs") ? { workflow_runs: [] } : f.api(path),
    ),
    null,
  );
});

test("API failure propagates instead of becoming a cache miss", async () => {
  await assert.rejects(
    select(async () => {
      throw new Error("API unavailable");
    }),
    /API unavailable/,
  );
});

test("selects a trusted same-SHA merge queue artifact from the successful attempt", async () => {
  const f = fixture();
  assert.deepEqual(await select(f.api), {
    runId: 101,
    jobId: 201,
    artifactId: 301,
    sha,
  });
});

test("selects a later eligible run when the first has no artifact", async () => {
  const first = fixture();
  const later = fixture({ runId: 102, jobId: 202, artifactId: 302 });
  const selected = await select(async (path) => {
    const { pathname } = new URL(path, "https://api.github.com");
    if (pathname.endsWith("/actions/workflows/pr.yaml/runs"))
      return { workflow_runs: [first.run, later.run] };
    if (pathname.endsWith("/runs/101/artifacts")) return { artifacts: [] };
    if (pathname.endsWith("/runs/102/attempts/2/jobs"))
      return {
        jobs: [{ ...later.job, id: 999, name: "other-job" }, later.job],
      };
    if (pathname.endsWith("/runs/102/artifacts"))
      return {
        artifacts: [
          { ...later.artifact, id: 999, name: "other-artifact" },
          later.artifact,
        ],
      };
    return first.api(path);
  });
  assert.deepEqual(selected, {
    runId: 102,
    jobId: 202,
    artifactId: 302,
    sha,
  });
});
