package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type authorizationConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

func (s *Server) oauthTokenEndpoint() string {
	if strings.TrimSpace(s.tokenEndpoint) != "" {
		return s.tokenEndpoint
	}
	return aniListTokenURL
}

type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
	Code         string `json:"code"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func initAuthorizationSettings(req *pluginv1.WatchSyncInitAuthorizeRequest) (authorizationConfig, *pluginv1.WatchSyncFault) {
	if req.GetCapabilityId() != capabilityID {
		return authorizationConfig{}, invalidRequestFault("Unknown AniList capability")
	}
	return settingsFromConfig(req.GetProviderConfig(), req.GetRedirectUri(), false)
}

func exchangeAuthorizationSettings(req *pluginv1.WatchSyncExchangeCodeRequest) (authorizationConfig, *pluginv1.WatchSyncFault) {
	if req.GetCapabilityId() != capabilityID {
		return authorizationConfig{}, invalidRequestFault("Unknown AniList capability")
	}
	return settingsFromConfig(req.GetProviderConfig(), req.GetRedirectUri(), true)
}

func settingsFromConfig(config *pluginv1.WatchSyncProviderConfig, redirectURI string, requireSecret bool) (authorizationConfig, *pluginv1.WatchSyncFault) {
	settings := authorizationConfig{
		ClientID:     strings.TrimSpace(config.GetValues()[configClientID]),
		ClientSecret: strings.TrimSpace(config.GetSecretValues()[configClientSecret]),
		RedirectURI:  strings.TrimSpace(redirectURI),
	}
	if settings.ClientID == "" {
		return authorizationConfig{}, invalidRequestFault("AniList client ID is required")
	}
	if requireSecret && settings.ClientSecret == "" {
		return authorizationConfig{}, invalidRequestFault("AniList client secret is required")
	}
	if !validRedirectURI(settings.RedirectURI) {
		return authorizationConfig{}, invalidRequestFault("AniList redirect URI must be an absolute http or https URL")
	}
	return settings, nil
}

func validRedirectURI(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func buildAuthorizationURL(clientID, redirectURI, state string) (string, *pluginv1.WatchSyncFault) {
	parsed, err := url.Parse(aniListAuthorizeURL)
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
		return "", permanentFault("AniList authorization endpoint is invalid")
	}
	values := parsed.Query()
	values.Set("client_id", clientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("response_type", "code")
	if strings.TrimSpace(state) != "" {
		values.Set("state", state)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func (s *Server) exchangeAuthorizationCode(ctx context.Context, settings authorizationConfig, code string) (*pluginv1.WatchSyncCredentials, *pluginv1.WatchSyncFault) {
	body, err := json.Marshal(tokenRequest{
		GrantType:    "authorization_code",
		ClientID:     settings.ClientID,
		ClientSecret: settings.ClientSecret,
		RedirectURI:  settings.RedirectURI,
		Code:         strings.TrimSpace(code),
	})
	if err != nil {
		return nil, permanentFault("AniList token request could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.oauthTokenEndpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, permanentFault("AniList token request could not be created")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, temporaryFault("AniList token endpoint is temporarily unreachable", 0)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, tokenHTTPFault(resp.StatusCode)
	}

	var token tokenResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	if err := decoder.Decode(&token); err != nil {
		return nil, temporaryFault("AniList token endpoint returned an unreadable response", 0)
	}
	return credentialsFromToken(token)
}

func (s *Server) httpClient() *http.Client {
	if s.http != nil {
		return s.http
	}
	return &http.Client{Timeout: defaultRequestTimeout}
}

func tokenHTTPFault(statusCode int) *pluginv1.WatchSyncFault {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest, http.StatusUnprocessableEntity:
		return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL, SafeMessage: "AniList rejected the authorization code"}
	case http.StatusTooManyRequests:
		return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED, SafeMessage: "AniList rate limit reached"}
	default:
		if statusCode >= http.StatusInternalServerError {
			return temporaryFault("AniList token endpoint is temporarily unavailable", 0)
		}
		return permanentFault("AniList token request failed")
	}
}

func credentialsFromToken(token tokenResponse) (*pluginv1.WatchSyncCredentials, *pluginv1.WatchSyncFault) {
	accessToken := strings.TrimSpace(token.AccessToken)
	if accessToken == "" {
		return nil, temporaryFault("AniList token endpoint did not return an access token", 0)
	}
	credentials := &pluginv1.WatchSyncCredentials{AccessToken: accessToken, TokenType: strings.TrimSpace(token.TokenType)}
	if token.ExpiresIn > 0 {
		credentials.ExpiresAt = timestamppb.New(time.Now().Add(time.Duration(token.ExpiresIn) * time.Second))
	}
	return credentials, nil
}
