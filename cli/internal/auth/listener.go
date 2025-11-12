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

// CallbackResult contains the data returned from the auth callback.
type CallbackResult struct {
	APIKey  string
	Project string
}

// Listener manages an HTTP server that waits for OAuth callback.
type Listener struct {
	server   *http.Server
	listener net.Listener
	result   chan CallbackResult
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
		result:   make(chan CallbackResult, 1),
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
func (l *Listener) Wait(ctx context.Context) (*CallbackResult, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, callbackTimeout)
	defer cancel()

	select {
	case result := <-l.result:
		return &result, nil
	case err := <-l.errChan:
		return nil, err
	case <-timeoutCtx.Done():
		return nil, fmt.Errorf("timeout waiting for authentication callback")
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

const authSuccessHTML = `<!doctype html>
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
        font-weight: 300;
        font-family:
          -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica,
          Arial, sans-serif;
      }
      h1 {
        font-weight: 200;
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
        height: 5rem;
        width: 4rem;
        margin-bottom: 0.5rem;
      }
    </style>
  </head>
  <body>
    <div class="card">
      <svg
        width="37"
        height="45"
        viewBox="0 0 37 45"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <path
          d="M23.3749 40.8878V32.1033H24.5979L24.7873 33.2746C25.2007 32.6545 26.0103 32 27.3538 32C28.5767 32 29.6274 32.5512 30.1269 33.6708C30.6609 32.7234 31.6082 32 33.1756 32C35.0014 32 36.5 32.999 36.5 35.5827V40.8878H35.0875V35.6688C35.0875 34.1358 34.3297 33.2918 33.0034 33.2918C31.5393 33.2918 30.6436 34.3253 30.6436 35.9444V40.8878H29.2312V35.6688C29.2312 34.1358 28.4561 33.2918 27.1126 33.2918C25.6658 33.2918 24.7873 34.4286 24.7873 36.0133V40.8878H23.3749Z"
          fill="currentColor"
        />
        <path
          d="M17.7764 40.9912C15.8473 40.9912 14.5382 40.0266 14.5382 38.4247C14.5382 36.6678 15.7611 35.686 18.0692 35.686H20.6529V35.0832C20.6529 33.8947 19.8433 33.2574 18.4654 33.2574C17.1735 33.2574 16.3984 33.843 16.2262 34.7387H14.8138C15.0205 33.0162 16.4157 32 18.5343 32C20.7734 32 22.0653 33.1196 22.0653 35.1693V39.0103C22.0653 39.4754 22.2375 39.596 22.6337 39.596H24.4819V40.8878H22.3064C21.1868 40.8878 20.7218 40.3711 20.7218 39.5615V39.5271C20.205 40.3194 19.2921 40.9912 17.7764 40.9912ZM17.8797 39.7854C19.6366 39.7854 20.6529 38.7692 20.6529 37.3051V36.8745H17.9314C16.674 36.8745 15.9678 37.3396 15.9678 38.3386C15.9678 39.2687 16.7085 39.7854 17.8797 39.7854Z"
          fill="currentColor"
        />
        <path
          d="M10.3456 40.888V32.1035H11.5685L11.758 33.4298C12.1714 32.758 12.8431 32.1035 14.4105 32.1035H14.8067V33.4815H14.1005C12.2575 33.4815 11.758 35.0144 11.758 36.4096V40.888H10.3456Z"
          fill="currentColor"
        />
        <path
          d="M4.78888 44.436C2.54971 44.436 0.982284 43.3853 0.758366 41.4562H2.20522C2.44636 42.5586 3.42815 43.1787 4.89223 43.1787C6.73524 43.1787 7.73426 42.3174 7.73426 40.4055V39.4754C7.14863 40.2849 6.20129 40.9911 4.71998 40.9911C2.29134 40.9911 0.5 39.3548 0.5 36.4956C0.5 33.8086 2.29134 32 4.71998 32C6.20129 32 7.2003 32.6029 7.73426 33.4469L7.92373 32.1033H9.14666V40.4572C9.14666 42.9892 7.80316 44.436 4.78888 44.436ZM4.85778 39.7338C6.58022 39.7338 7.75148 38.4075 7.75148 36.53C7.75148 34.6009 6.58022 33.2574 4.85778 33.2574C3.11811 33.2574 1.94685 34.5837 1.94685 36.4956C1.94685 38.4075 3.11811 39.7338 4.85778 39.7338Z"
          fill="currentColor"
        />
        <path
          fill-rule="evenodd"
          clip-rule="evenodd"
          d="M17.843 0.1756C18.2491 -0.0574956 18.7507 -0.0602177 19.161 0.177541L29.8003 5.7685L29.812 5.77432L29.8168 5.77723L29.8256 5.78305C30.2484 6.03812 30.4989 6.50138 30.4991 6.98839V8.16753C30.499 8.65302 30.2508 9.1179 29.8304 9.37093L29.8178 9.37869L29.8139 9.38064L29.8013 9.38743L29.0152 9.78824L29.8062 10.2153L29.8159 10.2201L29.8207 10.223L29.8275 10.2279C30.2469 10.4821 30.5001 10.9448 30.5001 11.4322V12.9054C30.5 13.3904 30.2522 13.8537 29.8324 14.1069L29.8333 14.1079L29.8236 14.1137L29.8168 14.1185L29.8062 14.1234L29.8052 14.1224L29.0608 14.5135L29.8033 14.9037L29.8139 14.9095L29.8178 14.9124L29.8285 14.9182C30.2514 15.1733 30.4999 15.6365 30.5001 16.1236V17.0077C30.5 17.4932 30.252 17.958 29.8314 18.2111L29.8217 18.2169L29.8178 18.2198L29.8071 18.2247L19.1571 23.8214L19.1086 23.8476L19.1066 23.8457C19.0177 23.8919 18.9253 23.9269 18.83 23.9515L18.8281 23.9563L18.8077 24H18.501C18.2749 24 18.0465 23.9409 17.8401 23.8205L7.19687 18.2295L7.18619 18.2237L7.18231 18.2217L7.1726 18.2159C6.74957 17.9608 6.50014 17.4968 6.50006 17.0096V6.98839C6.50022 6.503 6.74822 6.03893 7.16872 5.78596L7.17843 5.77917L7.18231 5.77723L7.19298 5.77141L17.843 0.1756ZM18.435 1.13056C18.4186 1.13561 18.4024 1.14329 18.3865 1.15288L18.3749 1.15967L18.372 1.16064L18.3613 1.16646L7.7316 6.75063C7.66447 6.79414 7.6103 6.88418 7.61029 6.99033V17.0115C7.61053 17.118 7.6662 17.2086 7.73645 17.2532L18.3574 22.8335L18.3584 22.8344L18.3681 22.8393L18.371 22.8412L18.3817 22.8471C18.4565 22.8921 18.5416 22.8906 18.6088 22.85L18.6185 22.8432L18.634 22.8354L29.2617 17.2503C29.3296 17.2072 29.3847 17.1182 29.385 17.0115V16.1265C29.3848 16.0212 29.3313 15.9319 29.2627 15.8868L27.8574 15.1482L19.1542 19.7221L19.1532 19.7211C18.747 19.954 18.2484 19.9545 17.8411 19.7221L9.6648 15.5529L9.65121 15.5461L9.64636 15.5432L9.63472 15.5354C9.33631 15.3553 9.16126 15.0324 9.16112 14.6931V14.4718C9.16116 14.1323 9.33453 13.8106 9.62792 13.6314L9.63763 13.6255L9.64345 13.6216L9.6551 13.6158L12.5986 12.0679L9.65995 10.5161L9.64927 10.5112L9.64248 10.5074L9.63277 10.5006C9.33762 10.3207 9.16223 10.0002 9.16209 9.65916V9.43789C9.16214 9.09856 9.33565 8.77674 9.62889 8.59746L9.64345 8.58872L9.64927 8.58484L9.6648 8.57805L17.8479 4.4729C18.2547 4.23964 18.7553 4.24208 19.1629 4.47872L27.8215 9.14772L29.2695 8.40627C29.3325 8.36438 29.3878 8.27706 29.3879 8.17044V6.99228C29.3879 6.88506 29.333 6.79314 29.2627 6.74868L18.6418 1.16938L18.6301 1.16355L18.6253 1.16064L18.6146 1.15385C18.5791 1.13254 18.5386 1.12182 18.4991 1.12182H18.4845L18.435 1.13056ZM10.2723 14.5514V14.6057L18.3516 18.7283H18.3535L18.3661 18.7351L18.369 18.7371L18.3817 18.7448C18.4583 18.7911 18.5441 18.7881 18.6107 18.7477L18.6214 18.7419L18.6243 18.74L18.6359 18.7342L26.657 14.5184L23.2622 12.7346L19.1493 14.8396L19.1483 14.8386C18.7423 15.0698 18.2427 15.0684 17.8363 14.8328L13.7971 12.6987L10.2723 14.5514ZM18.6175 10.2909C18.5426 10.2459 18.4538 10.2489 18.3914 10.2871L18.3817 10.2939L18.3749 10.2977L18.3623 10.3036L14.9995 12.0698L18.3584 13.8439L18.3758 13.8526L18.3855 13.8584C18.4604 13.9034 18.5445 13.9011 18.6117 13.8604L18.6233 13.8526L18.6282 13.8497L18.6418 13.8439L22.054 12.097L18.6418 10.3045L18.6408 10.3036L18.6301 10.2987L18.6282 10.2977L18.6175 10.2909ZM24.4802 12.1106L27.8574 13.8856L29.2685 13.1442C29.3341 13.0998 29.3868 13.0123 29.3869 12.9083V11.4351C29.3869 11.3282 29.3312 11.2371 29.2608 11.1925L27.806 10.4074L24.4802 12.1106ZM18.6155 5.45115C18.5406 5.40612 18.4554 5.40691 18.3885 5.44727L18.3894 5.44824L18.3758 5.45697L18.3681 5.46085L18.3535 5.46668L10.2743 9.52039V9.57473L13.8 11.4371L17.8421 9.3127C18.0428 9.19624 18.272 9.13704 18.501 9.13704C18.7324 9.13734 18.9586 9.20015 19.1571 9.31464L23.27 11.4749L26.6133 9.76495L18.6359 5.46279L18.635 5.46182L18.6262 5.45697L18.6243 5.456L18.6155 5.45115Z"
          fill="currentColor"
        />
      </svg>
      <h1>Authentication successful!</h1>
      <p>You can close this window.</p>
    </div>
  </body>
</html>
`

func (l *Listener) handleCallback(w http.ResponseWriter, r *http.Request) {
	apiKey := r.URL.Query().Get("api_key")
	if apiKey == "" {
		http.Error(w, "missing api_key parameter", http.StatusBadRequest)
		return
	}

	project := r.URL.Query().Get("project")

	l.result <- CallbackResult{
		APIKey:  apiKey,
		Project: project,
	}
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
