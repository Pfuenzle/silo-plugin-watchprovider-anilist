package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInitAuthorizeBuildsAniListAuthorizationURL(t *testing.T) {
	t.Parallel()
	response, err := NewServer(nil).InitAuthorize(context.Background(), &pluginv1.WatchSyncInitAuthorizeRequest{
		CapabilityId:   capabilityID,
		ProviderConfig: oauthProviderConfig(defaultGraphQLEndpoint),
		RedirectUri:    "https://silo.example/callback",
		State:          "host-state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetFault() != nil {
		t.Fatalf("fault = %v", response.GetFault())
	}
	parsed, err := url.Parse(response.GetAuthorizationUrl())
	if err != nil {
		t.Fatalf("parse authorization url: %v", err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != aniListAuthorizeURL {
		t.Fatalf("authorization endpoint = %s", parsed.String())
	}
	values := parsed.Query()
	if values.Get("client_id") != "12345" || values.Get("redirect_uri") != "https://silo.example/callback" || values.Get("response_type") != "code" || values.Get("state") != "host-state" {
		t.Fatalf("query = %#v", values)
	}
	if len(response.GetProviderState()) != 0 {
		t.Fatalf("provider state = %q", response.GetProviderState())
	}
}

func TestExchangeCodeRequestsTokenAndValidatesAccount(t *testing.T) {
	tokenCalls := 0
	viewerCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls++
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("token request = %s content-type %q", r.Method, r.Header.Get("Content-Type"))
			}
			payload := decodeJSONBody[map[string]string](t, r)
			if payload["grant_type"] != "authorization_code" || payload["client_id"] != "12345" || payload["client_secret"] != "client-secret" || payload["redirect_uri"] != "https://silo.example/callback" || payload["code"] != "auth-code" {
				t.Fatalf("token payload = %#v", payload)
			}
			writeJSON(t, w, map[string]any{"access_token": "oauth-token", "token_type": "Bearer", "expires_in": 3600})
		case "/graphql":
			viewerCalls++
			if r.Header.Get("Authorization") != "Bearer oauth-token" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			assertGraphQLQuery(t, r, "Viewer")
			writeJSON(t, w, map[string]any{"data": map[string]any{"Viewer": map[string]any{"id": 42, "name": "mari"}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	server := NewServer(upstream.Client())
	server.tokenEndpoint = upstream.URL + "/oauth/token"

	before := time.Now()
	response, err := server.ExchangeCode(context.Background(), &pluginv1.WatchSyncExchangeCodeRequest{
		CapabilityId:      capabilityID,
		ProviderConfig:    oauthProviderConfig(upstream.URL + "/graphql"),
		RedirectUri:       "https://silo.example/callback",
		AuthorizationCode: "auth-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetFault() != nil {
		t.Fatalf("fault = %v", response.GetFault())
	}
	credentials := response.GetCredentials()
	if credentials.GetAccessToken() != "oauth-token" || credentials.GetTokenType() != "Bearer" || credentials.GetRefreshToken() != "" || len(credentials.GetScopes()) != 0 || len(credentials.GetSecretAttributes()) != 0 {
		t.Fatalf("credentials = %#v", credentials)
	}
	expiresAt := credentials.GetExpiresAt().AsTime()
	if expiresAt.Before(before.Add(3599*time.Second)) || expiresAt.After(time.Now().Add(3601*time.Second)) {
		t.Fatalf("expires_at = %s", expiresAt)
	}
	if response.GetAccount().GetExternalSubject() != "42" || response.GetAccount().GetUsername() != "mari" {
		t.Fatalf("account = %#v", response.GetAccount())
	}
	if tokenCalls != 1 || viewerCalls != 1 {
		t.Fatalf("calls = token %d viewer %d", tokenCalls, viewerCalls)
	}
}

func TestExchangeCodeRejectsMissingClientSecret(t *testing.T) {
	t.Parallel()
	config := oauthProviderConfig(defaultGraphQLEndpoint)
	delete(config.SecretValues, configClientSecret)
	response, err := NewServer(nil).ExchangeCode(context.Background(), &pluginv1.WatchSyncExchangeCodeRequest{
		CapabilityId:      capabilityID,
		ProviderConfig:    config,
		RedirectUri:       "https://silo.example/callback",
		AuthorizationCode: "auth-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSafeFault(t, response.GetFault(), "client-secret", "auth-code")
}

func TestExchangeCodeReturnsSafeFaultForRejectedToken(t *testing.T) {
	upstreamBody := `{"error":"invalid_grant","error_description":"bad auth-code for client-secret"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		if _, err := w.Write([]byte(upstreamBody)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer upstream.Close()
	server := NewServer(upstream.Client())
	server.tokenEndpoint = upstream.URL

	response, err := server.ExchangeCode(context.Background(), &pluginv1.WatchSyncExchangeCodeRequest{
		CapabilityId:      capabilityID,
		ProviderConfig:    oauthProviderConfig(defaultGraphQLEndpoint),
		RedirectUri:       "https://silo.example/callback",
		AuthorizationCode: "auth-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSafeFault(t, response.GetFault(), "auth-code", "client-secret", upstreamBody)
}

func TestRefreshCredentialsReturnsUnimplemented(t *testing.T) {
	t.Parallel()
	_, err := NewServer(nil).RefreshCredentials(context.Background(), &pluginv1.WatchSyncRefreshCredentialsRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("code = %s, err = %v", status.Code(err), err)
	}
}
