export function collectionInstallDisabledReason({
  isLoading,
  installableServerCount,
  projectCount,
}: {
  isLoading: boolean;
  installableServerCount: number;
  projectCount: number;
}): string | null {
  if (isLoading) {
    return "Checking whether this collection can be installed";
  }
  if (installableServerCount === 0) {
    return "This collection has no servers with active endpoints to install";
  }
  if (projectCount === 0) {
    return "Create a project before installing this collection";
  }
  return null;
}
