import { serve } from "@hono/node-server";
import { app } from "./routes.js";

const port = parseInt(process.env.PORT || "35291", 10);

import { getDevUser, getDevOrganizations } from "./fixtures.js";

serve({ fetch: app.fetch, port }, (info) => {
  const user = getDevUser();
  const orgs = getDevOrganizations();
  console.log(`Mock Speakeasy IDP running on http://localhost:${info.port}`);
  console.log(
    `User: ${user.display_name} <${user.email}> (admin=${user.admin})`,
  );
  console.log(`Org:  ${orgs[0].name} (${orgs[0].slug})`);
  console.log("Endpoints:");
  console.log("  GET  /v1/speakeasy_provider/login");
  console.log("  POST /v1/speakeasy_provider/exchange");
  console.log("  GET  /v1/speakeasy_provider/validate");
  console.log("  POST /v1/speakeasy_provider/revoke");
  console.log("  POST /v1/speakeasy_provider/register");
});
