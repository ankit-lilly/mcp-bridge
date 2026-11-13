package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ankit-lilly/mcp-bridge/internal/oauth"
	"github.com/ankit-lilly/mcp-bridge/internal/remote"
)

func testChallenge(token string) *remote.AuthRequiredError {
	credential := ""
	if token != "" {
		credential = "Bearer " + token
	}
	return &remote.AuthRequiredError{StatusCode: http.StatusUnauthorized, SentCredential: remote.CredentialFingerprint(credential)}
}

func TestAuthorizer_DeduplicatesSimultaneousAndStaleChallenges(t *testing.T) {
	mgr := oauth.NewManager(oauth.ManagerConfig{Token: &oauth.Token{AccessToken: "old"}})
	started := make(chan struct{})
	finish := make(chan struct{})
	var calls atomic.Int32
	a := &authorizer{mgr: mgr, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), authorize: func(context.Context, *oauth.ChallengeInfo) (*oauth.Token, *oauth.ClientRegistration, error) {
		calls.Add(1)
		close(started)
		<-finish
		mgr.SetToken(&oauth.Token{AccessToken: "new"})
		return nil, nil, nil
	}}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.EnsureAuthorized(context.Background(), testChallenge("old")); err != nil {
			t.Errorf("first challenge: %v", err)
		}
	}()
	<-started
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.EnsureAuthorized(context.Background(), testChallenge("old")); err != nil {
			t.Errorf("concurrent challenge: %v", err)
		}
	}()
	close(finish)
	wg.Wait()
	if err := a.EnsureAuthorized(context.Background(), testChallenge("old")); err != nil {
		t.Fatalf("stale challenge: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected one browser authorization, got %d", n)
	}
	if err := a.EnsureAuthorized(context.Background(), testChallenge("new")); !errors.Is(err, remote.ErrUnauthorized) {
		t.Fatalf("newly authorized token should not prompt repeatedly: %v", err)
	}
}

func TestAuthorizer_WaitingCallerCanCancel(t *testing.T) {
	mgr := oauth.NewManager(oauth.ManagerConfig{})
	started := make(chan struct{})
	finish := make(chan struct{})
	a := &authorizer{mgr: mgr, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), authorize: func(context.Context, *oauth.ChallengeInfo) (*oauth.Token, *oauth.ClientRegistration, error) {
		close(started)
		<-finish
		mgr.SetToken(&oauth.Token{AccessToken: "new"})
		return nil, nil, nil
	}}
	leaderDone := make(chan error, 1)
	go func() { leaderDone <- a.EnsureAuthorized(context.Background(), testChallenge("")) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.EnsureAuthorized(ctx, testChallenge("")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	close(finish)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader: %v", err)
	}
}

func TestAuthorizer_FailedFlowDoesNotImmediatelyReopen(t *testing.T) {
	failure := errors.New("browser authorization failed")
	var calls atomic.Int32
	a := &authorizer{mgr: oauth.NewManager(oauth.ManagerConfig{}), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), authorize: func(context.Context, *oauth.ChallengeInfo) (*oauth.Token, *oauth.ClientRegistration, error) {
		calls.Add(1)
		return nil, nil, failure
	}}
	for range 2 {
		if err := a.EnsureAuthorized(context.Background(), testChallenge("")); !errors.Is(err, failure) {
			t.Fatalf("expected authorization failure, got %v", err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected one authorization attempt, got %d", n)
	}
}

func TestAuthorizer_InitialChallengeAndPreexistingToken(t *testing.T) {
	var calls atomic.Int32
	mgr := oauth.NewManager(oauth.ManagerConfig{Token: &oauth.Token{AccessToken: "old"}})
	a := &authorizer{mgr: mgr, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), authorize: func(context.Context, *oauth.ChallengeInfo) (*oauth.Token, *oauth.ClientRegistration, error) {
		calls.Add(1)
		mgr.SetToken(&oauth.Token{AccessToken: "new"})
		return nil, nil, nil
	}}
	if err := a.EnsureAuthorized(context.Background(), testChallenge("old")); err != nil {
		t.Fatalf("existing rejected token needs auth: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected authorization, got %d", n)
	}
	mgr.SetToken(nil)
	if err := a.EnsureAuthorized(context.Background(), testChallenge("")); err != nil {
		t.Fatalf("missing token should allow initial authorization: %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("expected second authorization after token removed, got %d", n)
	}
}

func TestAuthorizer_NoTokenChallengeAfterAnotherAuthorization(t *testing.T) {
	mgr := oauth.NewManager(oauth.ManagerConfig{})
	var calls atomic.Int32
	a := &authorizer{mgr: mgr, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), authorize: func(context.Context, *oauth.ChallengeInfo) (*oauth.Token, *oauth.ClientRegistration, error) {
		calls.Add(1)
		mgr.SetToken(&oauth.Token{AccessToken: "new"})
		return nil, nil, nil
	}}
	if err := a.EnsureAuthorized(context.Background(), testChallenge("")); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureAuthorized(context.Background(), testChallenge("")); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected one authorization for repeated no-token challenge, got %d", n)
	}
}

