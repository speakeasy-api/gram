package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	minPort         = 1024
	maxPort         = 65535
	callbackTimeout = 5 * time.Minute
)

// Listener manages an HTTP server that waits for OAuth callback.
type Listener struct {
	server   *http.Server
	listener net.Listener
	apiKey   chan string
	errChan  chan error
}

// NewListener creates a new callback listener on an available port.
func NewListener() (*Listener, error) {
	ln, err := findAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("failed to find available port: %w", err)
	}

	l := &Listener{
		server:   nil,
		listener: ln,
		apiKey:   make(chan string, 1),
		errChan:  make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", l.handleCallback)

	l.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return l, nil
}

// URL returns the callback URL for this listener.
func (l *Listener) URL() string {
	addr, ok := l.listener.Addr().(*net.TCPAddr)
	if !ok {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d/callback", addr.Port)
}

// Start begins listening for callbacks.
func (l *Listener) Start() {
	go func() {
		if err := l.server.Serve(l.listener); err != nil && err != http.ErrServerClosed {
			l.errChan <- fmt.Errorf("server error: %w", err)
		}
	}()
}

// Wait blocks until an API key is received or timeout occurs.
func (l *Listener) Wait(ctx context.Context) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, callbackTimeout)
	defer cancel()

	select {
	case key := <-l.apiKey:
		return key, nil
	case err := <-l.errChan:
		return "", err
	case <-timeoutCtx.Done():
		return "", fmt.Errorf("timeout waiting for authentication callback")
	}
}

// Stop gracefully shuts down the server.
func (l *Listener) Stop(ctx context.Context) error {
	if l.server == nil {
		return nil
	}
	if err := l.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	return nil
}

