package main

import (
	"encoding/json"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
)

func TestManifestDeclaresAniListAuthorizationCodeProvider(t *testing.T) {
	t.Parallel()
	parsed, err := manifest.Load(manifestJSON)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	capabilities := parsed.GetCapabilities()
	if len(capabilities) != 1 {
		t.Fatalf("capabilities = %d, want 1", len(capabilities))
	}
	capability := capabilities[0]
	if capability.GetId() != "anilist" || capability.GetDisplayName() != "AniList" {
		t.Fatalf("capability = %#v", capability)
	}
	if descriptor := capability.GetWatchSyncProvider(); descriptor == nil || !descriptor.GetExportWatched() || descriptor.GetImportWatched() || descriptor.GetImportProgress() || descriptor.GetScrobblePlayback() {
		t.Fatalf("watch sync descriptor = %#v", descriptor)
	}
	descriptor := capability.GetWatchSyncProvider()
	if methods := descriptor.GetAuthMethods(); len(methods) != 1 || methods[0] != pluginv1.WatchSyncAuthMethod_WATCH_SYNC_AUTH_METHOD_AUTHORIZATION_CODE {
		t.Fatalf("auth methods = %#v", methods)
	}
	schemas := capability.GetConfigSchema()
	if len(schemas) != 0 {
		t.Fatalf("connection config schemas = %#v", schemas)
	}
	globalSchemas := parsed.GetGlobalConfigSchema()
	if len(globalSchemas) != 1 {
		t.Fatalf("global config schemas = %#v", globalSchemas)
	}
	assertOAuthGlobalConfigSchema(t, globalSchemas[0])
}

func TestManifestVersionMatchesRuntimeDefault(t *testing.T) {
	t.Parallel()
	parsed, err := manifest.Load(manifestJSON)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	runtimeManifest, err := manifest.LoadWithChecksum(manifestJSON, version)
	if err != nil {
		t.Fatalf("load runtime manifest: %v", err)
	}
	if parsed.GetVersion() != manifestVersion || runtimeManifest.GetVersion() != manifestVersion {
		t.Fatalf("manifest version = %q runtime version = %q, want %q", parsed.GetVersion(), runtimeManifest.GetVersion(), manifestVersion)
	}
}

func assertOAuthGlobalConfigSchema(t *testing.T, schema *pluginv1.ConfigSchema) {
	t.Helper()
	if schema.GetKey() != "anilist" || !schema.GetRequired() {
		t.Fatalf("global config schema = %#v", schema)
	}
	var decoded struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(schema.GetJsonSchema()), &decoded); err != nil {
		t.Fatalf("decode json schema: %v", err)
	}
	if decoded.Properties["client_id"].Type != "string" || decoded.Properties["client_secret"].Type != "string" {
		t.Fatalf("properties = %#v", decoded.Properties)
	}
	if len(decoded.Required) != 2 || decoded.Required[0] != "client_id" || decoded.Required[1] != "client_secret" {
		t.Fatalf("required = %#v", decoded.Required)
	}
	fields := schema.GetAdminForm().GetFields()
	if len(fields) != 2 {
		t.Fatalf("fields = %#v", fields)
	}
	if fields[0].GetKey() != "client_id" || fields[0].GetSecret() {
		t.Fatalf("client id field = %#v", fields[0])
	}
	if fields[1].GetKey() != "client_secret" || !fields[1].GetSecret() {
		t.Fatalf("client secret field = %#v", fields[1])
	}
}
