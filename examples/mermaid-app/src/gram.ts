import { Gram } from "@gram-ai/functions";
import { z } from "zod/mini";
import { HTML } from "./inlined.ts";

const UI_URI = "ui://mermaid";

// Shim: the functions runner (gram-start.mjs) dispatches resource reads by
// looking for a `handleResources` method on the default export, but
// @gram-ai/functions@0.14.x's `Gram` class only exposes `handleResourceRead`.
// Adapt the signature until the framework exposes `handleResources` natively.
type GramWithResources<G> = G & {
  handleResources(req: { uri: string; input?: unknown }): Promise<Response>;
};

function withResourceHandler<G extends Gram<any, any>>(
  g: G,
): GramWithResources<G> {
  return Object.assign(g, {
    handleResources(req: { uri: string }) {
      return g.handleResourceRead({ uri: req.uri });
    },
  });
}

const gram = new Gram()
  .experimental_uiResource({
    name: "mermaid",
    uri: UI_URI,
    title: "Mermaid diagram",
    description:
      "An interactive Mermaid diagram widget rendered by a React app.",
    content: HTML,
  })
  .tool({
    name: "render_diagram",
    description:
      "Render a Mermaid diagram (flowchart, sequence, class, gantt, state, ER, journey, etc.) as an interactive widget. The `diagram` argument is the raw Mermaid source.",
    inputSchema: {
      diagram: z.string(),
      title: z.optional(z.string()),
    },
    annotations: {
      title: "Render Mermaid diagram",
      readOnlyHint: true,
      openWorldHint: false,
    },
    meta: {
      "ui/resourceUri": UI_URI,
    },
    async execute(ctx, input) {
      return ctx.json({
        diagram: input.diagram,
        title: input.title,
      });
    },
  });

export default withResourceHandler(gram);
