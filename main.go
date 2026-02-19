package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const (
	apiURL      = "https://api.ouraring.com/v2/usercollection/heartrate"
	tokenURL    = "https://api.ouraring.com/oauth/token"
	authURL     = "https://cloud.ouraring.com/oauth/authorize"
	redirectURI = "http://localhost:8085/callback"
	scope       = "heartrate"

	defaultTTL    = 300
	cacheFileName = "oura-hr"
	tokenFileName = "oura-tokens.json"
)

type storedTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type hrEntry struct {
	BPM       int    `json:"bpm"`
	Source    string `json:"source"`
	Timestamp string `json:"timestamp"`
}

type hrResponse struct {
	Data []hrEntry `json:"data"`
}

func cacheDir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache")
}

func cachePath() string { return filepath.Join(cacheDir(), cacheFileName) }
func tokenPath() string { return filepath.Join(cacheDir(), tokenFileName) }

func ttl() int {
	if v := os.Getenv("OURA_HR_CACHE_TTL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultTTL
}

func loadTokens() (*storedTokens, error) {
	data, err := os.ReadFile(tokenPath())
	if err != nil {
		return nil, err
	}
	var t storedTokens
	return &t, json.Unmarshal(data, &t)
}

func saveTokens(t *storedTokens) {
	data, _ := json.Marshal(t)
	os.MkdirAll(cacheDir(), 0o755)
	os.WriteFile(tokenPath(), data, 0o600)
}

func exchangeToken(clientID, clientSecret string, vals url.Values) (*storedTokens, error) {
	vals.Set("client_id", clientID)
	vals.Set("client_secret", clientSecret)

	resp, err := http.PostForm(tokenURL, vals)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.AccessToken == "" {
		return nil, fmt.Errorf("token exchange failed: %s", body)
	}
	return &storedTokens{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}, nil
}

func refresh(clientID, clientSecret string, old *storedTokens) (*storedTokens, error) {
	t, err := exchangeToken(clientID, clientSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {old.RefreshToken},
	})
	if err != nil {
		return nil, err
	}
	if t.RefreshToken == "" {
		t.RefreshToken = old.RefreshToken // keep if not rotated
	}
	return t, nil
}

func runSetup(clientID, clientSecret string) {
	codeCh := make(chan string, 1)
	mux := http.NewServeMux()
	srv := &http.Server{Addr: ":8085", Handler: mux}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		fmt.Fprintf(w, "<html><body><h2>%s</h2><p>You can close this tab.</p></body></html>",
			map[bool]string{true: "Authorization successful!", false: "Error: no code received"}[code != ""])
		codeCh <- code
	})

	go srv.ListenAndServe()
	time.Sleep(100 * time.Millisecond) // let the server start

	authorizationURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s",
		authURL, url.QueryEscape(clientID), url.QueryEscape(redirectURI), scope)

	fmt.Println("Opening browser for Oura authorization...")
	fmt.Println("If the browser doesn't open, visit:")
	fmt.Println(authorizationURL)
	if err := exec.Command("/usr/bin/open", authorizationURL).Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically: %v\n", err)
		fmt.Println("Please open the URL above manually.")
	}

	code := <-codeCh
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv.Shutdown(ctx)

	if code == "" {
		fmt.Fprintln(os.Stderr, "No authorization code received.")
		os.Exit(1)
	}

	t, err := exchangeToken(clientID, clientSecret, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
		os.Exit(1)
	}

	saveTokens(t)
	fmt.Printf("Done! Tokens saved to %s\n", tokenPath())
}

func main() {
	// Handle setup before the silent-exit check so we can print useful errors
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		clientID := os.Getenv("OURA_CLIENT_ID")
		clientSecret := os.Getenv("OURA_CLIENT_SECRET")
		if clientID == "" || clientSecret == "" {
			fmt.Fprintln(os.Stderr, "Error: OURA_CLIENT_ID and OURA_CLIENT_SECRET must be set.")
			fmt.Fprintln(os.Stderr, "Hint:  source ~/.secrets && ~/.dotfiles/oura-hr/oura-hr setup")
			os.Exit(1)
		}
		runSetup(clientID, clientSecret)
		return
	}

	clientID := os.Getenv("OURA_CLIENT_ID")
	clientSecret := os.Getenv("OURA_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		os.Exit(0)
	}

	// Serve from cache if fresh
	cache := cachePath()
	if info, err := os.Stat(cache); err == nil {
		if int(time.Since(info.ModTime()).Seconds()) < ttl() {
			if data, err := os.ReadFile(cache); err == nil {
				fmt.Print(string(data))
				return
			}
		}
	}

	t, err := loadTokens()
	if err != nil {
		os.Exit(0) // Not set up yet — silent
	}

	// Refresh if within 60s of expiry
	if time.Now().After(t.ExpiresAt.Add(-60 * time.Second)) {
		t, err = refresh(clientID, clientSecret, t)
		if err != nil {
			os.Exit(0)
		}
		saveTokens(t)
	}

	now := time.Now().UTC()
	reqURL := fmt.Sprintf("%s?start_datetime=%s&end_datetime=%s",
		apiURL, now.Add(-4*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		os.Exit(0)
	}
	req.Header.Set("Authorization", "Bearer "+t.AccessToken)

	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		os.Exit(0)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		os.Exit(0)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		os.Exit(0)
	}

	var result hrResponse
	if err := json.Unmarshal(body, &result); err != nil || len(result.Data) == 0 {
		os.Exit(0)
	}

	output := fmt.Sprintf("♥ %d\n", result.Data[len(result.Data)-1].BPM)
	os.MkdirAll(filepath.Dir(cache), 0o755)
	os.WriteFile(cache, []byte(output), 0o600)
	fmt.Print(output)
}
