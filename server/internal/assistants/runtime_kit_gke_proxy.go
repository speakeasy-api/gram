package assistants

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// Kit v0.2.17's gateway serves HTTP only. Keep that listener inside the Pod's
// loopback namespace and terminate verified TLS with the repository's nginx
// unprivileged image pattern. Neither container receives a cluster API token.
const kitGKETLSProxyImage = "nginxinc/nginx-unprivileged:1.28-alpine"
const kitGKEPlaintextPort = 17766

func kitGKETLSProxyConfig(port int32) string {
	return fmt.Sprintf(`pid /tmp/nginx.pid;
error_log stderr crit;
worker_processes 1;
events { worker_connections 128; }
http {
 access_log off;
 client_body_temp_path /tmp/client_body;
 proxy_temp_path /tmp/proxy;
 fastcgi_temp_path /tmp/fastcgi;
 uwsgi_temp_path /tmp/uwsgi;
 scgi_temp_path /tmp/scgi;
 server {
  listen %d ssl;
  ssl_certificate /tls/tls.crt;
  ssl_certificate_key /tls/tls.key;
  ssl_protocols TLSv1.2 TLSv1.3;
  ssl_session_tickets off;
  client_max_body_size 16m;
  location / {
   proxy_pass http://127.0.0.1:%d;
   proxy_http_version 1.1;
   proxy_set_header Connection "";
   proxy_buffering off;
   proxy_request_buffering off;
   proxy_cache off;
   proxy_next_upstream off;
   proxy_read_timeout 3600s;
   proxy_send_timeout 3600s;
  }
 }
}
`, port, kitGKEPlaintextPort)
}

// No environment proxy, redirects, or retries may carry the runtime bearer to
// another destination. Dialing retains the guardian's CIDR enforcement, while
// TLS checks the unique bootstrap certificate and runtime DNS identity even
// though the route uses an authoritative Pod IP rather than cluster DNS.
func kitGKEGatewayClient(policy *guardian.Policy, cidrs []string, cert []byte, name string) (*guardian.HTTPClient, error) {
	config, err := kitGKETLSConfig(cert, name)
	if err != nil {
		return nil, err
	}
	blocks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid Kit gateway CIDR")
		}
		blocks = append(blocks, block)
	}
	dialer := policy.Dialer(guardian.WithDialerAllowedCIDRBlocks(blocks))
	return &guardian.HTTPClient{
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSClientConfig: config, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 2, MaxIdleConnsPerHost: 2},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}
