import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Markdown } from "@/elements/components/Markdown";
import { remarkSetupGuide } from "./setupGuideMarkdown";

afterEach(cleanup);

// Exercised through <Markdown /> rather than a bare unified pipeline: the
// unified toolchain is not a declared dependency of the dashboard, and this is
// the path the panel actually renders through.
function renderGuide(markdown: string, guideSlug = "box"): HTMLElement {
  const { container } = render(
    <Markdown extraRemarkPlugins={[[remarkSetupGuide, { guideSlug }]]}>
      {markdown}
    </Markdown>,
  );
  return container;
}

describe("remarkSetupGuide", () => {
  it("lifts a trailing custom id onto the heading so in-page links resolve", () => {
    const container = renderGuide(
      "### Add the server in Speakeasy {#add-server-in-speakeasy}\n",
    );

    const heading = container.querySelector("h3");
    expect(heading?.id).toBe("box--add-server-in-speakeasy");
    expect(heading?.textContent).toBe("Add the server in Speakeasy");
  });

  it("scopes the id to its guide, so stacked guides do not answer for each other", () => {
    // Both halves of every guide are templated from the same canonical
    // section, so this id is authored identically in all of them.
    const box = renderGuide("## Connect {#connect-speakeasy-credentials}\n");
    const asana = renderGuide(
      "## Connect {#connect-speakeasy-credentials}\n",
      "asana",
    );

    expect(box.querySelector("h2")?.id).toBe(
      "box--connect-speakeasy-credentials",
    );
    expect(asana.querySelector("h2")?.id).toBe(
      "asana--connect-speakeasy-credentials",
    );
  });

  it("handles a heading whose custom id follows inline formatting", () => {
    const container = renderGuide(
      "## Enable **Box AI** API {#enable-box-ai-api}\n",
    );

    const heading = container.querySelector("h2");
    expect(heading?.id).toBe("box--enable-box-ai-api");
    expect(heading?.textContent).toBe("Enable Box AI API");
    expect(container.querySelector("strong")?.textContent).toBe("Box AI");
  });

  it("leaves a heading without a custom id untouched", () => {
    const container = renderGuide("## Create credentials\n");

    const heading = container.querySelector("h2");
    expect(heading?.hasAttribute("id")).toBe(false);
    expect(heading?.textContent).toBe("Create credentials");
  });

  it("does not touch a custom id inside a fenced code block", () => {
    const container = renderGuide("```\n## Heading {#not-a-real-id}\n```\n");

    expect(container.querySelector("h2")).toBeNull();
    expect(container.textContent).toContain("## Heading {#not-a-real-id}");
  });

  it("drops the authoring notes guides carry as HTML comments", () => {
    const container = renderGuide(
      "Click **Enable**.\n\n<!-- screenshot: the BigQuery API page showing Enable -->\n\nThen continue.\n",
    );

    expect(container.textContent).not.toContain("screenshot:");
    expect(container.textContent).not.toContain("<!--");
    expect(container.textContent).toContain("Click Enable.");
    expect(container.textContent).toContain("Then continue.");
  });

  it("drops a comment that trails text on the same line", () => {
    const container = renderGuide("Click Enable. <!-- note to self -->\n");

    expect(container.textContent).not.toContain("note to self");
    expect(container.textContent).toContain("Click Enable.");
  });

  it("keeps an HTML comment that is part of a code sample", () => {
    const container = renderGuide("```html\n<!-- keep me -->\n```\n");

    expect(container.textContent).toContain("<!-- keep me -->");
  });

  it("renders GFM tables, so the extra plugin does not displace gfm", () => {
    const container = renderGuide("| a | b |\n| - | - |\n| 1 | 2 |\n");

    expect(container.querySelector("table")).not.toBeNull();
  });
});
