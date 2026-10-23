import { ConsoleRow } from "./ConsoleRow";
import { customSetupInstructions } from "./CustomSetupInstructions";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { PlatformSetup } from "./setupInstructions";

export const INFERENCE_RESOURCE = "urn:gram:inference";

export function inferencePolicyGrants(
  projectId: string,
): AgentPolicyGrantForm[] {
  if (!projectId)
    throw new Error("Select a project before granting inference access.");
  return [
    {
      effect: "allow",
      scope: "inference:execute",
      selector: { resourceKind: "inference", resourceId: "*", projectId },
    },
  ];
}

export function inferenceSetupInstructions(
  projectId: string,
  completionEndpoint: string,
): PlatformSetup {
  const custom = customSetupInstructions(true);
  return {
    ...custom,
    steps: [
      ...custom.steps.filter((step) => step.phase !== "connect"),
      {
        id: "inference-exchange",
        short: "Exchange",
        title: "Exchange a workload JWT for inference",
        phase: "connect",
        body: ({ computed, issuerPicker }) => (
          <div className="space-y-4">
            <p className="text-sm">
              Your trusted external issuer signs the JWT. Its iss and sub must
              match the issuer and admission above, and its signature must
              verify against that issuer’s JWKS. Configure its aud to the
              Speakeasy issuer URL below (the token endpoint URL is also
              accepted). These are not the external issuer or JWKS URLs.
            </p>
            <ol className="border-border divide-border divide-y border">
              <ConsoleRow
                number={1}
                label="Speakeasy exchange configuration"
                fields={[
                  { label: "Token endpoint", value: computed.token_endpoint },
                  {
                    label: "Speakeasy issuer / external JWT audience",
                    value: computed.issuer_url,
                  },
                  {
                    label:
                      "Requested resource / returned access token audience",
                    value: INFERENCE_RESOURCE,
                  },
                  { label: "Target project", value: projectId },
                ]}
              />
              <ConsoleRow
                number={2}
                label="Inference endpoint"
                fields={[
                  { label: "POST chat completions", value: completionEndpoint },
                ]}
              />
            </ol>
            {issuerPicker}
            <p className="text-sm">
              POST application/x-www-form-urlencoded to the token endpoint with
              these fields. Obtain the assertion from your own issuer; do not
              paste live tokens into the dashboard.
            </p>
            <pre className="overflow-x-auto border p-3 text-xs">{`grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer\nassertion=<YOUR_EXTERNAL_JWT>\nresource=${INFERENCE_RESOURCE}\nproject_id=${projectId}`}</pre>
            <p className="text-sm">
              Send the returned access_token as Authorization: Bearer
              &lt;access_token&gt; to the inference endpoint, with a JSON model
              and messages body. No API key or MCP server is required. The
              session fixes the project. Do not send assistant or chat
              attribution headers. Exchange a fresh assertion when expires_in
              elapses; no refresh token is issued.
            </p>
            <p className="text-sm">
              A 403 can mean that the agent or its owner no longer has
              inference:execute for this project. Revoke the permission,
              withdraw the admission, or suspend the identity to stop access.
              Existing identities receive no inference permission automatically.
            </p>
          </div>
        ),
      },
    ],
  };
}
