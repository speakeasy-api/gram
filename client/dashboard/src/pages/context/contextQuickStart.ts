export function buildCorpusQuickStart(
  corpusRemoteURL: string,
  projectSlug: string | null,
): string {
  const directory = projectSlug ?? "context-repo";

  return [
    `git clone ${corpusRemoteURL} ${directory}`,
    `cd ${directory}`,
    "",
    "# make your changes",
    "git add .",
    'git commit -m "Update context"',
    "git push origin HEAD",
  ].join("\n");
}
