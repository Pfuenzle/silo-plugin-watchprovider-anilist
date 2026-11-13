package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func validateAccount(ctx context.Context, client *apiClient) (*pluginv1.WatchSyncAccount, *pluginv1.WatchSyncFault) {
	var response graphQLResponse
	if fault := client.graphQL(ctx, viewerQuery, nil, &response); fault != nil {
		return nil, fault
	}
	viewer := response.Data.Viewer
	if viewer == nil || viewer.ID <= 0 || strings.TrimSpace(viewer.Name) == "" {
		return nil, &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL, SafeMessage: "AniList did not validate the access token"}
	}
	return &pluginv1.WatchSyncAccount{ExternalSubject: fmt.Sprint(viewer.ID), Username: viewer.Name, DisplayName: viewer.Name}, nil
}
func findMedia(ctx context.Context, client *apiClient, target *scrobbleTarget) (aniListMedia, *pluginv1.WatchSyncFault) {
	if target.SeriesTVDBID != "" {
		_, _ = lookupXEM(ctx, client.http, defaultXEMEndpoint, target.SeriesTVDBID, target.SeasonNumber, target.Progress)
	}
	var response graphQLResponse
	variables := map[string]any{"search": target.Search, "formats": target.Formats, "year": yearLike(target.Year)}
	if fault := client.graphQL(ctx, mediaSearchQuery, variables, &response); fault != nil {
		return aniListMedia{}, fault
	}
	season := target.SeasonNumber
	var selected aniListMedia
	if len(target.Formats) == 1 && target.Formats[0] == "MOVIE" {
		selected = firstMedia(response.Data.Page.Media)
	} else {
		selected = selectSeason(response.Data.Page.Media, season, target.Progress)
	}
	if selected.ID != 0 {
		if !(len(target.Formats) == 1 && target.Formats[0] == "MOVIE") {
			target.Progress = selectedProgress(response.Data.Page.Media, season, target.Progress, selected)
		}
		return selected, nil
	}
	for _, search := range []string{target.Search, normalizeAnimeTitle(target.Search)} {
		response = graphQLResponse{}
		if fault := client.graphQL(ctx, mediaFallbackQuery, map[string]any{"search": search}, &response); fault != nil {
			return aniListMedia{}, fault
		}
		selected = selectSeason(response.Data.Page.Media, season, target.Progress)
		if selected.ID != 0 {
			target.Progress = selectedProgress(response.Data.Page.Media, season, target.Progress, selected)
			return selected, nil
		}
	}
	return aniListMedia{}, nil
}
func lookupXEM(ctx context.Context, httpClient *http.Client, endpointURL, tvdbID string, season, episode int32) (xemMapping, bool) {
	endpoint, err := url.Parse(endpointURL)
	if err != nil {
		return xemMapping{}, false
	}
	query := endpoint.Query()
	query.Set("id", tvdbID)
	query.Set("origin", "tvdb")
	query.Set("season", strconv.FormatInt(int64(season), 10))
	query.Set("episode", strconv.FormatInt(int64(episode), 10))
	query.Set("destination", "anidb")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return xemMapping{}, false
	}
	request.Header.Set("User-Agent", "Silo-AniList/0.3")
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return xemMapping{}, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return xemMapping{}, false
	}
	var payload xemMappingResponse
	if json.NewDecoder(response.Body).Decode(&payload) != nil || payload.Result != "success" {
		return xemMapping{}, false
	}
	mapping, ok := payload.Data["anidb"]
	return mapping, ok
}

func firstMedia(media []aniListMedia) aniListMedia {
	if len(media) == 0 {
		return aniListMedia{}
	}
	return media[0]
}
func selectSeason(media []aniListMedia, seasonNumber, episodeNumber int32) aniListMedia {
	if len(media) == 0 {
		return aniListMedia{}
	}
	tv := make([]aniListMedia, 0, len(media))
	for _, candidate := range media {
		if candidate.Format == "TV" {
			tv = append(tv, candidate)
		}
	}
	if len(tv) == 0 {
		return aniListMedia{}
	}
	start := int(seasonNumber - 1)
	if start < 0 {
		start = 0
	}
	if start >= len(tv) {
		return aniListMedia{}
	}
	if episodeNumber <= 0 {
		return tv[start]
	}
	for i := start; i < len(tv); i++ {
		if tv[i].Episodes <= 0 || episodeNumber <= episodeOffset(tv, start, i)+tv[i].Episodes {
			return tv[i]
		}
	}
	return aniListMedia{}
}

func episodeOffset(media []aniListMedia, start, index int) int32 {
	var offset int32
	for i := start; i < index; i++ {
		if media[i].Episodes > 0 {
			offset += int32(media[i].Episodes)
		}
	}
	return offset
}

