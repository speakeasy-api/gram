package chat

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

// HandleCompletion is a simple proxy to the OpenAI API.
// TODO: Security etc
func HandleCompletion(w http.ResponseWriter, r *http.Request) {

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		println("OPENAI_API_KEY environment variable not set")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	target, _ := url.Parse("https://api.openai.com")
	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.URL.Host = target.Host
		req.URL.Scheme = target.Scheme
		req.URL.Path = "/v1/chat/completions"
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Handle CORS headers in the response
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Remove any existing CORS headers
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")

		return nil
	}

	proxy.ServeHTTP(w, r)
}
