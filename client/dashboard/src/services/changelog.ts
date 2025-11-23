export interface ChangelogFeature {
  description: string;
  prNumber?: string;
  author?: string;
}

export interface ChangelogEntry {
  version: string;
  title: string;
  date: string;
  description: string;
  features: ChangelogFeature[];
  bugFixes: ChangelogFeature[];
  changelogUrl: string;
}

export interface ChangelogResponse {
  latestVersion: ChangelogEntry;
  changelogUrl: string;
}

// Latest Gram changelog entry
// Update this when a new version is released
const LATEST_GRAM_VERSION: ChangelogEntry = {
  version: "v0.12.1",
  title: "Source details page",
  date: "2025-12-02",
  description: "Introduces a new page for each source added to a project. The source page provides details on the source, which toolsets use it, and the ability to attach an environment to a source.",
  features: [
    {
      description: "Openrouter Automatic Key Refresh",
      prNumber: "964",
      author: "ryan-timothy-albert",
    },
    {
      description: "Gram Agents API",
      prNumber: "907",
      author: "ryan-timothy-albert",
    },
    {
      description: "Source Details Viewing Enhancement",
      prNumber: "932",
      author: "simplesagar",
    },
    {
      description: "OAuth Passthrough for Function Tools",
      prNumber: "929",
      author: "ryan-timothy-albert",
    },
    {
      description: "Server Instructions Addition to Frontend",
      prNumber: "944",
      author: "tgmendes",
    },
    {
      description: "Admin View for Creating OAuth Proxies",
      prNumber: "936",
      author: "ryan-timothy-albert",
    },
  ],
  bugFixes: [
    {
      description: "Context Cancellation Tracking Fix",
      prNumber: "967",
      author: "ryan-timothy-albert",
    },
    {
      description: "Switch Product Limits to Check Enabled Servers",
      prNumber: "963",
      author: "chase-crumbaugh",
    },
    {
      description: "Output Capture Improvement in Gram Functions",
      prNumber: "938",
      author: "disintegrator",
    },
    {
      description: "Unauthenticated Running of ClickHouse Migrations",
      prNumber: "935",
      author: "tgmendes",
    },
  ],
  changelogUrl: "https://www.speakeasy.com/changelog/gram",
};

/**
 * Fetches the latest changelog entry for Gram
 *
 * NOTE: Since there's no public API endpoint for the Speakeasy changelog,
 * this function returns statically defined data. To update the changelog:
 * 1. Visit https://www.speakeasy.com/changelog/gram
 * 2. Copy the latest version information
 * 3. Update the LATEST_GRAM_VERSION constant above
 *
 * @returns Changelog response with the latest version
 */
export async function fetchChangelog(): Promise<ChangelogResponse> {
  return {
    latestVersion: LATEST_GRAM_VERSION,
    changelogUrl: LATEST_GRAM_VERSION.changelogUrl,
  };
}
