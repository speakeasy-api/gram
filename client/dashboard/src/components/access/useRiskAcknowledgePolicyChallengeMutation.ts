import { useMutation, type UseMutationResult } from "@tanstack/react-query";

// TODO(regen): available after SDK regen for risk.acknowledgePolicyChallenge.
//
// The `risk.acknowledgePolicyChallenge` endpoint (POST
// /rpc/risk.acknowledgePolicyChallenge) is a NEW server endpoint that is not yet
// in the generated `@gram/client` SDK. Once the SDK is regenerated, the codegen
// will emit `useRiskAcknowledgePolicyChallengeMutation` from
// `@gram/client/react-query/riskAcknowledgePolicyChallenge.js` (mirroring
// `useRiskCreatePolicyBypassRequestMutation` in
// `@gram/client/react-query/riskCreatePolicyBypassRequest.js`).
//
// When that happens, delete the local implementation below and replace this file
// with a single re-export so the acknowledge page picks up the real hook with no
// other changes:
//
//   export { useRiskAcknowledgePolicyChallengeMutation } from
//     "@gram/client/react-query/riskAcknowledgePolicyChallenge.js";
//
// Until regen, this local shim keeps the acknowledge page compiling and lets it
// call the endpoint via the RPC path directly. No existing code imports this
// module, so the not-yet-generated symbol cannot break the rest of the build.

export type RiskAcknowledgePolicyChallengeMutationVariables = {
  request: {
    acknowledgeRiskPolicyChallengeForm: {
      ackToken: string;
    };
  };
};

export function useRiskAcknowledgePolicyChallengeMutation(): UseMutationResult<
  unknown,
  Error,
  RiskAcknowledgePolicyChallengeMutationVariables
> {
  return useMutation({
    mutationKey: ["@gram/client", "riskPolicyChallenge", "acknowledge"],
    mutationFn: async ({
      request,
    }: RiskAcknowledgePolicyChallengeMutationVariables) => {
      const response = await fetch("/rpc/risk.acknowledgePolicyChallenge", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ack_token: request.acknowledgeRiskPolicyChallengeForm.ackToken,
        }),
      });
      if (!response.ok) {
        throw new Error(
          `acknowledgePolicyChallenge failed: ${response.status}`,
        );
      }
      return undefined;
    },
  });
}
