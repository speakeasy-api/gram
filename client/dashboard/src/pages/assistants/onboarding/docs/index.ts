import { KIT_SLACK_DOCS, SLACK_DOCS } from "./slack";
import { CRON_DOCS } from "./cron";

export type IntegrationDoc = {
  slug: string;
  title: string;
  summary: string;
  body: string;
};

const INTEGRATION_DOCS: Record<string, IntegrationDoc> = {
  slack: SLACK_DOCS,
  cron: CRON_DOCS,
};

export function listIntegrationDocs(): Pick<
  IntegrationDoc,
  "slug" | "title" | "summary"
>[] {
  return Object.values(INTEGRATION_DOCS).map(({ slug, title, summary }) => ({
    slug,
    title,
    summary,
  }));
}

export function getIntegrationDoc(
  slug: string,
  usesProjectMCPAccess = false,
): IntegrationDoc | undefined {
  if (slug === "slack" && usesProjectMCPAccess) return KIT_SLACK_DOCS;
  return INTEGRATION_DOCS[slug];
}
