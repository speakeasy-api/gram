import { describe, expect, it } from "vitest";
import {
  excerptFromReadDoc,
  findDocsExcerpts,
  findReadDocs,
  httpsURL,
  previewText,
} from "./search-docs-result";

const excerpt = {
  uri: "gram://platform-mcp/setup/acme/provider_setup",
  title: "Acme — provider setup",
  heading: "Generate OAuth credentials",
  excerpt: "Register an OAuth app and copy the client id.",
  source: "mcp-setup-docs/go@v0.3.0",
  observed_at: "2026-07-27",
  revalidate_by: "2026-10-25",
  links: ["https://developers.acme.test/oauth"],
};

describe("findDocsExcerpts", () => {
  it("finds citations in a direct tool result", () => {
    expect(
      findDocsExcerpts({ query: "acme oauth", excerpts: [excerpt] }),
    ).toEqual([excerpt]);
  });

  // The runtime calls tools inside a compose script, so citations arrive nested
  // under names the script chose rather than at a known key.
  it("finds citations nested in a compose result", () => {
    const composed = {
      github: { excerpts: [] },
      acme: { result: { query: "acme", excerpts: [excerpt] } },
    };
    expect(findDocsExcerpts(composed)).toEqual([excerpt]);
  });

  it("finds citations in a JSON payload returned as a string", () => {
    const composed = { acme: JSON.stringify({ excerpts: [excerpt] }) };
    expect(findDocsExcerpts(composed)).toEqual([excerpt]);
  });

  it("returns each cited passage once", () => {
    const composed = { a: { excerpts: [excerpt] }, b: { excerpts: [excerpt] } };
    expect(findDocsExcerpts(composed)).toHaveLength(1);
  });

  // A guide_unavailable answer carries no excerpts, and another tool's payload
  // is not a citation. Both fall through to the generic result view rather than
  // showing an empty strip that reads like a sourceless answer.
  it("finds nothing in results that carry no citations", () => {
    expect(
      findDocsExcerpts({ excerpts: [], code: "guide_unavailable" }),
    ).toEqual([]);
    expect(
      findDocsExcerpts({ docs: [{ path: "/docs/guides/github" }] }),
    ).toEqual([]);
    expect(
      findDocsExcerpts("read gram://platform-mcp/setup/acme/provider_setup"),
    ).toEqual([]);
    expect(findDocsExcerpts(undefined)).toEqual([]);
  });

  // A read carries the whole guide; a search hit carries an excerpt of one.
  // Telling them apart is what lets a card appear only once the assistant has
  // settled on a guide, instead of once per search candidate.
  it("tells a read guide apart from a search hit", () => {
    const composed = {
      search: { excerpts: [excerpt] },
      read: {
        uri: "gram://platform-mcp/setup/github/provider_setup",
        title: "GitHub — provider setup",
        text: "# GitHub\n\nRegister an OAuth app.",
      },
    };
    expect(findReadDocs(composed).map((d) => d.uri)).toEqual([
      "gram://platform-mcp/setup/github/provider_setup",
    ]);
    expect(findDocsExcerpts(composed)).toEqual([excerpt]);
    expect(findReadDocs({ excerpts: [excerpt] })).toEqual([]);
    // A withheld guide answers with a code and no text, so nothing was read.
    expect(
      findReadDocs({
        uri: "gram://platform-mcp/setup/github/provider_setup",
        code: "guide_unavailable",
      }),
    ).toEqual([]);
  });

  // A URI outside the reviewed setup namespace is some other server's resource.
  it("ignores excerpt-shaped objects from outside the corpus", () => {
    expect(
      findDocsExcerpts({
        excerpts: [{ ...excerpt, uri: "https://example.test/doc" }],
      }),
    ).toEqual([]);
  });
});

// The excerpt is markdown and the strip clamps it to three lines, so a preview
// that keeps its syntax spends those lines on asterisks rather than on the
// sentence a reader is trying to skim.
describe("previewText", () => {
  it("reads as prose rather than markup", () => {
    expect(
      previewText(
        "From the server's **Overview**, open **Settings**.\n\n1. Set `Client Type` to Manual.\n2. See [the guide](https://example.test/x).",
      ),
    ).toBe(
      "From the server's Overview, open Settings. 1. Set Client Type to Manual. 2. See the guide.",
    );
  });

  it("drops headings, bullets, and fenced code", () => {
    expect(previewText("## Set up\n\n- first step\n- second step")).toBe(
      "Set up first step second step",
    );
    expect(
      previewText("Run this:\n\n```sh\nnpm install\n```\n\nThen retry."),
    ).toBe("Run this: Then retry.");
  });
});

// A guide opened by URI has no search behind it, so its card is built from the
// citation header the guide itself carries.
describe("excerptFromReadDoc", () => {
  const text = [
    "# GitHub — provider setup",
    "",
    "- Owner: Speakeasy mcp-setup-docs maintainers",
    "- Source: mcp-setup-docs/go@v0.3.0 (pinned reviewed export; never fetched at request time)",
    "- Observed: 2026-07-27",
    "- Revalidate by: 2026-10-25",
    "- Speakeasy docs: https://www.speakeasy.com/docs/ai-control-plane/guides/github",
    "- Canonical sources:",
    "  - https://docs.github.com/en/apps",
    "",
    "---",
    "",
    "## Generate the OAuth credentials",
    "",
    "Register an OAuth app and copy the client id.",
  ].join("\n");

  it("recovers the citation from the guide's own header", () => {
    const built = excerptFromReadDoc({
      uri: "gram://platform-mcp/setup/github/provider_setup",
      title: "GitHub — provider setup",
      text,
    });

    expect(built).toMatchObject({
      uri: "gram://platform-mcp/setup/github/provider_setup",
      title: "GitHub — provider setup",
      heading: "Generate the OAuth credentials",
      source: "mcp-setup-docs/go@v0.3.0",
      observed_at: "2026-07-27",
      revalidate_by: "2026-10-25",
      docs_url: "https://www.speakeasy.com/docs/ai-control-plane/guides/github",
      links: ["https://docs.github.com/en/apps"],
    });
    expect(built.excerpt).toContain("Register an OAuth app");
    // The header is the citation, not the answer — it must not become the
    // preview a reader skims.
    expect(built.excerpt).not.toContain("- Owner:");
  });
});

// A tool result is assembled by a model-authored compose script, so a URL
// arriving here is not guaranteed to be the https link the corpus emitted.
describe("citation link safety", () => {
  it("drops any link that is not an absolute https URL", () => {
    expect(httpsURL("https://docs.github.com/en/apps")).toBe(
      "https://docs.github.com/en/apps",
    );
    for (const hostile of [
      "javascript:alert(1)",
      "data:text/html,<script>alert(1)</script>",
      "http://docs.github.com",
      "/docs/ai-control-plane",
      "not a url",
      "",
      undefined,
    ]) {
      expect(httpsURL(hostile)).toBeUndefined();
    }
  });

  it("only accepts absolute https links", () => {
    const built = excerptFromReadDoc({
      uri: "gram://platform-mcp/setup/github/provider_setup",
      text: [
        "# GitHub",
        "",
        "- Canonical sources:",
        "  - https://docs.github.com/en/apps",
        "",
        "---",
        "",
        "Steps.",
      ].join("\n"),
    });
    expect(built.links).toEqual(["https://docs.github.com/en/apps"]);
  });
});
