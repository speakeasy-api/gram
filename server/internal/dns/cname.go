package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	dnswire "github.com/miekg/dns"
)

// cnameQueryTimeout bounds a record observation, including nameserver fallback
// and a truncated UDP response's TCP retry.
const cnameQueryTimeout = 3 * time.Second

// LookupCNAMERecord observes only the CNAME owned by host. An empty result means
// NOERROR without a CNAME, not NXDOMAIN. It never derives a name from A/AAAA.
func (n *NetResolver) LookupCNAMERecord(ctx context.Context, host string) (string, error) {
	config, err := dnswire.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return "", fmt.Errorf("read DNS nameservers: %w", err)
	}
	servers := make([]string, 0, len(config.Servers))
	for _, server := range config.Servers {
		servers = append(servers, net.JoinHostPort(server, config.Port))
	}
	return lookupCNAMERecord(ctx, host, servers)
}

func lookupCNAMERecord(ctx context.Context, host string, servers []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cnameQueryTimeout)
	defer cancel()

	name := dnswire.Fqdn(host)
	query := new(dnswire.Msg)
	query.SetQuestion(name, dnswire.TypeCNAME)
	var lastErr error
	for _, server := range servers {
		client := new(dnswire.Client)
		answer, _, err := client.ExchangeContext(ctx, query, server)
		if err == nil && answer.Truncated {
			client.Net = "tcp"
			answer, _, err = client.ExchangeContext(ctx, query, server)
		}
		if err != nil {
			lastErr = err
			continue
		}
		if !answer.Response || answer.Truncated || len(answer.Question) != 1 ||
			!strings.EqualFold(answer.Question[0].Name, name) ||
			answer.Question[0].Qtype != dnswire.TypeCNAME ||
			answer.Question[0].Qclass != dnswire.ClassINET {
			lastErr = errors.New("invalid CNAME response")
			continue
		}
		switch answer.Rcode {
		case dnswire.RcodeSuccess, dnswire.RcodeNameError:
			// A negative response can describe the terminal target while still
			// proving the queried alias exists (RFC 2308 section 2.1).
			for _, record := range answer.Answer {
				cname, ok := record.(*dnswire.CNAME)
				if ok && cname.Hdr.Class == dnswire.ClassINET && strings.EqualFold(cname.Hdr.Name, name) {
					return cname.Target, nil
				}
			}
			if answer.Rcode == dnswire.RcodeNameError {
				return "", &net.DNSError{Err: "no such host", Name: host, Server: server, IsNotFound: true, UnwrapErr: nil, IsTimeout: false, IsTemporary: false}
			}
			return "", nil
		default:
			lastErr = fmt.Errorf("CNAME response code %d", answer.Rcode)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no DNS nameservers configured")
	}
	return "", fmt.Errorf("observe CNAME for %s: %w", host, lastErr)
}
