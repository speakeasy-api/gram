import { z } from "zod";

import { ROUTING_METHODS } from "./customDomainModel";

const searchSchema = z.object({
  // How the latest health check saw the domain routed. Sent to the server as
  // `dns_routing_method`; a value this build does not know filters nothing.
  routing: z.enum(ROUTING_METHODS).optional().catch(undefined),
});

export type CustomDomainsSearch = z.infer<typeof searchSchema>;

export function customDomainsSearch(
  search: Record<string, unknown>,
): CustomDomainsSearch {
  return searchSchema.parse(search);
}
