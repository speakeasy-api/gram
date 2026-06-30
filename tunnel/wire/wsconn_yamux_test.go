package wire_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestWSConnYamuxRoundTrip(t *testing.T) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		cfg := yamux.DefaultConfig()
		cfg.LogOutput = os.Stderr
		session, err := yamux.Server(wire.NewWSConn(ws), cfg)
		if err != nil {
			return
		}
		_ = http.Serve(session, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_, _ = w.Write([]byte("ok:" + string(body)))
		}))
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = os.Stderr
	session, err := yamux.Client(wire.NewWSConn(ws), cfg)
	require.NoError(t, err)
	defer session.Close()

	for i := range 25 {
		st, err := session.Open()
		require.NoErrorf(t, err, "open stream %d", i)
		req, _ := http.NewRequest(http.MethodPost, "http://x/", strings.NewReader("hello"))
		require.NoError(t, req.Write(st))
		resp, err := http.ReadResponse(bufio.NewReader(st), req)
		require.NoErrorf(t, err, "read response %d", i)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		_ = st.Close()
		require.Equalf(t, "ok:hello", string(body), "iter %d", i)
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWSConnHTTPServeClient(t *testing.T) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		session, err := yamux.Server(wire.NewWSConn(ws), yamux.DefaultConfig())
		if err != nil {
			return
		}
		go func() {
			_ = http.Serve(session, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hi")) }))
		}()
		<-session.CloseChan()
	}))
	defer srv.Close()

	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	session, err := yamux.Client(wire.NewWSConn(ws), yamux.DefaultConfig())
	require.NoError(t, err)
	defer session.Close()

	client := &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext:       func(_ context.Context, _, _ string) (net.Conn, error) { return session.Open() },
	}, Timeout: 5 * time.Second}

	resp, err := client.Post("http://x/_tunnel/hello", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp.Body.Close()

	time.Sleep(100 * time.Millisecond)
	require.False(t, session.IsClosed(), "session must survive the http.Client request")
}
