package main

import (
	_ "embed"

	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/Silo-Server/silo-plugin-watchprovider-anilist/provider"
)

const manifestVersion = "0.1.1"

// version is set at build time with -ldflags "-X main.version=...".
var version = manifestVersion

//go:embed manifest.json
var manifestJSON []byte

func main() {
	runtime.ServeManifest(manifestJSON, version, runtime.CapabilityServers{
		WatchSyncProvider: provider.NewServer(nil),
	})
}
