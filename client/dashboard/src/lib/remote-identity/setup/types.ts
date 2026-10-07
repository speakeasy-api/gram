import type { ReactNode } from "react";
import type { UserIdentityDraft } from "../drafts/useIdentityDraft";

export interface OAuthSetupGuideProps {
  serverUrl: string;
  draft: UserIdentityDraft;
  disabled: boolean;
  children: ReactNode;
}
