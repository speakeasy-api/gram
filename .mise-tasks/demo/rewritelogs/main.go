package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	// maxExportBytes bounds one OTLP log export. Demo turns are short text
	// records; 32 MiB is far above that and still rejects a runaway body.
	maxExportBytes = 32 << 20 // 32 MiB

	// upstreamTimeout bounds the forward to this worktree's hooks ingest.
	// The exporter retries on its own schedule, so a hung local server
	// must not pin the hop open.
	upstreamTimeout = 30 * time.Second
)

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "address to receive OTLP/HTTP JSON logs on")
	upstream := flag.String("upstream", "", "hooks logs URL to forward rewritten exports to")
	readyFile := flag.String("ready-file", "", "file to write the logs URL into once listening")
	flag.Parse()

	if *upstream == "" {
		fmt.Fprintln(os.Stderr, "rewritelogs: -upstream is required")
		os.Exit(2)
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rewritelogs: listen: %v\n", err)
		os.Exit(1)
	}

	// The local Speakeasy server presents a mkcert certificate this process does
	// not trust. The hop only forwards to the URL it was started with.
	client := &http.Client{
		Timeout: upstreamTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local mkcert server
		},
	}
	server := &http.Server{
		Handler:           newRewriteHandler(*upstream, client),
		ReadHeaderTimeout: 5 * time.Second,
	}

	if *readyFile != "" {
		logsURL := "http://" + ln.Addr().String() + "/v1/logs"
		if err := writeReady(*readyFile, logsURL); err != nil {
			fmt.Fprintf(os.Stderr, "rewritelogs: ready file: %v\n", err)
			os.Exit(1)
		}
	}

	if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "rewritelogs: serve: %v\n", err)
		os.Exit(1)
	}
}

func writeReady(path, logsURL string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(logsURL), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newRewriteHandler(upstream string, client *http.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := readExport(r)
		if err != nil {
			http.Error(w, "read export", http.StatusBadRequest)
			return
		}
		payload, err := decodePayload(body)
		if err != nil {
			http.Error(w, "decode export", http.StatusBadRequest)
			return
		}
		rewritten, kept := rewriteDemoLogs(payload)
		if kept == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"partialSuccess":{}}`))
			return
		}
		encoded, err := json.Marshal(rewritten)
		if err != nil {
			http.Error(w, "encode export", http.StatusInternalServerError)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream, bytes.NewReader(encoded))
		if err != nil {
			http.Error(w, "forward export", http.StatusBadGateway)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		for _, key := range []string{"Gram-Key", "Gram-Project"} {
			if value := r.Header.Get(key); value != "" {
				req.Header.Set(key, value)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "forward export", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, maxExportBytes))
	})
}

func readExport(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	compressed := io.LimitReader(r.Body, maxExportBytes+1)
	var reader io.Reader = compressed
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(compressed)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	// Cap the decoded body too. A small gzip bomb is otherwise unbounded.
	body, err := io.ReadAll(io.LimitReader(reader, maxExportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxExportBytes {
		return nil, errors.New("export exceeds limit")
	}
	return body, nil
}
