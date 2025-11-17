import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

// To learn more about Gram Functions, check out our documentation at:
// https://www.speakeasy.com/docs/gram/gram-functions/functions-framework
const gram = new Gram().tool({
  name: "greet",
  description: "Greet someone special",
  inputSchema: { name: z.string() },
  async execute(ctx, input) {
    return ctx.json({ message: `Hello, ${input.name}!` });
  },
});

export default gram;
