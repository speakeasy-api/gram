import { gramAdminFetch, gramAdminMutation } from "@/lib/gramAdminApi";
import { snapshotSchema, type Draft, type Snapshot } from "./model";
import { serializeSupportMatrixUpdate } from "./request";

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
export async function saveSupportMatrix(
  revision: string,
  draft: Draft,
): Promise<Snapshot> {
  return snapshotSchema.parse(
    await gramAdminMutation<unknown>("/admin/supportMatrix.update", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: serializeSupportMatrixUpdate(revision, draft),
    }),
  );
}
