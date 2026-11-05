package dns

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	typeCAA          dnsmessage.Type = 257
	caaLookupTimeout                 = 5 * time.Second
	resolvConfPath                   = "/etc/resolv.conf"
	dnsHeaderLen                     = 12
	dnsPort                          = "53"
)

func lookupCAA(ctx context.Context, resolver *net.Resolver, name string) ([]CAA, error) {
	name = normalizeDNSName(name)
	if name == "" {
		return nil, &net.DNSError{
			UnwrapErr:   nil,
			Err:         "empty name",
			Name:        name,
			Server:      "",
			IsTimeout:   false,
			IsTemporary: false,
			IsNotFound:  true,
		}
	}

	qname, err := dnsmessage.NewName(ensureFQDN(name))
	if err != nil {
		return nil, fmt.Errorf("caa query name %q: %w", name, err)
	}

	query := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 uint16(rand.UintN(1 << 16)), //nolint:gosec // DNS message id
			RecursionDesired:   true,
			RecursionAvailable: false,
			Response:           false,
			OpCode:             0,
			Authoritative:      false,
			Truncated:          false,
			AuthenticData:      false,
			CheckingDisabled:   false,
			RCode:              dnsmessage.RCodeSuccess,
		},
		Questions: []dnsmessage.Question{{
			Name:  qname,
			Type:  typeCAA,
			Class: dnsmessage.ClassINET,
		}},
		Answers:     nil,
		Authorities: nil,
		Additionals: nil,
	}
	payload, err := query.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack caa query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, caaLookupTimeout)
	defer cancel()

	resp, err := exchangeDNS(ctx, resolver, payload)
	if err != nil {
		return nil, fmt.Errorf("caa lookup %s: %w", name, err)
	}

	records, err := parseCAAAnswers(resp, name)
	if err != nil {
		return nil, fmt.Errorf("parse caa response for %s: %w", name, err)
	}
	return records, nil
}

func exchangeDNS(ctx context.Context, resolver *net.Resolver, payload []byte) ([]byte, error) {
	if resolver != nil && resolver.Dial != nil {
		conn, err := resolver.Dial(ctx, "udp", "0.0.0.0:53")
		if err != nil {
			return nil, fmt.Errorf("dial resolver: %w", err)
		}
		defer conn.Close() //nolint:errcheck // best-effort cleanup
		if deadline, ok := ctx.Deadline(); ok {
			if err := conn.SetDeadline(deadline); err != nil {
				return nil, fmt.Errorf("set resolver deadline: %w", err)
			}
		}
		if _, err := conn.Write(payload); err != nil {
			return nil, fmt.Errorf("write caa query: %w", err)
		}
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return nil, fmt.Errorf("read caa response: %w", err)
		}
		return buf[:n], nil
	}

	nameservers, err := systemNameservers()
	if err != nil {
		return nil, err
	}

	var lastErr error
	dialer := net.Dialer{}
	for _, ns := range nameservers {
		resp, err := exchangeUDP(ctx, &dialer, ns, payload)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no nameservers configured")
	}
	return nil, lastErr
}

func exchangeUDP(ctx context.Context, dialer *net.Dialer, nameserver string, payload []byte) ([]byte, error) {
	conn, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(nameserver, dnsPort))
	if err != nil {
		return nil, fmt.Errorf("dial nameserver %s: %w", nameserver, err)
	}
	defer conn.Close() //nolint:errcheck // best-effort cleanup
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set nameserver deadline: %w", err)
		}
	}
	if _, err := conn.Write(payload); err != nil {
		return nil, fmt.Errorf("write caa query: %w", err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read caa response: %w", err)
	}
	return buf[:n], nil
}

func systemNameservers() ([]string, error) {
	data, err := os.ReadFile(resolvConfPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", resolvConfPath, err)
	}
	var nameservers []string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		nameservers = append(nameservers, fields[1])
	}
	if len(nameservers) == 0 {
		return nil, fmt.Errorf("no nameserver entries in %s", resolvConfPath)
	}
	return nameservers, nil
}

func parseCAAAnswers(msg []byte, name string) ([]CAA, error) {
	if len(msg) < dnsHeaderLen {
		return nil, errors.New("response too short")
	}
	rcode := msg[3] & 0x0f
	if rcode == uint8(dnsmessage.RCodeNameError) {
		return nil, &net.DNSError{
			UnwrapErr:   nil,
			Err:         "no such host",
			Name:        name,
			Server:      "",
			IsTimeout:   false,
			IsTemporary: false,
			IsNotFound:  true,
		}
	}
	if rcode != uint8(dnsmessage.RCodeSuccess) {
		return nil, fmt.Errorf("rcode %d", rcode)
	}

	qdcount := binary.BigEndian.Uint16(msg[4:6])
	ancount := binary.BigEndian.Uint16(msg[6:8])
	off := dnsHeaderLen
	var err error
	for range qdcount {
		off, err = skipDNSName(msg, off)
		if err != nil {
			return nil, err
		}
		if off+4 > len(msg) {
			return nil, errors.New("truncated question")
		}
		off += 4
	}

	var records []CAA
	for range ancount {
		off, err = skipDNSName(msg, off)
		if err != nil {
			return nil, err
		}
		if off+10 > len(msg) {
			return nil, errors.New("truncated answer header")
		}
		typ := binary.BigEndian.Uint16(msg[off : off+2])
		rdlen := int(binary.BigEndian.Uint16(msg[off+8 : off+10]))
		off += 10
		if off+rdlen > len(msg) {
			return nil, errors.New("truncated answer rdata")
		}
		if typ == uint16(typeCAA) {
			rec, err := parseCAAResource(msg[off : off+rdlen])
			if err != nil {
				return nil, err
			}
			records = append(records, rec)
		}
		off += rdlen
	}
	return records, nil
}

func parseCAAResource(rdata []byte) (CAA, error) {
	var none CAA
	if len(rdata) < 2 {
		return none, errors.New("caa rdata too short")
	}
	flag := rdata[0]
	tagLen := int(rdata[1])
	if tagLen == 0 || 2+tagLen > len(rdata) {
		return none, errors.New("caa tag length out of range")
	}
	return CAA{
		Flag:  flag,
		Tag:   string(rdata[2 : 2+tagLen]),
		Value: string(rdata[2+tagLen:]),
	}, nil
}

func skipDNSName(msg []byte, off int) (int, error) {
	// At most one compression pointer may appear; after following it the
	// caller's offset is the byte after the pointer (2 bytes).
	jumped := false
	start := off
	for hops := 0; hops < 128; hops++ {
		if off >= len(msg) {
			return 0, errors.New("name out of range")
		}
		length := int(msg[off])
		if length == 0 {
			if !jumped {
				return off + 1, nil
			}
			return start + 2, nil
		}
		if length&0xc0 == 0xc0 {
			if off+1 >= len(msg) {
				return 0, errors.New("truncated name pointer")
			}
			if !jumped {
				start = off
			}
			off = int(binary.BigEndian.Uint16(msg[off:off+2]) & 0x3fff)
			jumped = true
			continue
		}
		if length&0xc0 != 0 {
			return 0, errors.New("invalid name label")
		}
		off += 1 + length
		if jumped {
			// Still walking the pointed-to name; return after we hit the
			// terminator so the caller offset is the original pointer + 2.
			continue
		}
	}
	return 0, errors.New("name too long")
}
