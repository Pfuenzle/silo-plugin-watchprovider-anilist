# AniList watch-provider plugin for Silo

Connects Silo profiles to [AniList](https://anilist.co/) through Silo's `watch_sync_provider.v1` plugin contract.

## Capabilities

- Connects each Silo profile to AniList with authorization-code OAuth.
- Validates each profile's AniList token without storing it in plugin.
- Sends completed movie and episode watches to AniList immediately after Silo records completion.
- Updates AniList anime list progress from Silo's absolute episode number, including season boundaries.
- Uses authenticated `mediaListEntry` data to make repeated deliveries idempotent.

AniList list API stores current progress and completion, not playback sessions or separate watch-history records. Starts, pauses, and incomplete stops do not change AniList progress; completed stops and manual watched marks do.

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

The plugin builds against the `silo-plugin-sdk` watch-sync contract.

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
