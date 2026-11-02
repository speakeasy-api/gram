package tailnetgateway

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// forwardedForHeader carries the tailnet source address when tsnet's
// service-mode proxy hands the connection over loopback.
const forwardedForHeader = "X-Forwarded-For"

// PeerAddr returns the tailnet address of the peer behind a request. tsnet
// serves Tailscale Services through an internal loopback proxy that records
// the original source in X-Forwarded-For; direct listeners expose it as the
// remote address. The result is only used to ask WhoIs, never trusted as
// identity on its own.
func PeerAddr(r *http.Request) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, errors.New("invalid remote address")
	}
	if !remote.IsLoopback() {
		return remote.Unmap(), nil
	}

	forwarded := r.Header.Get(forwardedForHeader)
	first, _, _ := strings.Cut(forwarded, ",")
	peer, err := netip.ParseAddr(strings.TrimSpace(first))
	if err != nil {
		return netip.Addr{}, errors.New("loopback request without a forwarded tailnet address")
	}
	return peer.Unmap(), nil
}
