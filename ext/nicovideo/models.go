package nicovideo

type ServerResponse struct {
	Meta *Meta `json:"meta,omitempty"`
	Data *Data `json:"data,omitempty"`
}

type PlayerResponse struct {
	Data struct {
		ContentURL string `json:"contentUrl,omitempty"`
	} `json:"data,omitempty"`
}

type Meta struct {
	Status int    `json:"status,omitempty"`
	Code   string `json:"code,omitempty"`
}

type Client struct {
	WatchID      string `json:"watchId,omitempty"`
	WatchTrackID string `json:"watchTrackId,omitempty"`
	Nicosid      string `json:"nicosid,omitempty"`
}

type Videos struct {
	QualityLevel                        int    `json:"qualityLevel,omitempty"`
	RecommendedHighestAudioQualityLevel int    `json:"recommendedHighestAudioQualityLevel,omitempty"`
	ID                                  string `json:"id,omitempty"`
	IsAvailable                         bool   `json:"isAvailable,omitempty"`
	Label                               string `json:"label,omitempty"`
	Bitrate                             int    `json:"bitRate,omitempty"`
	Width                               int    `json:"width,omitempty"`
	Height                              int    `json:"height,omitempty"`
}

type LoudnessCollection struct {
	Type  string  `json:"type,omitempty"`
	Value float64 `json:"value,omitempty"`
}

type Label struct {
	Quality string `json:"quality,omitempty"`
	Bitrate string `json:"bitrate,omitempty"`
}

type Audios struct {
	IsAvailable        bool                  `json:"isAvailable,omitempty"`
	IntegratedLoudness float64               `json:"integratedLoudness,omitempty"`
	TruePeak           float64               `json:"truePeak,omitempty"`
	QualityLevel       int                   `json:"qualityLevel,omitempty"`
	LoudnessCollection []*LoudnessCollection `json:"loudnessCollection,omitempty"`
	SamplingRate       int                   `json:"samplingRate,omitempty"`
	Label              *Label                `json:"label,omitempty"`
	ID                 string                `json:"id,omitempty"`
	BitRate            int                   `json:"bitRate,omitempty"`
}

type Domand struct {
	AccessRightKey string    `json:"accessRightKey,omitempty"`
	Videos         []*Videos `json:"videos,omitempty"`
	Audios         []*Audios `json:"audios,omitempty"`
}

type Media struct {
	Domand *Domand `json:"domand,omitempty"`
}

type Thumbnail struct {
	URL       string `json:"url,omitempty"`
	MiddleURL string `json:"middleUrl,omitempty"`
	LargeURL  string `json:"largeUrl,omitempty"`
	Player    string `json:"player,omitempty"`
	Ogp       string `json:"ogp,omitempty"`
}

type Rating struct {
	IsAdult bool `json:"isAdult,omitempty"`
}

type Video struct {
	IsAuthenticationRequired    bool       `json:"isAuthenticationRequired,omitempty"`
	Thumbnail                   *Thumbnail `json:"thumbnail,omitempty"`
	WatchableUserTypeForPayment string     `json:"watchableUserTypeForPayment,omitempty"`
	Rating                      *Rating    `json:"rating,omitempty"`
	Duration                    int64      `json:"duration,omitempty"`
	Title                       string     `json:"title,omitempty"`
	IsPrivate                   bool       `json:"isPrivate,omitempty"`
	ID                          string     `json:"id,omitempty"`
	Description                 string     `json:"description,omitempty"`
	IsDeleted                   bool       `json:"isDeleted,omitempty"`
}

type Response struct {
	Client *Client `json:"client,omitempty"`
	Media  *Media  `json:"media,omitempty"`
	Video  *Video  `json:"video,omitempty"`
}

type Data struct {
	Response *Response `json:"response,omitempty"`
}
