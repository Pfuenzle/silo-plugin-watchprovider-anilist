package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestExchangeAPIKeyValidatesAniListToken(t *testing.T) {
	t.Parallel()
	var authorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		assertGraphQLQuery(t, r, "Viewer")
		writeJSON(t, w, map[string]any{"data": map[string]any{"Viewer": map[string]any{"id": 42, "name": "mari"}}})
	}))
	defer upstream.Close()

	response, err := NewServer(upstream.Client()).ExchangeAPIKey(context.Background(), &pluginv1.WatchSyncExchangeAPIKeyRequest{
		CapabilityId:   capabilityID,
		ProviderConfig: providerConfig(upstream.URL),
		ApiKey:         "secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetFault() != nil {
		t.Fatalf("fault = %v", response.GetFault())
	}
	if authorization != "Bearer secret-token" {
		t.Fatalf("Authorization = %q", authorization)
	}
	if response.GetCredentials().GetAccessToken() != "secret-token" || response.GetCredentials().GetTokenType() != "Bearer" {
		t.Fatalf("credentials = %#v", response.GetCredentials())
	}
	if response.GetAccount().GetExternalSubject() != "42" || response.GetAccount().GetUsername() != "mari" {
		t.Fatalf("account = %#v", response.GetAccount())
	}
}

