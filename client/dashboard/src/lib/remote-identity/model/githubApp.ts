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
    /^[1-9][0-9]*$/.test(draft.appId) &&
    /^[1-9][0-9]*$/.test(draft.installationId) &&
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
      ["api.githubcopilot.com", "github.com", "api.github.com"].includes(
        url.hostname,
      )
    );
  } catch {
    return false;
  }
}
