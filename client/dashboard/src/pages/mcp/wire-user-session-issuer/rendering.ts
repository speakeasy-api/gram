export function shouldRenderWireUserSessionIssuerModal({
  showWireUserSessionIssuer,
  isOpen,
}: {
  showWireUserSessionIssuer: boolean;
  isOpen: boolean;
}): boolean {
  return showWireUserSessionIssuer || isOpen;
}
