import { withFunctions } from "@speakeasy-api/functions/mcp";
import { server } from "./mcp.ts";

export default withFunctions(server, {
  // Describe environment variables required by the function here. These will be
  // available to fill in the Gram dashboard and hosted MCP servers. Example:
  // variables: {
  //   API_KEY: { description: "API key for authentication" },
  // },
});
