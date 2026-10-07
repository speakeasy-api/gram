import { getOAuthSetupGuide } from "./registry";
import type { OAuthSetupGuideProps } from "./types";

export function GuidedOAuthSetup({
  serverUrl,
  ...props
}: Omit<OAuthSetupGuideProps, "serverUrl"> & {
  serverUrl: string | undefined;
}): JSX.Element {
  const Guide = getOAuthSetupGuide(serverUrl);
  if (!Guide || !serverUrl) return <>{props.children}</>;
  return <Guide serverUrl={serverUrl} {...props} />;
}
