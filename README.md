# AniList watch-provider plugin for Silo

Connects Silo profiles to [AniList](https://anilist.co/) through Silo's `watch_sync_provider.v1` plugin contract.

## Capabilities

- Connects each Silo profile to AniList with the authorization-code OAuth flow.
- Validates each profile's AniList access token without persisting it in the plugin.
- Exports completed anime movie watches as completed AniList media-list entries.
- Exports completed episode watches as AniList series progress.
- Uses Silo's absolute episode number across season boundaries. A season-one episode may use its local number; later seasons and specials require an absolute number and are rejected when catalog ordering is unknown.
- Uses AniList's authenticated `mediaListEntry` data to keep repeated Silo deliveries idempotent.

The plugin deliberately does not advertise imports, favorites, watchlists, unwatched export, or live playback scrobbling. AniList's list API can update anime media-list entries, but it does not expose Silo-compatible watch-history pages, durable resume records, or per-playback session state. Silo only enables capabilities a plugin explicitly advertises.

## Setup

1. Install the plugin.
2. Create an AniList OAuth application.
3. Set the AniList application redirect URL to `https://YOUR-SILO-HOST/api/v2/watch-providers/PROVIDER-KEY/auth/callback`, replacing `PROVIDER-KEY` with the installed provider key displayed by Silo (for example, `plugin:4:anilist`). Include your deployment's URL prefix if it has one. The installation ID varies between deployments.
4. Configure the plugin global setting `anilist.client_id` with the OAuth client ID.
5. Configure the plugin secret global setting `anilist.client_secret` with the OAuth client secret.
6. In Silo's watch-provider settings, connect each profile through AniList's authorization page.

The returned access token is profile-scoped connection data encrypted and owned by the Silo host. The plugin receives it only during a watch-sync RPC and never stores it. AniList does not issue refresh tokens for this flow, so expired credentials must be reconnected by the host.

Silo computes absolute episode numbers from regular catalog episodes in season order, excluding specials. Gaps in earlier seasons or episode numbering leave the absolute number unknown; the plugin then rejects later-season progress instead of treating season two episode one as episode one. A contiguous catalog also supplies the regular series episode count for completion. This count reflects known catalog rows, so keep series metadata complete.

The plugin uses AniList OAuth:

- `GET https://anilist.co/api/v2/oauth/authorize`
- `POST https://anilist.co/api/v2/oauth/token`

The plugin uses AniList GraphQL:

- `POST https://graphql.anilist.co`
- `Viewer` token validation query
- `Media` search query for anime titles
- `SaveMediaListEntry` mutation for status and progress updates

## Development

The plugin currently pins `silo-plugin-sdk` v0.13.2 in `go.mod`.

```bash
make test
make build
./plugin manifest
```

`make build-all` produces static binaries for the platforms declared in `manifest.json`.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Changes to
authentication, reconciliation, idempotency, or the watch-sync contract should
start as an issue.

## License

AGPL-3.0-only.
