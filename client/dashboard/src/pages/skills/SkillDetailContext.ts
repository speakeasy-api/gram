import type { useSkill } from "@gram/client/react-query/skill.js";
import { useOutletContext } from "react-router";

export type SkillDetailContextValue = {
  skillQueryData: NonNullable<ReturnType<typeof useSkill>["data"]>;
};

export function useSkillDetailContext(): SkillDetailContextValue {
  return useOutletContext<SkillDetailContextValue>();
}
