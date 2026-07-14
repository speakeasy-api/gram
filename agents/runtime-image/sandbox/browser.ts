import { Readability } from "@mozilla/readability";
import { JSDOM } from "jsdom";
import {
  chromium,
  type Browser,
  type BrowserContext,
  type Page,
} from "playwright-core";
import TurndownService from "turndown";
// @ts-expect-error no published types
import { gfm } from "turndown-plugin-gfm";

const CDP_ENDPOINT = "http://127.0.0.1:9222";

// Lightpanda reuses frame IDs across contexts, so a single CDP connection can
// only ever host one BrowserContext — a second newContext() on the same
// connection corrupts Playwright's target registry in a way that poisons the
// whole handle. Every Browser handle is therefore a dedicated connection that
// refuses a second context up front, and callers must close it when done.
export async function getBrowser(): Promise<Browser> {
  const browser = await chromium.connectOverCDP(CDP_ENDPOINT);
  const newContext = browser.newContext.bind(browser);
  let hasContext = false;
  browser.newContext = async (options) => {
    if (hasContext) {
      throw new Error(
        "this browser handle already hosts a BrowserContext and the sandbox browser supports exactly one per handle; call getBrowser() again (or use withContext) for the next context",
      );
    }
    hasContext = true;
    return await newContext(options);
  };
  return browser;
}

export async function withContext<T>(
  fn: (ctx: BrowserContext) => Promise<T>,
): Promise<T> {
  const browser = await getBrowser();
  try {
    const ctx = await browser.newContext();
    try {
      return await fn(ctx);
    } finally {
      await ctx.close();
    }
  } finally {
    await browser.close();
  }
}

export type MarkdownOptions = {
  readable?: boolean;
  url?: string;
};

export type MarkdownResult = {
  title?: string;
  byline?: string;
  markdown: string;
};

export async function markdown(
  source: Page | string,
  opts: MarkdownOptions = {},
): Promise<MarkdownResult> {
  const [html, url] =
    typeof source === "string"
      ? [source, opts.url]
      : [await source.content(), opts.url ?? source.url()];

  const dom = new JSDOM(html, url ? { url } : undefined);
  let sourceHtml = html;
  let title: string | undefined;
  let byline: string | undefined;

  if (opts.readable !== false) {
    const article = new Readability(dom.window.document).parse();
    if (article?.content) {
      sourceHtml = article.content;
      title = article.title ?? undefined;
      byline = article.byline ?? undefined;
    }
  }

  const td = new TurndownService({
    headingStyle: "atx",
    codeBlockStyle: "fenced",
    bulletListMarker: "-",
  });
  td.use(gfm);

  return { title, byline, markdown: td.turndown(sourceHtml) };
}

export const browser = {
  getBrowser,
  withContext,
  markdown,
};
