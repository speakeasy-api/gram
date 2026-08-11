import type { useSkill } from "@gram/client/react-query/skill.js";
import { useOutletContext } from "react-router";

export type SkillDetailContextValue = {
  skillQueryData: NonNullable<ReturnType<typeof useSkill>["data"]>;
  versionLabels: Map<string, string>;
  versionsLoading: boolean;
  versionsError: Error | null;
};

export function useSkillDetailContext(): SkillDetailContextValue {
  return useOutletContext<SkillDetailContextValue>();
}
