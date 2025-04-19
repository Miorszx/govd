package redgifs

type Response struct {
	Gif *Gif `json:"gif"`
}

type Token struct {
	AccessToken string `json:"token"`
	Agent       string `json:"agent"`
	ExpiresIn   int64  `json:"expires_in"`
}

type Urls struct {
	Silent    string `json:"silent"`
	Sd        string `json:"sd"`
	Hd        string `json:"hd"`
	Thumbnail string `json:"thumbnail"`
	HTML      string `json:"html"`
	Poster    string `json:"poster"`
}

type Gif struct {
	AvgColor     string   `json:"avgColor"`
	CreateDate   int      `json:"createDate"`
	Description  string   `json:"description"`
	Duration     float64  `json:"duration"`
	HasAudio     bool     `json:"hasAudio"`
	Height       int      `json:"height"`
	HideHome     bool     `json:"hideHome"`
	HideTrending bool     `json:"hideTrending"`
	Hls          bool     `json:"hls"`
	ID           string   `json:"id"`
	Likes        int      `json:"likes"`
	Niches       []string `json:"niches"`
	Published    bool     `json:"published"`
	Type         int      `json:"type"`
	Sexuality    []string `json:"sexuality"`
	Tags         []string `json:"tags"`
	Urls         Urls     `json:"urls"`
	UserName     string   `json:"userName"`
	Verified     bool     `json:"verified"`
	Views        int      `json:"views"`
	Width        int      `json:"width"`
}
