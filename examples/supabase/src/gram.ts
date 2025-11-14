import { createClient } from "@supabase/supabase-js";

import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

const gram = new Gram({
  envSchema: { SUPABASE_URL: z.string(), SUPABASE_ANON_KEY: z.string() },
}).tool({
  name: "top_cities_by_property_sales",
  description: "Get top N UK cities by property sales and includes average and maximum property prices.",
  inputSchema: { count: z._default(z.number(), 10) },
  async execute(ctx, input) {
    const supabase = createClient(ctx.env.SUPABASE_URL, ctx.env.SUPABASE_ANON_KEY);

    const { data, error } = await supabase
      .from("land_registry_price_paid_uk")
      .select(
        `
        city::text,
        count(),
        price.avg(),
        price.max()
        `
      )
      .eq("record_status", "A")
      .not("city", "is", null)
      .neq("city", "")
      .order("count", {
        ascending: false,
      })
      .limit(input.count);
    if (error != null) {
      throw new Error(`Supabase query failed: ${error.message}`);
    }

    return ctx.json(data);
  },
});

export default gram;
