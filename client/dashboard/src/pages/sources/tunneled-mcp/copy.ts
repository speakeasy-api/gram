// Single source for the resource-identifier explainer shown on both the
// create form and the settings section; each page appends its own tail.
export const RESOURCE_IDENTIFIER_EXPLAINER =
  "The identifier this server's own authorization server recognizes as its " +
  "audience (its RFC 9728 protected resource identifier). When set, user " +
  "credentials granted for this server are routed to it by exact match — " +
  "required once a gateway holds credentials for several servers. Gram " +
  "never connects to this address.";
