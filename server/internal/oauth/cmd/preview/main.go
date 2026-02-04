// Package main provides a standalone preview server for the external OAuth success page.
// This allows developers to preview the success page without going through an actual OAuth flow.
//
// Usage:
//
//	go run main.go
//	go run main.go --provider "GitHub"
//
// Then open http://localhost:8765 in a browser.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pageData struct {
	ProviderName string
	ScriptHash   string
	StyleHash    string
}

func main() {
	provider := flag.String("provider", "Test Provider", "OAuth provider name to display")
	port := flag.Int("port", 8765, "Port to run the preview server on")
	flag.Parse()

	// Find the oauth package directory (relative to this file's location)
	oauthDir, err := findOAuthDir()
	if err != nil {
		log.Fatalf("Failed to find oauth directory: %v", err)
	}

	// Load files using fixed filenames (not user-controlled)
	tmplData, err := loadFile(oauthDir, "hosted_external_oauth_success_page.html.tmpl")
	if err != nil {
		log.Fatalf("Failed to read template file: %v", err)
	}

	cssData, err := loadFile(oauthDir, "hosted_external_oauth_success_page.css")
	if err != nil {
		log.Fatalf("Failed to read CSS file: %v", err)
	}

	jsData, err := loadFile(oauthDir, "hosted_external_oauth_success_script.js")
	if err != nil {
		log.Fatalf("Failed to read JS file: %v", err)
	}

	// Compute hashes (same as external_oauth.go)
	scriptHash := sha256.Sum256(jsData)
	scriptHashStr := hex.EncodeToString(scriptHash[:])[:8]

	styleHash := sha256.Sum256(cssData)
	styleHashStr := hex.EncodeToString(styleHash[:])[:8]

	// Parse template
	successPageTmpl := template.Must(template.New("external_oauth_success").Parse(string(tmplData)))

	data := pageData{
		ProviderName: *provider,
		ScriptHash:   scriptHashStr,
		StyleHash:    styleHashStr,
	}

	mux := http.NewServeMux()

	// Serve the page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := successPageTmpl.Execute(w, data); err != nil {
			log.Printf("Failed to render template: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	// Serve CSS with cache-busting URL
	cssPath := fmt.Sprintf("/oauth-external/oauth_success-%s.css", styleHashStr)
	mux.HandleFunc(cssPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, err := w.Write(cssData); err != nil {
			log.Printf("Failed to write CSS: %v", err)
		}
	})

	// Serve JS with cache-busting URL
	jsPath := fmt.Sprintf("/oauth-external/oauth_success-%s.js", scriptHashStr)
	mux.HandleFunc(jsPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, err := w.Write(jsData); err != nil {
			log.Printf("Failed to write JS: %v", err)
		}
	})

	addr := fmt.Sprintf("localhost:%d", *port)
	fmt.Printf("Preview server running at http://%s\n", addr)
	fmt.Printf("Provider name: %s\n", *provider)
	fmt.Printf("CSS hash: %s\n", styleHashStr)
	fmt.Printf("JS hash: %s\n", scriptHashStr)
	fmt.Println("\nPress Ctrl+C to stop")

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// loadFile reads a file from the oauth directory. The filename must be one of the
// known static asset filenames to prevent path traversal.
func loadFile(oauthDir, filename string) ([]byte, error) {
	// Validate filename is one of our known files
	allowedFiles := map[string]bool{
		"hosted_external_oauth_success_page.html.tmpl": true,
		"hosted_external_oauth_success_page.css":       true,
		"hosted_external_oauth_success_script.js":      true,
	}
	if !allowedFiles[filename] {
		return nil, fmt.Errorf("unknown file: %s", filename)
	}

	path := filepath.Join(oauthDir, filename)
	data, err := os.ReadFile(path) //#nosec G304 -- filename is validated above
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filename, err)
	}
	return data, nil
}

// findOAuthDir locates the oauth package directory by looking for the template file.
// It searches relative to the current working directory.
func findOAuthDir() (string, error) {
	// Try common relative paths from where this might be run
	candidates := []string{
		".",                            // Running from oauth dir
		"../..",                        // Running from cmd/preview
		"server/internal/oauth",        // Running from repo root
		"internal/oauth",               // Running from server dir
		"../../server/internal/oauth",  // Running from some other location
	}

	// Also try to find it based on the executable location
	execPath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(execPath)
		candidates = append(candidates, filepath.Join(execDir, "../.."))
	}

	// Get current working directory for absolute path resolution
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	for _, candidate := range candidates {
		var checkPath string
		if filepath.IsAbs(candidate) {
			checkPath = candidate
		} else {
			checkPath = filepath.Join(cwd, candidate)
		}

		tmplPath := filepath.Join(checkPath, "hosted_external_oauth_success_page.html.tmpl")
		if _, err := os.Stat(tmplPath); err == nil {
			return checkPath, nil
		}
	}

	// If running with go run from the preview directory, go up to oauth dir
	if strings.HasSuffix(cwd, "cmd/preview") || strings.HasSuffix(cwd, "cmd"+string(filepath.Separator)+"preview") {
		oauthDir := filepath.Join(cwd, "../..")
		tmplPath := filepath.Join(oauthDir, "hosted_external_oauth_success_page.html.tmpl")
		if _, err := os.Stat(tmplPath); err == nil {
			return oauthDir, nil
		}
	}

	return "", fmt.Errorf("could not find oauth directory with template files (searched from %s)", cwd)
}
