package provider

import (
	"context"
	"net/http"
	"strings"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	capabilityID           = "anilist"
	configClientID         = "anilist.client_id"
	configClientSecret     = "anilist.client_secret"
	configGraphQLEndpoint  = "anilist.graphql_url"
	defaultGraphQLEndpoint = "https://graphql.anilist.co"
)

var (
	aniListAuthorizeURL = "https://anilist.co/api/v2/oauth/authorize"
	aniListTokenURL     = "https://anilist.co/api/v2/oauth/token"
)

const viewerQuery = `query { Viewer { id name } }`

const mediaSearchQuery = `query ($search: String!, $formats: [MediaFormat], $year: String) {
  Page(page: 1, perPage: 1) {
    media(type: ANIME, search: $search, format_in: $formats, startDate_like: $year, sort: [SEARCH_MATCH]) {
      id
      episodes
      format
      mediaListEntry { id status progress }
    }
  }
}`

const saveMediaListEntryMutation = `mutation ($mediaId: Int!, $status: MediaListStatus!, $progress: Int, $completedAt: FuzzyDateInput) {
  SaveMediaListEntry(mediaId: $mediaId, status: $status, progress: $progress, completedAt: $completedAt) {
    id
    status
    progress
  }
}`

type Server struct {
	pluginv1.UnimplementedWatchSyncProviderServer
	http          *http.Client
	tokenEndpoint string
}

func NewServer(httpClient *http.Client) *Server {
	return &Server{http: httpClient}
}

func (s *Server) ExchangeAPIKey(ctx context.Context, req *pluginv1.WatchSyncExchangeAPIKeyRequest) (*pluginv1.WatchSyncCredentialResponse, error) {
	client, fault := s.client(req.GetCapabilityId(), req.GetProviderConfig(), req.GetApiKey())
	if fault != nil {
		return &pluginv1.WatchSyncCredentialResponse{Fault: fault}, nil
	}
	account, fault := validateAccount(ctx, client)
	if fault != nil {
		return &pluginv1.WatchSyncCredentialResponse{Fault: fault}, nil
	}
	return &pluginv1.WatchSyncCredentialResponse{Credentials: credentials(req.GetApiKey()), Account: account}, nil
}

func (s *Server) InitAuthorize(_ context.Context, req *pluginv1.WatchSyncInitAuthorizeRequest) (*pluginv1.WatchSyncInitAuthorizeResponse, error) {
	settings, fault := initAuthorizationSettings(req)
	if fault != nil {
		return &pluginv1.WatchSyncInitAuthorizeResponse{Fault: fault}, nil
	}
	authorizationURL, fault := buildAuthorizationURL(settings.ClientID, settings.RedirectURI, req.GetState())
	if fault != nil {
		return &pluginv1.WatchSyncInitAuthorizeResponse{Fault: fault}, nil
	}
	return &pluginv1.WatchSyncInitAuthorizeResponse{AuthorizationUrl: authorizationURL}, nil
}

func (s *Server) ExchangeCode(ctx context.Context, req *pluginv1.WatchSyncExchangeCodeRequest) (*pluginv1.WatchSyncCredentialResponse, error) {
	settings, fault := exchangeAuthorizationSettings(req)
	if fault != nil {
		return &pluginv1.WatchSyncCredentialResponse{Fault: fault}, nil
	}
	if strings.TrimSpace(req.GetAuthorizationCode()) == "" {
		return &pluginv1.WatchSyncCredentialResponse{Fault: invalidRequestFault("AniList authorization code is required")}, nil
	}
	credentials, fault := s.exchangeAuthorizationCode(ctx, settings, req.GetAuthorizationCode())
	if fault != nil {
		return &pluginv1.WatchSyncCredentialResponse{Fault: fault}, nil
	}
	client, fault := s.client(req.GetCapabilityId(), req.GetProviderConfig(), credentials.GetAccessToken())
	if fault != nil {
		return &pluginv1.WatchSyncCredentialResponse{Fault: fault}, nil
	}
	account, fault := validateAccount(ctx, client)
	if fault != nil {
		return &pluginv1.WatchSyncCredentialResponse{Fault: fault}, nil
	}
	return &pluginv1.WatchSyncCredentialResponse{Credentials: credentials, Account: account}, nil
}

func (s *Server) RefreshCredentials(context.Context, *pluginv1.WatchSyncRefreshCredentialsRequest) (*pluginv1.WatchSyncCredentialResponse, error) {
	return nil, status.Error(codes.Unimplemented, "AniList does not issue refresh tokens")
}

func (s *Server) GetAccount(ctx context.Context, req *pluginv1.WatchSyncGetAccountRequest) (*pluginv1.WatchSyncGetAccountResponse, error) {
	client, fault := s.authenticatedClient(req.GetContext())
	if fault != nil {
		return &pluginv1.WatchSyncGetAccountResponse{Fault: fault}, nil
	}
	account, fault := validateAccount(ctx, client)
	return &pluginv1.WatchSyncGetAccountResponse{Account: account, Fault: fault}, nil
}

