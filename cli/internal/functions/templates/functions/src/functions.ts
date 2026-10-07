import { Functions } from "@speakeasy-api/functions";
import * as z from "zod/mini";

// To learn more about functions, check out our documentation at:
// https://www.speakeasy.com/docs/ai-control-plane/mcp-gateway/building-servers/functions/functions-framework
const functions = new Functions().tool({
  name: "greet",
  description: "Greet someone special",
  inputSchema: { name: z.string() },
  async execute(ctx, input) {
    return ctx.json({ message: `Hello, ${input.name}!` });
  },
});

export default functions;
