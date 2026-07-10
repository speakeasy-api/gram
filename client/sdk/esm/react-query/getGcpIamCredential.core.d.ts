import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GcpIamCredential } from "../models/components/gcpiamcredential.js";
import { GetGcpIamCredentialRequest, GetGcpIamCredentialSecurity } from "../models/operations/getgcpiamcredential.js";
export type GetGcpIamCredentialQueryData = GcpIamCredential;
export declare function prefetchGetGcpIamCredential(queryClient: QueryClient, client$: GramCore, request: GetGcpIamCredentialRequest, security?: GetGcpIamCredentialSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildGetGcpIamCredentialQuery(client$: GramCore, request: GetGcpIamCredentialRequest, security?: GetGcpIamCredentialSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<GetGcpIamCredentialQueryData>;
};
export declare function queryKeyGetGcpIamCredential(parameters: {
    id: string;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getGcpIamCredential.core.d.ts.map