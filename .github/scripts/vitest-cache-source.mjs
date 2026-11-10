import { appendFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

const workflowPath = ".github/workflows/pr.yaml";
const requiredSteps = ["Test", "Test admin", "Test registry editor in Chrome"];
const successful = (value) =>
  value.status === "completed" && value.conclusion === "success";
const numericId = (value) => Number.isSafeInteger(value) && value > 0;

async function list(api, path, key) {
  const items = [];
  for (let page = 1; ; page++) {
    const result = await api(
      `${path}${path.includes("?") ? "&" : "?"}per_page=100&page=${page}`,
    );
    if (!Array.isArray(result[key]))
      throw new Error("Invalid GitHub list response");
    items.push(...result[key]);
    if (result[key].length < 100) return items;
  }
}

export async function selectCacheSource({
  api,
  repository,
  sha,
  prefix,
  now = Date.now(),
}) {
  const base = `/repos/${repository}`;
  const repo = await api(base);
  const workflow = await api(`${base}/actions/workflows/pr.yaml`);
  if (
    !numericId(repo.id) ||
    repo.full_name !== repository ||
    !numericId(workflow.id) ||
    workflow.path !== workflowPath
  )
    return null;
  const runs = await list(
    api,
    `${base}/actions/workflows/pr.yaml/runs?event=merge_group&status=success&head_sha=${encodeURIComponent(sha)}`,
    "workflow_runs",
  );
  for (const run of runs) {
    if (
      !numericId(run.id) ||
      !numericId(run.run_attempt) ||
      run.workflow_id !== workflow.id ||
      run.path !== workflowPath ||
      run.event !== "merge_group" ||
      !successful(run) ||
      run.head_sha !== sha ||
      !run.head_branch?.startsWith("gh-readonly-queue/main/") ||
      run.repository?.id !== repo.id ||
      run.repository?.full_name !== repository ||
      run.head_repository?.id !== repo.id ||
      run.head_repository?.full_name !== repository
    )
      continue;
    const jobs = await list(
      api,
      `${base}/actions/runs/${run.id}/attempts/${run.run_attempt}/jobs`,
      "jobs",
    );
    const matchingJobs = jobs.filter((job) => job.name === "client-test");
    if (matchingJobs.length !== 1) continue;
    const job = matchingJobs[0];
    if (
      !numericId(job.id) ||
      job.run_id !== run.id ||
      job.run_attempt !== run.run_attempt ||
      job.head_sha !== sha ||
      !successful(job) ||
      !requiredSteps.every((name) => {
        const steps = job.steps?.filter((step) => step.name === name) ?? [];
        return steps.length === 1 && successful(steps[0]);
      })
    )
      continue;
    const artifacts = await list(
      api,
      `${base}/actions/runs/${run.id}/artifacts`,
      "artifacts",
    );
    const matches = artifacts.filter(
      (artifact) =>
        artifact.name === `${prefix}${sha}-attempt-${run.run_attempt}`,
    );
    if (matches.length !== 1) continue;
    const artifact = matches[0];
    const provenance = artifact.workflow_run;
    if (
      !numericId(artifact.id) ||
      artifact.expired !== false ||
      !(Date.parse(artifact.expires_at) > now) ||
      provenance?.id !== run.id ||
      provenance?.head_sha !== sha ||
      provenance?.repository_id !== repo.id ||
      provenance?.head_repository_id !== repo.id
    )
      continue;
    return { runId: run.id, jobId: job.id, artifactId: artifact.id, sha };
  }
  return null;
}

async function main() {
  const {
    GITHUB_REPOSITORY: repository,
    GITHUB_SHA: sha,
    GITHUB_API_URL: apiUrl,
    GH_TOKEN: token,
    CACHE_PREFIX: prefix,
    GITHUB_OUTPUT: output,
  } = process.env;
  if (
    !repository ||
    !/^[\w.-]+\/[\w.-]+$/.test(repository) ||
    !/^[a-f0-9]{40}$/.test(sha ?? "") ||
    !apiUrl ||
    !token ||
    !prefix ||
    !output
  )
    throw new Error("Missing or invalid cache selector configuration");
  const api = async (path) => {
    const response = await fetch(`${apiUrl.replace(/\/$/, "")}${path}`, {
      method: "GET",
      redirect: "error",
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
      },
    });
    if (!response.ok)
      throw new Error(`GitHub API request failed (${response.status})`);
    return response.json();
  };
  const selected = await selectCacheSource({ api, repository, sha, prefix });
  if (!selected) {
    console.log(`No trusted cache source for ${sha}`);
    return;
  }
  await appendFile(
    output,
    `run-id=${selected.runId}\nartifact-id=${selected.artifactId}\n`,
  );
  console.log(
    `Trusted cache source: run ${selected.runId}, job ${selected.jobId}, SHA ${sha}`,
  );
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  main().catch(() => {
    console.error("Cache source selection failed");
    process.exitCode = 1;
  });
}
