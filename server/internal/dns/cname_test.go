package dns

import (
	"context"
	"net"
	"testing"
	"time"

	dnswire "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

func startCNAMEServer(t *testing.T, handler dnswire.HandlerFunc) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	packet, err := net.ListenPacket("udp", listener.Addr().String())
	require.NoError(t, err)
	for _, server := range []*dnswire.Server{
		{Listener: listener, Handler: handler},
		{PacketConn: packet, Handler: handler},
	} {
		ready := make(chan struct{})
		done := make(chan error, 1)
		server.NotifyStartedFunc = func() { close(ready) }
		go func() { done <- server.ActivateAndServe() }()
		<-ready
		t.Cleanup(func() { require.NoError(t, server.Shutdown()); require.NoError(t, <-done) })
	}
	return listener.Addr().String()
}

func TestLookupCNAMERecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		rcode    int
		answers  []dnswire.RR
		want     string
		notFound bool
	}{
		{name: "chain in reverse order", answers: []dnswire.RR{
			&dnswire.CNAME{Hdr: dnswire.RR_Header{Name: "shared.example.", Rrtype: dnswire.TypeCNAME, Class: dnswire.ClassINET}, Target: "ingress.example."},
			&dnswire.CNAME{Hdr: dnswire.RR_Header{Name: "DOMAIN.EXAMPLE.", Rrtype: dnswire.TypeCNAME, Class: dnswire.ClassINET}, Target: "shared.example."},
		}, want: "shared.example."},
		{name: "no CNAME"},
		{name: "NXDOMAIN", rcode: dnswire.RcodeNameError, notFound: true},
		{name: "NXDOMAIN for terminal target preserves alias", rcode: dnswire.RcodeNameError, answers: []dnswire.RR{
			&dnswire.CNAME{Hdr: dnswire.RR_Header{Name: "domain.example.", Rrtype: dnswire.TypeCNAME, Class: dnswire.ClassINET}, Target: "missing.example."},
		}, want: "missing.example."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := startCNAMEServer(t, func(w dnswire.ResponseWriter, request *dnswire.Msg) {
				reply := new(dnswire.Msg).SetReply(request)
				reply.Rcode = tc.rcode
				reply.Answer = tc.answers
				_ = w.WriteMsg(reply)
			})
			got, err := lookupCNAMERecord(t.Context(), "domain.example", []string{server})
			if tc.notFound {
				var dnsErr *net.DNSError
				require.ErrorAs(t, err, &dnsErr)
				require.True(t, dnsErr.IsNotFound)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestLookupCNAMERecordRetriesTruncatedUDPOverTCP(t *testing.T) {
	t.Parallel()
	server := startCNAMEServer(t, func(w dnswire.ResponseWriter, request *dnswire.Msg) {
		reply := new(dnswire.Msg).SetReply(request)
		if w.LocalAddr().Network() == "udp" {
			reply.Truncated = true
		} else {
			reply.Answer = []dnswire.RR{&dnswire.CNAME{Hdr: dnswire.RR_Header{Name: "domain.example.", Rrtype: dnswire.TypeCNAME, Class: dnswire.ClassINET}, Target: "shared.example."}}
		}
		_ = w.WriteMsg(reply)
	})
	got, err := lookupCNAMERecord(t.Context(), "domain.example", []string{server})
	require.NoError(t, err)
	require.Equal(t, "shared.example.", got)
}

func TestLookupCNAMERecordCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := lookupCNAMERecord(ctx, "domain.example", []string{"127.0.0.1:53"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestLookupCNAMERecordFallsBackAfterServerFailure(t *testing.T) {
	t.Parallel()
	failed := startCNAMEServer(t, func(w dnswire.ResponseWriter, request *dnswire.Msg) {
		_ = w.WriteMsg(new(dnswire.Msg).SetRcode(request, dnswire.RcodeServerFailure))
	})
	working := startCNAMEServer(t, func(w dnswire.ResponseWriter, request *dnswire.Msg) {
		reply := new(dnswire.Msg).SetReply(request)
		reply.Answer = []dnswire.RR{&dnswire.CNAME{Hdr: dnswire.RR_Header{Name: "domain.example.", Rrtype: dnswire.TypeCNAME, Class: dnswire.ClassINET}, Target: "fallback.example."}}
		_ = w.WriteMsg(reply)
	})
	got, err := lookupCNAMERecord(t.Context(), "domain.example", []string{failed, working})
	require.NoError(t, err)
	require.Equal(t, "fallback.example.", got)
}

func TestLookupCNAMERecordDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := lookupCNAMERecord(ctx, "domain.example", []string{"127.0.0.1:53"})
	require.Error(t, err)
	var timeout net.Error
	require.ErrorAs(t, err, &timeout)
	require.True(t, timeout.Timeout())
}
