import type { Gram } from "@gram/client";

/**
 * Polls a deployment by ID until its status reaches a terminal state
 * ("completed" or "failed"). Returns the settled deployment.
 */
export async function waitForDeployment(client: Gram, deploymentId: string) {
  let deployment = await client.deployments.getById({ id: deploymentId });
  while (deployment.status !== "completed" && deployment.status !== "failed") {
    await new Promise((resolve) => setTimeout(resolve, 500));
    deployment = await client.deployments.getById({ id: deploymentId });
  }
  return deployment;
}
