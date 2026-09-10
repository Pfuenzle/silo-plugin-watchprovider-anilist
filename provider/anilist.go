package provider

import (
	"context"
	"fmt"
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

func findMedia(ctx context.Context, client *apiClient, target scrobbleTarget) (aniListMedia, *pluginv1.WatchSyncFault) {
	variables := map[string]any{"search": target.Search, "formats": nil, "year": nil}
	if len(target.Formats) > 0 {
		variables["formats"] = target.Formats
	}
	if target.Year > 0 {
		variables["year"] = fmt.Sprintf("%d%%", target.Year)
	}
	var response graphQLResponse
	if fault := client.graphQL(ctx, mediaSearchQuery, variables, &response); fault != nil {
		return aniListMedia{}, fault
	}
	if len(response.Data.Page.Media) == 0 {
		return aniListMedia{}, nil
	}
	return response.Data.Page.Media[0], nil
}

func saveMediaListEntry(ctx context.Context, client *apiClient, mediaID int, status string, target scrobbleTarget) *pluginv1.WatchSyncFault {
	variables := map[string]any{"mediaId": mediaID, "status": status, "progress": target.Progress, "completedAt": target.CompletedAt}
	var response graphQLResponse
	return client.graphQL(ctx, saveMediaListEntryMutation, variables, &response)
}

func desiredStatus(target scrobbleTarget, media aniListMedia) string {
	if target.Final || (media.Episodes > 0 && target.Progress >= media.Episodes) {
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
