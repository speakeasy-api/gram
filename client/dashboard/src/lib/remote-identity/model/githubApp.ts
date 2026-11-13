export type GitHubAppDraft = {
  instanceUrl: string;
  appId: string;
  installationId: string;
  privateKey: string;
};

export function newGitHubAppDraft(): GitHubAppDraft {
  return {
    instanceUrl: "https://github.com",
    appId: "",
    installationId: "",
    privateKey: "",
  };
}

function validGitHubId(value: string): boolean {
  return (
    /^[1-9][0-9]{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n
  );
}

export function gitHubAppReady(draft: GitHubAppDraft): boolean {
  let validOrigin = false;
  try {
    const url = new URL(draft.instanceUrl);
    validOrigin =
      url.protocol === "https:" &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      (url.pathname === "/" || url.pathname === "") &&
      !draft.instanceUrl.includes("?") &&
      !draft.instanceUrl.includes("#");
  } catch {
    return false;
  }
  return (
    validOrigin &&
    validGitHubId(draft.appId) &&
    validGitHubId(draft.installationId) &&
    new TextEncoder().encode(draft.privateKey).length <= 32768 &&
    /-----BEGIN (RSA )?PRIVATE KEY-----/.test(draft.privateKey)
  );
}

export function isGitHubMcpUrl(raw: string | undefined): boolean {
  if (!raw) return false;
  try {
    const url = new URL(raw);
    return (
      url.protocol === "https:" &&
      !url.username &&
      !url.password &&
      !url.port &&
      ["api.githubcopilot.com", "github.com", "api.github.com"].includes(
        url.hostname,
      )
    );
  } catch {
    return false;
  }
}

export type GitHubAppBinding = {
  githubAppBound: boolean;
  githubAppEnabled: boolean;
  githubAppRequired: boolean;
};

/** Absent metadata is unknown, not evidence that no App is configured. */
export function gitHubAppBinding(
  metadata: Partial<GitHubAppBinding> | undefined,
): GitHubAppBinding | null {
  if (
    typeof metadata?.githubAppBound !== "boolean" ||
    typeof metadata.githubAppEnabled !== "boolean" ||
    typeof metadata.githubAppRequired !== "boolean"
  )
    return null;
  return {
    githubAppBound: metadata.githubAppBound,
    githubAppEnabled: metadata.githubAppEnabled,
    githubAppRequired: metadata.githubAppRequired,
  };
}
