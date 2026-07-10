import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AwsIamCredential } from "../models/components/awsiamcredential.js";
import { GetAwsIamCredentialRequest, GetAwsIamCredentialSecurity } from "../models/operations/getawsiamcredential.js";
export type GetAwsIamCredentialQueryData = AwsIamCredential;
export declare function prefetchGetAwsIamCredential(queryClient: QueryClient, client$: GramCore, request: GetAwsIamCredentialRequest, security?: GetAwsIamCredentialSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildGetAwsIamCredentialQuery(client$: GramCore, request: GetAwsIamCredentialRequest, security?: GetAwsIamCredentialSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<GetAwsIamCredentialQueryData>;
};
export declare function queryKeyGetAwsIamCredential(parameters: {
    id: string;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getAwsIamCredential.core.d.ts.map