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
  return (
    /^https:\/\/[^/]+\/?$/.test(draft.instanceUrl) &&
    /^[1-9][0-9]*$/.test(draft.appId) &&
    /^[1-9][0-9]*$/.test(draft.installationId) &&
    /-----BEGIN (RSA )?PRIVATE KEY-----/.test(draft.privateKey)
  );
}