func selectedProgress(media []aniListMedia, seasonNumber, episodeNumber int32, selected aniListMedia) int32 {
	start := int(seasonNumber - 1)
	if start < 0 {
		start = 0
	}
	for i := start; i < len(media); i++ {
		if media[i].ID == selected.ID {
			return episodeNumber - episodeOffset(media, start, i)
		}
	}
	return episodeNumber
}

func yearLike(year int32) string {
	if year <= 0 {
		return ""
	}
	return fmt.Sprintf("%d%%", year)
}

func normalizeAnimeTitle(title string) string {
	return strings.NewReplacer(".", " ", ":", " ", "-", " ").Replace(title)
}

func saveMediaListEntry(ctx context.Context, client *apiClient, mediaID int, status string, target *scrobbleTarget) *pluginv1.WatchSyncFault {
	variables := map[string]any{"mediaId": mediaID, "status": status, "progress": target.Progress, "completedAt": target.CompletedAt}
	var response graphQLResponse
	return client.graphQL(ctx, saveMediaListEntryMutation, variables, &response)
}

func desiredStatus(target *scrobbleTarget, media aniListMedia) string {
	if target.Final || (media.Episodes > 0 && target.Progress >= int32(media.Episodes)) {
		return "COMPLETED"
	}
	return "CURRENT"
}

func alreadyApplied(entry *aniListListEntry, status string, progress int32) bool {
	if entry == nil {
		return false
	}
	if status == "COMPLETED" {
		return entry.Status == "COMPLETED"
	}
	return entry.Progress >= progress && (entry.Status == "CURRENT" || entry.Status == "COMPLETED")
}

func dateFromTimestamp(value *timestamppb.Timestamp) *fuzzyDate {
	if value == nil || value.CheckValid() != nil {
		return nil
	}
	date := value.AsTime().UTC()
	return &fuzzyDate{Year: date.Year(), Month: int(date.Month()), Day: date.Day()}
}

func (s *Server) authenticatedClient(auth *pluginv1.WatchSyncAuthenticatedContext) (*apiClient, *pluginv1.WatchSyncFault) {
	if auth == nil || auth.GetCredentials() == nil {
		return nil, invalidRequestFault("AniList credentials are required")
	}
	return s.client(auth.GetCapabilityId(), auth.GetProviderConfig(), auth.GetCredentials().GetAccessToken())
}

func (s *Server) client(requestedCapability string, config *pluginv1.WatchSyncProviderConfig, token string) (*apiClient, *pluginv1.WatchSyncFault) {
	if requestedCapability != capabilityID {
		return nil, invalidRequestFault("Unknown AniList capability")
	}
	if strings.TrimSpace(token) == "" {
		return nil, invalidRequestFault("AniList access token is required")
	}
	client, err := newAPIClient(endpointFromConfig(config), token, s.http)
	if err != nil {
		return nil, invalidRequestFault(err.Error())
	}
	client.gate = s.gate
	return client, nil
}

func endpointFromConfig(config *pluginv1.WatchSyncProviderConfig) string {
	if config == nil {
		return ""
	}
	if value := strings.TrimSpace(config.GetValues()[configGraphQLEndpoint]); value != "" {
		return value
	}
	return strings.TrimSpace(config.GetSecretValues()[configGraphQLEndpoint])
}

func credentials(token string) *pluginv1.WatchSyncCredentials {
	return &pluginv1.WatchSyncCredentials{AccessToken: strings.TrimSpace(token), TokenType: "Bearer"}
}

func cloneCredentials(value *pluginv1.WatchSyncCredentials) *pluginv1.WatchSyncCredentials {
	if value == nil {
		return nil
	}
	return &pluginv1.WatchSyncCredentials{AccessToken: value.GetAccessToken(), RefreshToken: value.GetRefreshToken(), ExpiresAt: value.GetExpiresAt(), TokenType: value.GetTokenType(), Scopes: append([]string(nil), value.GetScopes()...), SecretAttributes: cloneMap(value.GetSecretAttributes())}
}

func invalidRequestFault(message string) *pluginv1.WatchSyncFault {
	return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_REQUEST, SafeMessage: message}
}

func connectionWide(fault *pluginv1.WatchSyncFault) bool {
	return fault.GetCode() == pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL || fault.GetCode() == pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED
}

func resultFromFault(eventID string, fault *pluginv1.WatchSyncFault) *pluginv1.WatchSyncApplyResult {
	status := pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_REJECTED
	if fault.GetCode() == pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY || fault.GetCode() == pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED {
		status = pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_RETRY
	}
	return &pluginv1.WatchSyncApplyResult{EventId: eventID, Status: status, Fault: fault}
}

func rejectedResult(eventID, message string) *pluginv1.WatchSyncApplyResult {
	return resultFromFault(eventID, invalidRequestFault(message))
}

func cloneMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
