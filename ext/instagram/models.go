package instagram

type IGramResponse struct {
	Items []*IGramMedia `json:"items"`
}

type IGramMedia struct {
	URL       []*MediaURL `json:"url"`
	Thumb     string      `json:"thumb"`
	Hosting   string      `json:"hosting"`
	Timestamp int         `json:"timestamp"`
}

type MediaURL struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Type string `json:"type"`
	Ext  string `json:"ext"`
}
