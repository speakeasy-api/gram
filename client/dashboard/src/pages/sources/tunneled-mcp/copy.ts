// Single source for the resource-identifier explainer shown on both the
// create form and the settings section; each page appends its own tail.
export const RESOURCE_IDENTIFIER_EXPLAINER =
  "The address this server's own sign-in system expects in its access " +
  "tokens. Set it when the server has its own OAuth, so each user's " +
  "credentials reach the right server. Speakeasy never connects to this " +
  "address.";

// Protocol detail for the "Technical details" disclosure under the explainer.
export const RESOURCE_IDENTIFIER_TECHNICAL =
  "This is the server's RFC 9728 protected resource identifier. Speakeasy " +
  "routes user credentials by resource match, which a gateway needs once it " +
  "holds credentials for several servers. It is also the audience (aud) of " +
  "signed caller assertions. When blank, assertions use " +
  "tunneled-mcp-server:<ID>.";
