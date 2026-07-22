package oauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

func (m *Manager) startCallbackServer(ctx context.Context, expectedState string, result chan<- callbackData) (*http.Server, string, error) {
	addr := fmt.Sprintf("%s:%d", m.callbackHost(), m.callbackPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("binding callback server to %s: %w", addr, err)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://%s:%d/oauth/callback", m.callbackHost(), actualPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state != expectedState {
			result <- callbackData{err: errors.New("state mismatch in callback")}
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			errDesc := r.URL.Query().Get("error_description")
			result <- callbackData{err: fmt.Errorf("authorization error: %s: %s", errMsg, errDesc)}
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}

		result <- callbackData{code: code}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h1>Authorization successful!</h1><p>You can close this window.</p></body></html>")
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)

	return server, redirectURI, nil
}
