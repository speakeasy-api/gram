import { useLocation } from "react-router";
import { RiskPolicyChallengeAcknowledgeContent } from "@/components/access/RiskPolicyChallengeAcknowledgeContent";

export default function RiskPolicyChallengeAcknowledge(): JSX.Element {
  // A new link opened in the same tab only changes the fragment, which holds
  // the token. Remount so the page loads that challenge, not the last one.
  const { hash } = useLocation();
  return <RiskPolicyChallengeAcknowledgeContent key={hash} />;
}