func TestApplyWatchedMovieCompletesAniListEntry(t *testing.T) {
	t.Parallel()
	occurredAt := time.Date(2026, time.August, 5, 12, 34, 56, 0, time.UTC)
	mutations := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := decodeGraphQL(t, r)
		if strings.Contains(payload.Query, "SaveMediaListEntry") {
			mutations++
			if payload.Variables["mediaId"] != float64(15125) || payload.Variables["status"] != "COMPLETED" {
				t.Fatalf("variables = %#v", payload.Variables)
			}
			completedAt := payload.Variables["completedAt"].(map[string]any)
			if completedAt["year"] != float64(2026) || completedAt["month"] != float64(8) || completedAt["day"] != float64(5) {
				t.Fatalf("completedAt = %#v", completedAt)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"SaveMediaListEntry": map[string]any{"id": 7, "status": "COMPLETED", "progress": 1}}})
			return
		}
		formats := payload.Variables["formats"].([]any)
		if payload.Variables["search"] != "The Movie" || len(formats) != 1 || formats[0] != "MOVIE" || payload.Variables["year"] != "2026%" {
			t.Fatalf("variables = %#v", payload.Variables)
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"Page": map[string]any{"media": []any{map[string]any{
			"id": 15125, "episodes": 1, "format": "MOVIE", "mediaListEntry": nil,
		}}}}})
	}))
	defer upstream.Close()

	response, err := NewServer(upstream.Client()).ApplyEvents(context.Background(), &pluginv1.WatchSyncApplyEventsRequest{
		Context: authenticatedContext(upstream.URL),
		Events: []*pluginv1.WatchSyncEvent{{
			EventId: "history-1", Operation: pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_MARK_WATCHED,
			OccurredAt: timestamppb.New(occurredAt),
			Media:      &pluginv1.WatchSyncMedia{MediaType: pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_MOVIE, Title: "The Movie", Year: 2026},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetFault() != nil || len(response.GetResults()) != 1 || response.GetResults()[0].GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_APPLIED {
		t.Fatalf("response = %#v", response)
	}
	if mutations != 1 {
		t.Fatalf("mutations = %d, want 1", mutations)
	}
}

func TestApplyWatchedEpisodeUpdatesSeriesProgress(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := decodeGraphQL(t, r)
		if strings.Contains(payload.Query, "SaveMediaListEntry") {
			if payload.Variables["mediaId"] != float64(1) || payload.Variables["status"] != "CURRENT" || payload.Variables["progress"] != float64(5) {
				t.Fatalf("variables = %#v", payload.Variables)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"SaveMediaListEntry": map[string]any{"id": 9, "status": "CURRENT", "progress": 5}}})
			return
		}
		formats := payload.Variables["formats"].([]any)
		if payload.Variables["search"] != "Cowboy Bebop" || len(formats) != 5 || formats[0] != "TV" || payload.Variables["year"] != "1998%" {
			t.Fatalf("variables = %#v", payload.Variables)
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"Page": map[string]any{"media": []any{map[string]any{
			"id": 1, "episodes": 26, "format": "TV", "mediaListEntry": map[string]any{"id": 3, "status": "CURRENT", "progress": 4},
		}}}}})
	}))
	defer upstream.Close()

	response, err := NewServer(upstream.Client()).ApplyEvents(context.Background(), &pluginv1.WatchSyncApplyEventsRequest{
		Context: authenticatedContext(upstream.URL),
		Events: []*pluginv1.WatchSyncEvent{{
			EventId: "episode-5", Operation: pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_SCROBBLE_STOP, CompletionPercent: 100,
			Media: &pluginv1.WatchSyncMedia{MediaType: pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_EPISODE,
				SeriesTitle: "Cowboy Bebop", SeriesYear: 1998, EpisodeNumber: 5},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetFault() != nil || response.GetResults()[0].GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_APPLIED {
		t.Fatalf("response = %#v", response)
	}
}

func TestApplyWatchedEpisodeIsNoChangeWhenProgressAlreadyCoversEpisode(t *testing.T) {
	t.Parallel()
	mutations := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := decodeGraphQL(t, r)
		if strings.Contains(payload.Query, "SaveMediaListEntry") {
			mutations++
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"Page": map[string]any{"media": []any{map[string]any{
			"id": 1, "episodes": 26, "format": "TV", "mediaListEntry": map[string]any{"id": 3, "status": "CURRENT", "progress": 5},
		}}}}})
	}))
	defer upstream.Close()

	response, err := NewServer(upstream.Client()).ApplyEvents(context.Background(), &pluginv1.WatchSyncApplyEventsRequest{
		Context: authenticatedContext(upstream.URL),
		Events: []*pluginv1.WatchSyncEvent{{
			EventId: "episode-5", Operation: pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_MARK_WATCHED,
			Media: &pluginv1.WatchSyncMedia{MediaType: pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_EPISODE,
				SeriesTitle: "Cowboy Bebop", EpisodeNumber: 5},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetFault() != nil || response.GetResults()[0].GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_NO_CHANGE {
		t.Fatalf("response = %#v", response)
	}
	if mutations != 0 {
		t.Fatalf("mutations = %d, want 0", mutations)
	}
}

func TestIncompletePlaybackIsRejectedBecauseAniListHasNoLiveScrobble(t *testing.T) {
	t.Parallel()
	response, err := NewServer(nil).ApplyEvents(context.Background(), &pluginv1.WatchSyncApplyEventsRequest{
		Context: authenticatedContext(defaultGraphQLEndpoint),
		Events: []*pluginv1.WatchSyncEvent{{
			EventId: "playback-1", Operation: pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_SCROBBLE_STOP,
			Media: &pluginv1.WatchSyncMedia{MediaType: pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_MOVIE, Title: "The Movie"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetResults()[0].GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_REJECTED {
		t.Fatalf("response = %#v", response)
	}
}

func TestGraphQLRateLimitIsConnectionWide(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(t, w, map[string]any{"errors": []any{map[string]any{"message": "Too Many Requests.", "status": 429}}})
	}))
	defer upstream.Close()

	response, err := NewServer(upstream.Client()).ApplyEvents(context.Background(), &pluginv1.WatchSyncApplyEventsRequest{
		Context: authenticatedContext(upstream.URL),
		Events: []*pluginv1.WatchSyncEvent{{
			EventId: "history-1", Operation: pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_MARK_WATCHED,
			Media: &pluginv1.WatchSyncMedia{MediaType: pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_MOVIE, Title: "The Movie"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetFault().GetCode() != pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED || response.GetFault().GetRetryAfter().AsDuration() != 30*time.Second {
		t.Fatalf("response = %#v", response)
	}
}

func TestDefaultClientUsesAniListEndpoint(t *testing.T) {
	t.Parallel()
	client, err := newAPIClient("", "token", nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.endpoint.String() != defaultGraphQLEndpoint || client.http.Timeout != defaultRequestTimeout {
		t.Fatalf("client = %#v", client)
	}
}

func TestEndpointRejectsEmbeddedCredentials(t *testing.T) {
	t.Parallel()
	_, err := newAPIClient("https://user:pass@example.com/graphql", "token", nil)
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("err = %v", err)
	}
}

func TestEndpointRejectsPlainHTTPOutsideLocalhost(t *testing.T) {
	t.Parallel()
	_, err := newAPIClient("http://anilist.example.com/graphql", "token", nil)
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v", err)
	}
}
