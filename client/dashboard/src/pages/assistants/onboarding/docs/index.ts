import { SLACK_DOCS } from "./slack";
import { CRON_DOCS } from "./cron";

export type IntegrationDoc = {
  slug: string;
  title: string;
  summary: string;
  body: string;
};

export const INTEGRATION_DOCS: Record<string, IntegrationDoc> = {
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

export function getIntegrationDoc(slug: string): IntegrationDoc | undefined {
  return INTEGRATION_DOCS[slug];
}
