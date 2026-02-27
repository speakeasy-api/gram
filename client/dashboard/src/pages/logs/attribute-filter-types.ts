import { Op } from "@gram/client/models/components/attributefilter";

export type { Op };

export interface ActiveAttributeFilter {
  id: string;
  path: string;
  op: Op;
  value?: string;
}

export const OP_LABELS: Record<Op, string> = {
  eq: "=",
  not_eq: "\u2260",
  contains: "~",
  exists: "exists",
  not_exists: "\u2204",
};

export const VALUELESS_OPS: Op[] = [Op.Exists, Op.NotExists];