func (s *Server) ApplyEvents(ctx context.Context, req *pluginv1.WatchSyncApplyEventsRequest) (*pluginv1.WatchSyncApplyEventsResponse, error) {
	client, fault := s.authenticatedClient(req.GetContext())
	if fault != nil {
		return &pluginv1.WatchSyncApplyEventsResponse{Fault: fault}, nil
	}
	response := &pluginv1.WatchSyncApplyEventsResponse{Results: make([]*pluginv1.WatchSyncApplyResult, 0, len(req.GetEvents()))}
	for _, event := range req.GetEvents() {
		result, connectionFault := s.applyEvent(ctx, client, event)
		if connectionFault != nil {
			return &pluginv1.WatchSyncApplyEventsResponse{Fault: connectionFault}, nil
		}
		response.Results = append(response.Results, result)
	}
	return response, nil
}

func (s *Server) ListRemoteState(context.Context, *pluginv1.WatchSyncListRemoteStateRequest) (*pluginv1.WatchSyncListRemoteStateResponse, error) {
	return &pluginv1.WatchSyncListRemoteStateResponse{Fault: invalidRequestFault("AniList import is not advertised by this provider")}, nil
}

func (s *Server) applyEvent(ctx context.Context, client *apiClient, event *pluginv1.WatchSyncEvent) (*pluginv1.WatchSyncApplyResult, *pluginv1.WatchSyncFault) {
	if event == nil || strings.TrimSpace(event.GetEventId()) == "" {
		return rejectedResult("", "Watch event ID is required"), nil
	}
	target, fault := targetFromEvent(event)
	if fault != nil {
		return resultFromFault(event.GetEventId(), fault), nil
	}
	media, requestFault := findMedia(ctx, client, target)
	if requestFault != nil {
		if connectionWide(requestFault) {
			return nil, requestFault
		}
		return resultFromFault(event.GetEventId(), requestFault), nil
	}
	if media.ID == 0 {
		return resultFromFault(event.GetEventId(), invalidRequestFault("AniList media could not be matched")), nil
	}
	status := desiredStatus(target, media)
	if alreadyApplied(media.MediaListEntry, status, target.Progress) {
		return &pluginv1.WatchSyncApplyResult{EventId: event.GetEventId(), Status: pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_NO_CHANGE}, nil
	}
	if requestFault := saveMediaListEntry(ctx, client, media.ID, status, target); requestFault != nil {
		if connectionWide(requestFault) {
			return nil, requestFault
		}
		return resultFromFault(event.GetEventId(), requestFault), nil
	}
	return &pluginv1.WatchSyncApplyResult{EventId: event.GetEventId(), Status: pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_APPLIED}, nil
}

func targetFromEvent(event *pluginv1.WatchSyncEvent) (scrobbleTarget, *pluginv1.WatchSyncFault) {
	completedScrobble := event.GetCompletionPercent() >= 100
	if event.GetOperation() != pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_MARK_WATCHED && (event.GetOperation() != pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_SCROBBLE_STOP || !completedScrobble) {
		return scrobbleTarget{}, invalidRequestFault("AniList supports completed watch events only")
	}
	media := event.GetMedia()
	if media == nil {
		return scrobbleTarget{}, invalidRequestFault("Watch event media is required")
	}
	target := scrobbleTarget{CompletedAt: dateFromTimestamp(event.GetOccurredAt())}
	switch media.GetMediaType() {
	case pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_MOVIE:
		target.Search = firstNonEmpty(media.GetTitle(), media.GetOriginalTitle())
		target.Year = media.GetYear()
		target.Formats = []string{"MOVIE"}
		target.Progress = 1
		target.Final = true
	case pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_EPISODE:
		target.Search = firstNonEmpty(media.GetSeriesTitle(), media.GetTitle(), media.GetOriginalTitle())
		target.Year = media.GetSeriesYear()
		target.Formats = []string{"TV", "TV_SHORT", "ONA", "OVA", "SPECIAL"}
		target.Progress = media.GetAbsoluteEpisodeNumber()
		if target.Progress <= 0 {
			if media.GetSeasonNumber() != 1 {
				return scrobbleTarget{}, invalidRequestFault("Episode events outside season one require an absolute episode number")
			}
			target.Progress = media.GetEpisodeNumber()
		}
		if target.Progress <= 0 {
			return scrobbleTarget{}, invalidRequestFault("Episode events require an episode number")
		}
		if media.GetSeriesEpisodeCount() > 0 && target.Progress >= media.GetSeriesEpisodeCount() {
			target.Final = true
		}
	default:
		return scrobbleTarget{}, invalidRequestFault("AniList supports movie and episode events only")
	}
	if strings.TrimSpace(target.Search) == "" {
		return scrobbleTarget{}, invalidRequestFault("Watch event needs a title for AniList matching")
	}
	return target, nil
}
