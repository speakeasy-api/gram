import { GramError } from "./gramerror.js";
/** The fallback error class if no more specific error class is matched */
export declare class APIError extends GramError {
  constructor(
    message: string,
    httpMeta: {
      response: Response;
      request: Request;
      body: string;
    },
  );
}
//# sourceMappingURL=apierror.d.ts.map
