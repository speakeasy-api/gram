import type { IncomingMessage } from "node:http";
import { BlockList, isIP } from "node:net";

// A coarse address filter for trusted development networks, not peer
// authentication. Tailscale shares its IPv4 range with other CGNAT networks.
const allowedPeers = new BlockList();
allowedPeers.addSubnet("127.0.0.0", 8, "ipv4");
allowedPeers.addAddress("::1", "ipv6");
allowedPeers.addSubnet("100.64.0.0", 10, "ipv4");
allowedPeers.addSubnet("fd7a:115c:a1e0::", 48, "ipv6");

export function fromAllowedPeer(req: Pick<IncomingMessage, "socket">): boolean {
  // Forwarded headers cannot grant access; only the TCP peer is considered.
  const addr = req.socket.remoteAddress ?? "";
  const family = isIP(addr);
  return (
    family !== 0 && allowedPeers.check(addr, family === 4 ? "ipv4" : "ipv6")
  );
}
