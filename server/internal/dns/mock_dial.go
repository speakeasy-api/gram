package dns

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

// dial is the Dial function wired into the *net.Resolver returned by
// MockResolver.Resolver(). It creates an in-memory pipe and serves DNS
// responses from the mock's function fields.
//
// Because net.Pipe returns a stream connection (not a PacketConn), the Go
// resolver uses TCP framing: each DNS message is preceded by a 2-byte
// big-endian length prefix (RFC 7766 §5).
func (m *MockResolver) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()

	go m.serveDNS(ctx, server)

	return client, nil
}

// serveDNS reads length-prefixed DNS queries from conn, dispatches them to the
// appropriate mock function, and writes back length-prefixed DNS responses.
func (m *MockResolver) serveDNS(ctx context.Context, conn net.Conn) {
	defer conn.Close() //nolint:errcheck // best-effort cleanup

	for {
		// Read 2-byte length prefix.
		var lenBuf [2]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return // client closed
		}

		msgLen := binary.BigEndian.Uint16(lenBuf[:])

		msg := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, msg); err != nil {
			return
		}

		resp := m.handleQuery(ctx, msg)

		if len(resp) > math.MaxUint16 {
			return // response too large for TCP framing
		}

		// Write 2-byte length prefix + response.
		var respLen [2]byte
		binary.BigEndian.PutUint16(respLen[:], uint16(len(resp))) //nolint:gosec // length checked above

		if _, err := conn.Write(respLen[:]); err != nil {
			return
		}
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}

// handleQuery parses a raw DNS query, calls the matching mock function, and
// returns a packed DNS response.
func (m *MockResolver) handleQuery(ctx context.Context, raw []byte) []byte {
	var query dnsmessage.Message
	if err := query.Unpack(raw); err != nil {
		return m.buildSERVFAIL(query.Header, nil, fmt.Errorf("unpack query: %w", err))
	}

	if len(query.Questions) == 0 {
		return m.buildSERVFAIL(query.Header, nil, fmt.Errorf("empty question section"))
	}

	q := query.Questions[0]

	answers, err := m.answersForQuestion(ctx, q)
	if err != nil {
		return m.buildSERVFAIL(query.Header, &q, err)
	}

	resp := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 query.ID,
			Response:           true,
			OpCode:             0,
			Authoritative:      false,
			Truncated:          false,
			RecursionDesired:   query.RecursionDesired,
			RecursionAvailable: true,
			AuthenticData:      false,
			CheckingDisabled:   false,
			RCode:              dnsmessage.RCodeSuccess,
		},
		Questions:   query.Questions,
		Answers:     answers,
		Authorities: nil,
		Additionals: nil,
	}

	packed, err := resp.Pack()
	if err != nil {
		return m.buildSERVFAIL(query.Header, &q, fmt.Errorf("pack response: %w", err))
	}

	return packed
}

