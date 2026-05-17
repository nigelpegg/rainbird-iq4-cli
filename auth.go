package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

const (
	authBase    = "https://iq4server.rainbird.com/coreidentityserver"
	clientID    = "C5A6F324-3CD3-4B22-9F78-B4835BA55D25"
	redirectURI = "https://iq4.rainbird.com/auth.html"
	userAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var tokenRegex = regexp.MustCompile(`access_token=([^&"]+)`)

// Authenticate performs the OIDC implicit flow to get a JWT token.
func Authenticate(username, password string) (string, error) {
	state := randomHex(8)
	nonce := randomHex(8)

	returnURL := fmt.Sprintf(
		"/coreidentityserver/connect/authorize/callback?client_id=%s&redirect_uri=%s&response_type=%s&scope=%s&state=%s&nonce=%s",
		clientID,
		url.QueryEscape(redirectURI),
		url.QueryEscape("id_token token"),
		url.QueryEscape("coreAPI.read coreAPI.write openid profile"),
		state,
		nonce,
	)

	loginURL := fmt.Sprintf("%s/Account/Login?ReturnUrl=%s", authBase, url.QueryEscape(returnURL))

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Check each redirect URL for the access token
			if token := extractToken(req.URL.String()); token != "" {
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	// Step 1: GET login page
	req, _ := http.NewRequest("GET", loginURL, nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get login page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 202 {
		return "", fmt.Errorf("AWS WAF challenge blocked the request")
	}

	body, _ := io.ReadAll(resp.Body)
	antiforgery := extractAntiforgeryToken(string(body))
	if antiforgery == "" {
		return "", fmt.Errorf("could not find antiforgery token on login page")
	}

	// Step 2: POST credentials
	form := url.Values{
		"Username":                     {username},
		"Password":                     {password},
		"ReturnUrl":                    {returnURL},
		"__RequestVerificationToken":   {antiforgery},
	}

	req, _ = http.NewRequest("POST", loginURL, strings.NewReader(form.Encode()))
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", authBase)
	req.Header.Set("Referer", loginURL)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")

	resp, err = client.Do(req)
	if err != nil {
		// Check if we got redirected to a URL with the token
		if resp != nil {
			loc := resp.Header.Get("Location")
			if token := extractToken(loc); token != "" {
				return token, nil
			}
		}
		return "", fmt.Errorf("post credentials: %w", err)
	}
	defer resp.Body.Close()

	// Check redirect location
	if loc := resp.Header.Get("Location"); loc != "" {
		if token := extractToken(loc); token != "" {
			return token, nil
		}
	}

	// Check response body
	body, _ = io.ReadAll(resp.Body)
	if token := extractToken(string(body)); token != "" {
		return token, nil
	}

	if resp.StatusCode == 202 {
		return "", fmt.Errorf("AWS WAF challenge blocked the request")
	}

	return "", fmt.Errorf("authentication failed – could not obtain access token (status %d)", resp.StatusCode)
}

func extractToken(s string) string {
	m := tokenRegex.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

var antiforgeryRegex = regexp.MustCompile(`__RequestVerificationToken.*?value="([^"]+)"`)

func extractAntiforgeryToken(html string) string {
	m := antiforgeryRegex.FindStringSubmatch(html)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
