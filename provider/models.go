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
	Message    string              `json:"message"`
	Status     int                 `json:"status"`
	Validation map[string][]string `json:"validation"`
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
	Title          aniListTitle      `json:"title"`
	Episodes       int32             `json:"episodes"`
	Format         string            `json:"format"`
	Season         string            `json:"season"`
	SeasonYear     int32             `json:"seasonYear"`
	MediaListEntry *aniListListEntry `json:"mediaListEntry"`
	Relations      aniListRelations  `json:"relations"`
}

type aniListRelations struct {
	Edges []struct {
		RelationType string `json:"relationType"`
		Node         struct {
			ID int `json:"id"`
		} `json:"node"`
	} `json:"edges"`
}
type aniListTitle struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
	Native  string `json:"native"`
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
	Search       string
	Year         int32
	SeriesYear   int32
	Formats      []string
	Progress     int32
	SeasonNumber int32
	XEMSeason    int32
	SeriesTVDBID string
	CompletedAt  *fuzzyDate
	Final        bool
}
type xemMapping struct {
	Season   int32 `json:"season"`
	Episode  int32 `json:"episode"`
	Absolute int32 `json:"absolute"`
}

type xemMappingResponse struct {
	Result string                `json:"result"`
	Data   map[string]xemMapping `json:"data"`
}
