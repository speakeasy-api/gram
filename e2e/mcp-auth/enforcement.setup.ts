import { test } from "@playwright/test";
import { Dashboard } from "./dashboard.ts";

test("turn on upstream auth enforcement and the organization gateway", async ({
  page,
}) => {
  const dashboard = new Dashboard(page);
  await dashboard.login();
  await dashboard.enforce();
  await dashboard.organizationGateway();
});