func (m *MockResolver) answersForQuestion(ctx context.Context, q dnsmessage.Question) ([]dnsmessage.Resource, error) {
	name := strings.TrimSuffix(q.Name.String(), ".")
	hdr := func(t dnsmessage.Type) dnsmessage.ResourceHeader {
		return dnsmessage.ResourceHeader{
			Name:   q.Name,
			Type:   t,
			Class:  dnsmessage.ClassINET,
			TTL:    60,
			Length: 0, // set automatically during packing
		}
	}

	switch q.Type {
	case dnsmessage.TypeA:
		ips, err := m.lookupIPFunc(ctx, "ip4", name)
		if err != nil {
			return nil, err
		}

		var resources []dnsmessage.Resource
		for _, ip := range ips {
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}

			resources = append(resources, dnsmessage.Resource{
				Header: hdr(dnsmessage.TypeA),
				Body:   &dnsmessage.AResource{A: [4]byte(ip4)},
			})
		}

		return resources, nil

	case dnsmessage.TypeAAAA:
		ips, err := m.lookupIPFunc(ctx, "ip6", name)
		if err != nil {
			return nil, err
		}

		var resources []dnsmessage.Resource
		for _, ip := range ips {
			ip16 := ip.To16()
			if ip16 == nil || ip.To4() != nil {
				continue // skip v4 addresses
			}

			resources = append(resources, dnsmessage.Resource{
				Header: hdr(dnsmessage.TypeAAAA),
				Body:   &dnsmessage.AAAAResource{AAAA: [16]byte(ip16)},
			})
		}

		return resources, nil

	case dnsmessage.TypeCNAME:
		cname, err := m.lookupCNAMEFunc(ctx, name)
		if err != nil {
			return nil, err
		}

		if cname == "" {
			return nil, nil
		}

		cnameName, err := dnsmessage.NewName(ensureFQDN(cname))
		if err != nil {
			return nil, fmt.Errorf("invalid CNAME %q: %w", cname, err)
		}

		return []dnsmessage.Resource{{
			Header: hdr(dnsmessage.TypeCNAME),
			Body:   &dnsmessage.CNAMEResource{CNAME: cnameName},
		}}, nil

	case dnsmessage.TypeTXT:
		txts, err := m.lookupTXTFunc(ctx, name)
		if err != nil {
			return nil, err
		}

		if len(txts) == 0 {
			return nil, nil
		}

		return []dnsmessage.Resource{{
			Header: hdr(dnsmessage.TypeTXT),
			Body:   &dnsmessage.TXTResource{TXT: txts},
		}}, nil

	case dnsmessage.TypeMX:
		mxs, err := m.lookupMXFunc(ctx, name)
		if err != nil {
			return nil, err
		}

		var resources []dnsmessage.Resource
		for _, mx := range mxs {
			mxName, err := dnsmessage.NewName(ensureFQDN(mx.Host))
			if err != nil {
				return nil, fmt.Errorf("invalid MX host %q: %w", mx.Host, err)
			}

			resources = append(resources, dnsmessage.Resource{
				Header: hdr(dnsmessage.TypeMX),
				Body: &dnsmessage.MXResource{
					Pref: mx.Pref,
					MX:   mxName,
				},
			})
		}

		return resources, nil

	case dnsmessage.TypeNS:
		nss, err := m.lookupNSFunc(ctx, name)
		if err != nil {
			return nil, err
		}

		var resources []dnsmessage.Resource
		for _, ns := range nss {
			nsName, err := dnsmessage.NewName(ensureFQDN(ns.Host))
			if err != nil {
				return nil, fmt.Errorf("invalid NS host %q: %w", ns.Host, err)
			}

			resources = append(resources, dnsmessage.Resource{
				Header: hdr(dnsmessage.TypeNS),
				Body:   &dnsmessage.NSResource{NS: nsName},
			})
		}

		return resources, nil

	case dnsmessage.TypeSRV:
		_, srvs, err := m.lookupSRVFunc(ctx, "", "", name)
		if err != nil {
			return nil, err
		}

		var resources []dnsmessage.Resource
		for _, srv := range srvs {
			target, err := dnsmessage.NewName(ensureFQDN(srv.Target))
			if err != nil {
				return nil, fmt.Errorf("invalid SRV target %q: %w", srv.Target, err)
			}

			resources = append(resources, dnsmessage.Resource{
				Header: hdr(dnsmessage.TypeSRV),
				Body: &dnsmessage.SRVResource{
					Priority: srv.Priority,
					Weight:   srv.Weight,
					Port:     srv.Port,
					Target:   target,
				},
			})
		}

		return resources, nil

	case dnsmessage.TypePTR:
		names, err := m.lookupAddrFunc(ctx, name)
		if err != nil {
			return nil, err
		}

		var resources []dnsmessage.Resource
		for _, n := range names {
			ptrName, err := dnsmessage.NewName(ensureFQDN(n))
			if err != nil {
				return nil, fmt.Errorf("invalid PTR name %q: %w", n, err)
			}

			resources = append(resources, dnsmessage.Resource{
				Header: hdr(dnsmessage.TypePTR),
				Body:   &dnsmessage.PTRResource{PTR: ptrName},
			})
		}

		return resources, nil

	default:
		// Unsupported query type — return empty answer (NOERROR with no records).
		return nil, nil
	}
}

// buildSERVFAIL builds a minimal SERVFAIL DNS response. The error is silently
// discarded since DNS wire format cannot carry error messages.
func (m *MockResolver) buildSERVFAIL(reqHeader dnsmessage.Header, q *dnsmessage.Question, _ error) []byte {
	resp := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 reqHeader.ID,
			Response:           true,
			OpCode:             0,
			Authoritative:      false,
			Truncated:          false,
			RecursionDesired:   false,
			RecursionAvailable: false,
			AuthenticData:      false,
			CheckingDisabled:   false,
			RCode:              dnsmessage.RCodeServerFailure,
		},
		Questions:   nil,
		Answers:     nil,
		Authorities: nil,
		Additionals: nil,
	}

	if q != nil {
		resp.Questions = []dnsmessage.Question{*q}
	}

	packed, err := resp.Pack()
	if err != nil {
		// Absolute fallback — should never happen for a minimal SERVFAIL.
		return nil
	}

	return packed
}

// ensureFQDN appends a trailing dot if not already present.
func ensureFQDN(s string) string {
	if strings.HasSuffix(s, ".") {
		return s
	}

	return s + "."
}
