import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

const gram = new Gram().tool({
  name: "greet",
  description: "Greet someone special",
  inputSchema: { name: z.string() },
  async execute(ctx, input) {
    return ctx.json({ message: `Hello, ${input.name}!` });
  },
});

export default gram;