func TestAuthorizer_CurrentRejectedTokenCanReauthorizeAfterRecentFlow(t *testing.T) {
	mgr := oauth.NewManager(oauth.ManagerConfig{Token: &oauth.Token{AccessToken: "new"}})
	var calls atomic.Int32
	a := &authorizer{
		mgr: mgr, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		lastAuthorized: remote.CredentialFingerprint("Bearer new"),
		authorizedAt:   time.Now().Add(-time.Minute),
		authorize: func(context.Context, *oauth.ChallengeInfo) (*oauth.Token, *oauth.ClientRegistration, error) {
			calls.Add(1)
			mgr.SetToken(&oauth.Token{AccessToken: "replacement"})
			return nil, nil, nil
		},
	}
	if err := a.EnsureAuthorized(context.Background(), testChallenge("new")); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected expired prompt suppression to allow login, got %d calls", n)
	}
}

func TestAuthorizer_ForbiddenRequiresScopeChallenge(t *testing.T) {
	a := &authorizer{mgr: oauth.NewManager(oauth.ManagerConfig{}), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.EnsureAuthorized(ctx, &remote.AuthRequiredError{StatusCode: http.StatusForbidden}); !errors.Is(err, remote.ErrForbidden) {
		t.Fatalf("expected forbidden without scope challenge, got %v", err)
	}
}

func TestAuthorizer_PostAndEventStreamShareOneAuthorization(t *testing.T) {
	var oldRequests atomic.Int32
	bothChallenged := make(chan struct{})
	mgr := oauth.NewManager(oauth.ManagerConfig{Token: &oauth.Token{AccessToken: "old"}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer old" {
			if oldRequests.Add(1) == 2 {
				close(bothChallenged)
			}
			<-bothChallenged
			w.Header().Set("WWW-Authenticate", `Bearer realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer new" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	var authCalls atomic.Int32
	a := &authorizer{mgr: mgr, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), authorize: func(context.Context, *oauth.ChallengeInfo) (*oauth.Token, *oauth.ClientRegistration, error) {
		authCalls.Add(1)
		mgr.SetToken(&oauth.Token{AccessToken: "new"})
		return nil, nil, nil
	}}
	connector := remote.NewHTTPConnector(remote.HTTPConnectorConfig{URL: server.URL, Client: server.Client(), TokenSource: mgr, Authorizer: a})
	conn, err := connector.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	postDone := make(chan error, 1)
	go func() { postDone <- conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"tools/list","id":1}`)) }()
	getReq, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	getReq.Header.Set("Authorization", "Bearer old")
	resp, err := server.Client().Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	challenge := &remote.AuthRequiredError{StatusCode: resp.StatusCode, Headers: resp.Header, Phase: "server-event-stream", SentCredential: remote.CredentialFingerprint(resp.Request.Header.Get("Authorization"))}
	resp.Body.Close()
	if err := a.EnsureAuthorized(ctx, challenge); err != nil {
		t.Fatalf("GET authorization: %v", err)
	}
	if err := <-postDone; err != nil {
		t.Fatalf("POST retry: %v", err)
	}
	if n := authCalls.Load(); n != 1 {
		t.Fatalf("expected one browser flow for GET and POST, got %d", n)
	}
}

func TestDeriveCallbackPort_Stable(t *testing.T) {
	p1 := deriveCallbackPort("abc123")
	p2 := deriveCallbackPort("abc123")
	if p1 != p2 {
		t.Fatal("same hash must yield same port")
	}
	if p1 < derivedCallbackPortBase || p1 >= derivedCallbackPortBase+derivedCallbackPortSpan {
		t.Fatalf("port out of range: %d", p1)
	}
}

func TestDeriveCallbackPort_DifferentInputsStayInRange(t *testing.T) {
	p1 := deriveCallbackPort("hash1")
	p2 := deriveCallbackPort("hash2")
	if p1 < derivedCallbackPortBase || p1 >= derivedCallbackPortBase+derivedCallbackPortSpan {
		t.Fatalf("port out of range: %d", p1)
	}
	if p2 < derivedCallbackPortBase || p2 >= derivedCallbackPortBase+derivedCallbackPortSpan {
		t.Fatalf("port out of range: %d", p2)
	}
}
