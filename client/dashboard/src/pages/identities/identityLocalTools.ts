import { useCallback } from "react";
import { useSearchParams } from "react-router";

/**
 * Whether an identity page counts the identity's local tool calls in its
 * Tool calls chart and its Tool logs. Kept in the URL so a link keeps the
 * choice, and written only when local tools are hidden: showing them is the
 * default, so a default link carries no param.
 */
export const LOCAL_TOOLS_PARAM = "localTools";
const HIDE_LOCAL_TOOLS = "hide";

export function showsLocalTools(params: URLSearchParams): boolean {
  return params.get(LOCAL_TOOLS_PARAM) !== HIDE_LOCAL_TOOLS;
}

export function withShowLocalTools(
  params: URLSearchParams,
  show: boolean,
): URLSearchParams {
  const next = new URLSearchParams(params);
  if (show) {
    next.delete(LOCAL_TOOLS_PARAM);
    return next;
  }
  next.set(LOCAL_TOOLS_PARAM, HIDE_LOCAL_TOOLS);
  return next;
}

export function useShowLocalTools(): [boolean, (show: boolean) => void] {
  const [params, setParams] = useSearchParams();
  const setShow = useCallback(
    (show: boolean) => {
      setParams((prev) => withShowLocalTools(prev, show), { replace: true });
    },
    [setParams],
  );
  return [showsLocalTools(params), setShow];
}
