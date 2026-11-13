package tailnetgateway

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
)

// PeerAddr returns the tailnet address of the peer behind a request. tsnet
// forwards each service connection over loopback and names the peer in a
// PROXY header, which the listener turns into the remote address. A loopback
// address means a connection reached the listener without that header, and is
// refused. The result is only used to ask WhoIs, never trusted as identity on
// its own.
func PeerAddr(r *http.Request) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, errors.New("invalid remote address")
	}
	if remote.IsLoopback() {
		return netip.Addr{}, errors.New("request without a tailnet peer address")
	}
	return remote.Unmap(), nil
}
