import * as z from "zod/v4-mini";
import { GramError } from "./gramerror.js";
/**
 * unauthorized access
 */
export type ServiceErrorData = {
  /**
   * Is the error a server-side fault?
   */
  fault: boolean;
  /**
   * ID is a unique identifier for this particular occurrence of the problem.
   */
  id: string;
  /**
   * Message is a human-readable explanation specific to this occurrence of the problem.
   */
  message: string;
  /**
   * Name is the name of this class of errors.
   */
  name: string;
  /**
   * Is the error temporary?
   */
  temporary: boolean;
  /**
   * Is the error a timeout?
   */
  timeout: boolean;
};
/**
 * unauthorized access
 */
export declare class ServiceError extends GramError {
  /**
   * Is the error a server-side fault?
   */
  fault: boolean;
  /**
   * ID is a unique identifier for this particular occurrence of the problem.
   */
  id: string;
  /**
   * Is the error temporary?
   */
  temporary: boolean;
  /**
   * Is the error a timeout?
   */
  timeout: boolean;
  /** The original data that was passed to this error instance. */
  data$: ServiceErrorData;
  constructor(
    err: ServiceErrorData,
    httpMeta: {
      response: Response;
      request: Request;
      body: string;
    },
  );
}
/** @internal */
export declare const ServiceError$inboundSchema: z.ZodMiniType<
  ServiceError,
  unknown
>;
//# sourceMappingURL=serviceerror.d.ts.map
