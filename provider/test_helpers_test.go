package provider

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
)

func providerConfig(endpoint string) *pluginv1.WatchSyncProviderConfig {
	return &pluginv1.WatchSyncProviderConfig{Values: map[string]string{configGraphQLEndpoint: endpoint}}
}

func oauthProviderConfig(graphQLEndpoint string) *pluginv1.WatchSyncProviderConfig {
	return &pluginv1.WatchSyncProviderConfig{
		Values:       map[string]string{configGraphQLEndpoint: graphQLEndpoint, configClientID: "12345"},
		SecretValues: map[string]string{configClientSecret: "client-secret"},
	}
}

func authenticatedContext(endpoint string) *pluginv1.WatchSyncAuthenticatedContext {
	return &pluginv1.WatchSyncAuthenticatedContext{
		CapabilityId:   capabilityID,
		ProviderConfig: providerConfig(endpoint),
		Credentials:    credentials("token"),
	}
}

type capturedGraphQL struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func decodeGraphQL(t *testing.T, r *http.Request) capturedGraphQL {
	t.Helper()
	if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		t.Fatalf("request = %s authorization %q", r.Method, r.Header.Get("Authorization"))
	}
	var payload capturedGraphQL
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return payload
}

func assertGraphQLQuery(t *testing.T, r *http.Request, want string) {
	t.Helper()
	payload := decodeGraphQL(t, r)
	if !strings.Contains(payload.Query, want) {
		t.Fatalf("query = %q, want %q", payload.Query, want)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func assertSafeFault(t *testing.T, fault *pluginv1.WatchSyncFault, forbidden ...string) {
	t.Helper()
	if fault == nil {
		t.Fatal("fault is nil")
	}
	message := fault.GetSafeMessage()
	if strings.TrimSpace(message) == "" {
		t.Fatal("safe message is empty")
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(message, value) {
			t.Fatalf("safe message %q contains forbidden value %q", message, value)
		}
	}
}

func decodeJSONBody[T any](t *testing.T, r *http.Request) T {
	t.Helper()
	var payload T
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r.Body); err != nil {
		t.Fatalf("read request body: %v", err)
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return payload
}
