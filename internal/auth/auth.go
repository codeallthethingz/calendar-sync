// Package auth builds the OAuth client and runs the one-time consent flow.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

// CalendarScope grants read and write access to all calendars.
const CalendarScope = "https://www.googleapis.com/auth/calendar"

const loginTimeout = 5 * time.Minute

// Config returns the OAuth client configuration for the given credentials.
func Config(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     endpoints.Google,
		Scopes:       []string{CalendarScope},
	}
}

// HTTPClient returns an HTTP client that refreshes access tokens from refreshToken.
func HTTPClient(ctx context.Context, cfg *oauth2.Config, refreshToken string) *http.Client {
	return cfg.Client(ctx, &oauth2.Token{RefreshToken: refreshToken})
}

// Login runs the loopback consent flow and returns the refresh token.
// openURL is called with the consent URL; the URL is also written to out.
func Login(ctx context.Context, cfg *oauth2.Config, openURL func(string) error, out io.Writer) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("start loopback listener: %w", err)
	}
	defer ln.Close()

	c := *cfg
	c.RedirectURL = fmt.Sprintf("http://%s/", ln.Addr())
	state, err := randomState()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	url := c.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.S256ChallengeOption(verifier))

	fmt.Fprintf(out, "Open this URL and sign in as the hub account:\n\n%s\n\n", url)
	if openURL != nil {
		if err := openURL(url); err != nil {
			fmt.Fprintf(out, "Could not open a browser (%v); open the URL by hand.\n", err)
		}
	}

	code, err := awaitCode(ctx, ln, state)
	if err != nil {
		return "", err
	}
	tok, err := c.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("exchange authorization code: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", errors.New("google returned no refresh token")
	}
	return tok.RefreshToken, nil
}

type callback struct {
	code string
	err  error
}

// awaitCode serves the redirect and returns the authorization code.
func awaitCode(ctx context.Context, ln net.Listener, state string) (string, error) {
	results := make(chan callback, 1)
	srv := &http.Server{
		Handler:           callbackHandler(state, results),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	select {
	case r := <-results:
		return r.code, r.err
	case <-ctx.Done():
		return "", fmt.Errorf("waiting for browser sign-in: %w", ctx.Err())
	}
}

func callbackHandler(state string, results chan<- callback) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		var res callback
		switch {
		case q.Get("state") != state:
			res.err = errors.New("oauth callback state mismatch")
		case q.Get("error") != "":
			res.err = fmt.Errorf("oauth consent failed: %s", q.Get("error"))
		case q.Get("code") == "":
			res.err = errors.New("oauth callback had no code")
		default:
			res.code = q.Get("code")
		}
		if res.err != nil {
			http.Error(w, res.err.Error(), http.StatusBadRequest)
		} else {
			fmt.Fprintln(w, "Signed in. You can close this window and return to the terminal.")
		}
		select {
		case results <- res:
		default:
		}
	})
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}
	return hex.EncodeToString(b), nil
}
