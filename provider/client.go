package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	maxResponseBytes      = 8 << 20
	defaultRequestTimeout = 30 * time.Second
)

type apiClient struct {
	endpoint *url.URL
	token    string
	http     *http.Client
}

func newAPIClient(rawEndpoint, token string, httpClient *http.Client) (*apiClient, error) {
	if strings.TrimSpace(rawEndpoint) == "" {
		rawEndpoint = defaultGraphQLEndpoint
	}
	endpoint, err := url.Parse(strings.TrimSpace(rawEndpoint))
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, errors.New("AniList GraphQL endpoint must be an absolute http or https URL")
	}
	if endpoint.Scheme == "http" && !localHTTPHost(endpoint.Hostname()) {
		return nil, errors.New("AniList GraphQL endpoint must use https unless it targets localhost")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("AniList GraphQL endpoint must not include credentials, a query, or a fragment")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &apiClient{endpoint: endpoint, token: strings.TrimSpace(token), http: httpClient}, nil
}

func localHTTPHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func (c *apiClient) graphQL(ctx context.Context, query string, variables map[string]any, output *graphQLResponse) *pluginv1.WatchSyncFault {
	encoded, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return permanentFault("AniList request could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return permanentFault("AniList request could not be created")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return temporaryFault("AniList is temporarily unreachable", 0)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return faultForHTTPResponse(resp)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	if err := decoder.Decode(output); err != nil {
		return temporaryFault("AniList returned an unreadable response", 0)
	}
	if len(output.Errors) > 0 {
		return faultForGraphQLErrors(output.Errors)
	}
	return nil
}

func faultForHTTPResponse(resp *http.Response) *pluginv1.WatchSyncFault {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL, SafeMessage: "AniList rejected the access token"}
	case http.StatusTooManyRequests:
		return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED, SafeMessage: "AniList rate limit reached", RetryAfter: durationpb.New(retryAfter(resp.Header.Get("Retry-After")))}
	case http.StatusRequestTimeout:
		return temporaryFault("AniList request timed out", 0)
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_REQUEST, SafeMessage: fmt.Sprintf("AniList rejected the request (HTTP %d)", resp.StatusCode)}
	default:
		if resp.StatusCode >= http.StatusInternalServerError {
			return temporaryFault("AniList is temporarily unavailable", 0)
		}
		return permanentFault(fmt.Sprintf("AniList request failed (HTTP %d)", resp.StatusCode))
	}
}

func faultForGraphQLErrors(values []graphQLError) *pluginv1.WatchSyncFault {
	for _, value := range values {
		if value.Status == http.StatusTooManyRequests {
			return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED, SafeMessage: "AniList rate limit reached"}
		}
		if value.Status == http.StatusUnauthorized || value.Status == http.StatusForbidden {
			return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL, SafeMessage: "AniList rejected the access token"}
		}
		if value.Status >= http.StatusInternalServerError {
			return temporaryFault("AniList is temporarily unavailable", 0)
		}
	}
	return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_REQUEST, SafeMessage: "AniList rejected the GraphQL request"}
}

func retryAfter(value string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(0, time.Until(at))
	}
	return 0
}

func temporaryFault(message string, retryAfter time.Duration) *pluginv1.WatchSyncFault {
	fault := &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY, SafeMessage: message}
	if retryAfter > 0 {
		fault.RetryAfter = durationpb.New(retryAfter)
	}
	return fault
}

func permanentFault(message string) *pluginv1.WatchSyncFault {
	return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_PERMANENT, SafeMessage: message}
}