const authSuccessHTML = `
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Authentication Successful</title>
    <style>
      :root {
        --bg: hsl(0, 0%, 100%);
        --text: hsl(0, 0%, 33%);
        --card-bg: hsl(0, 0%, 99%);
        --card-border: hsl(0, 0%, 92%);
      }
      @media (prefers-color-scheme: dark) {
        :root {
          --bg: hsl(0, 0%, 0%);
          --text: hsl(0, 0%, 98%);
          --card-bg: hsl(0, 0%, 0%);
          --card-border: hsl(0, 0%, 33%);
        }
      }
      * {
        margin: 0;
        padding: 0;
        box-sizing: border-box;
      }
      html,
      body {
        height: 100%;
        font-weight: 200;
        font-family:
          -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica,
          Arial, sans-serif;
      }
      h1 {
        font-weight: 300;
        font-size: 1.2rem;
      }
      body {
        background: var(--bg);
        color: var(--text);
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
      }
      .card {
        border-radius: 0.5rem;
        background: var(--card-bg);
        border: solid 1px var(--card-border);
        padding: 1.5rem;
        gap: 0.5rem;
        display: flex;
        flex-direction: column;
        align-items: center;
        text-align: center;
      }

      svg {
        width: 3rem;
        height: 3rem;
      }
    </style>
  </head>
  <body>
    <div class="card">
      <svg
        width="24"
        height="24"
        viewBox="0 0 24 25"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <path
          fill-rule="evenodd"
          clip-rule="evenodd"
          d="M11.843 0.1756C12.249 -0.0574956 12.7506 -0.0602177 13.1609 0.177541L23.8003 5.7685L23.8119 5.77432L23.8168 5.77723L23.8255 5.78305C24.2484 6.03812 24.4989 6.50138 24.499 6.98839V8.16753C24.499 8.65302 24.2508 9.1179 23.8304 9.37093L23.8178 9.37869L23.8139 9.38064L23.8013 9.38743L23.0152 9.78824L23.8061 10.2153L23.8158 10.2201L23.8207 10.223L23.8275 10.2279C24.2469 10.4821 24.5 10.9448 24.5 11.4322V12.9054C24.4999 13.3904 24.2521 13.8537 23.8323 14.1069L23.8333 14.1079L23.8236 14.1137L23.8168 14.1185L23.8061 14.1234L23.8051 14.1224L23.0608 14.5135L23.8032 14.9037L23.8139 14.9095L23.8178 14.9124L23.8284 14.9182C24.2514 15.1733 24.4998 15.6365 24.5 16.1236V17.0077C24.5 17.4932 24.2519 17.958 23.8313 18.2111L23.8216 18.2169L23.8178 18.2198L23.8071 18.2247L13.157 23.8214L13.1085 23.8476L13.1066 23.8457C13.0176 23.8919 12.9252 23.9269 12.83 23.9515L12.828 23.9563L12.8076 24H12.501C12.2748 24 12.0465 23.9409 11.8401 23.8205L1.19681 18.2295L1.18613 18.2237L1.18225 18.2217L1.17254 18.2159C0.749505 17.9608 0.500079 17.4968 0.5 17.0096V6.98839C0.500163 6.503 0.748159 6.03893 1.16866 5.78596L1.17837 5.77917L1.18225 5.77723L1.19292 5.77141L11.843 0.1756ZM12.435 1.13056C12.4185 1.13561 12.4024 1.14329 12.3865 1.15288L12.3748 1.15967L12.3719 1.16064L12.3612 1.16646L1.73154 6.75063C1.66441 6.79414 1.61024 6.88418 1.61023 6.99033V17.0115C1.61047 17.118 1.66614 17.2086 1.73639 17.2532L12.3573 22.8335L12.3583 22.8344L12.368 22.8393L12.3709 22.8412L12.3816 22.8471C12.4565 22.8921 12.5415 22.8906 12.6087 22.85L12.6184 22.8432L12.6339 22.8354L23.2617 17.2503C23.3295 17.2072 23.3847 17.1182 23.3849 17.0115V16.1265C23.3848 16.0212 23.3312 15.9319 23.2626 15.8868L21.8574 15.1482L13.1541 19.7221L13.1531 19.7211C12.7469 19.954 12.2483 19.9545 11.841 19.7221L3.66474 15.5529L3.65115 15.5461L3.6463 15.5432L3.63465 15.5354C3.33625 15.3553 3.1612 15.0324 3.16106 14.6931V14.4718C3.1611 14.1323 3.33447 13.8106 3.62786 13.6314L3.63757 13.6255L3.64339 13.6216L3.65503 13.6158L6.5985 12.0679L3.65989 10.5161L3.64921 10.5112L3.64242 10.5074L3.63271 10.5006C3.33755 10.3207 3.16217 10.0002 3.16203 9.65916V9.43789C3.16208 9.09856 3.33559 8.77674 3.62883 8.59746L3.64339 8.58872L3.64921 8.58484L3.66474 8.57805L11.8478 4.4729C12.2547 4.23964 12.7553 4.24208 13.1628 4.47872L21.8215 9.14772L23.2694 8.40627C23.3325 8.36438 23.3878 8.27706 23.3878 8.17044V6.99228C23.3878 6.88506 23.3329 6.79314 23.2626 6.74868L12.6417 1.16938L12.63 1.16355L12.6252 1.16064L12.6145 1.15385C12.5791 1.13254 12.5385 1.12182 12.499 1.12182H12.4845L12.435 1.13056ZM4.27226 14.5514V14.6057L12.3515 18.7283H12.3535L12.3661 18.7351L12.369 18.7371L12.3816 18.7448C12.4582 18.7911 12.5441 18.7881 12.6106 18.7477L12.6213 18.7419L12.6242 18.74L12.6359 18.7342L20.6569 14.5184L17.2622 12.7346L13.1493 14.8396L13.1483 14.8386C12.7422 15.0698 12.2426 15.0684 11.8362 14.8328L7.79705 12.6987L4.27226 14.5514ZM12.6174 10.2909C12.5426 10.2459 12.4537 10.2489 12.3913 10.2871L12.3816 10.2939L12.3748 10.2977L12.3622 10.3036L8.99947 12.0698L12.3583 13.8439L12.3758 13.8526L12.3855 13.8584C12.4604 13.9034 12.5445 13.9011 12.6116 13.8604L12.6233 13.8526L12.6281 13.8497L12.6417 13.8439L16.0539 12.097L12.6417 10.3045L12.6407 10.3036L12.63 10.2987L12.6281 10.2977L12.6174 10.2909ZM18.4801 12.1106L21.8574 13.8856L23.2685 13.1442C23.334 13.0998 23.3868 13.0123 23.3869 12.9083V11.4351C23.3868 11.3282 23.3311 11.2371 23.2607 11.1925L21.8059 10.4074L18.4801 12.1106ZM12.6155 5.45115C12.5405 5.40612 12.4553 5.40691 12.3884 5.44727L12.3894 5.44824L12.3758 5.45697L12.368 5.46085L12.3535 5.46668L4.2742 9.52039V9.57473L7.79996 11.4371L11.842 9.3127C12.0427 9.19624 12.2719 9.13704 12.501 9.13704C12.7324 9.13734 12.9585 9.20015 13.157 9.31464L17.2699 11.4749L20.6132 9.76495L12.6359 5.46279L12.6349 5.46182L12.6262 5.45697L12.6242 5.456L12.6155 5.45115Z"
          fill="currentColor"
        />
      </svg>

      <h1>Authentication successful!</h1>
      <p>You can close this window.</p>
    </div>
  </body>
</html>`

func (l *Listener) handleCallback(w http.ResponseWriter, r *http.Request) {
	apiKey := r.URL.Query().Get("api_key")
	if apiKey == "" {
		http.Error(w, "missing api_key parameter", http.StatusBadRequest)
		return
	}

	l.apiKey <- apiKey
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, authSuccessHTML)
}

func findAvailablePort() (net.Listener, error) {
	// Let the OS pick an available port in the ephemeral range
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	// Verify it's in our acceptable range
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return nil, fmt.Errorf("unexpected address type")
	}
	if addr.Port < minPort || addr.Port > maxPort {
		_ = ln.Close()
		return nil, fmt.Errorf("port %d outside acceptable range", addr.Port)
	}

	return ln, nil
}
