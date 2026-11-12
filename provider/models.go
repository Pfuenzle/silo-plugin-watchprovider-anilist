package provider

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   graphQLData    `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type graphQLData struct {
	Viewer             *aniListViewer    `json:"Viewer"`
	Page               aniListPage       `json:"Page"`
	SaveMediaListEntry *aniListListEntry `json:"SaveMediaListEntry"`
}

type graphQLError struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
}

type aniListViewer struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type aniListPage struct {
	Media []aniListMedia `json:"media"`
}

type aniListMedia struct {
	ID             int               `json:"id"`
	Episodes       int32             `json:"episodes"`
	Format         string            `json:"format"`
	MediaListEntry *aniListListEntry `json:"mediaListEntry"`
}

type aniListListEntry struct {
	ID       int    `json:"id"`
	Status   string `json:"status"`
	Progress int32  `json:"progress"`
}

type fuzzyDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

type scrobbleTarget struct {
	Search      string
	Year        int32
	Formats     []string
	Progress    int32
	CompletedAt *fuzzyDate
	Final       bool
}
