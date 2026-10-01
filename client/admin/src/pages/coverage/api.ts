import { gramAdminFetch } from "@/lib/gramAdminApi";
import { snapshotSchema, type Snapshot } from "./model";

export const supportMatrixQuery = {
  queryKey: ["support-matrix"],
  queryFn: async (): Promise<Snapshot> =>
    snapshotSchema.parse(
      await gramAdminFetch<unknown>("/admin/supportMatrix.get", {
        cache: "no-store",
      }),
    ),
  staleTime: Infinity,
  refetchOnWindowFocus: false,
};
