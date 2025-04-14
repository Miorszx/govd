package reddit

type RedditResponse []struct {
	Data struct {
		Children []struct {
			Data PostData `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

type PostData struct {
	ID            string                   `json:"id"`
	Title         string                   `json:"title"`
	IsVideo       bool                     `json:"is_video"`
	Thumbnail     string                   `json:"thumbnail"`
	Media         *Media                   `json:"media"`
	Preview       *Preview                 `json:"preview"`
	MediaMetadata map[string]MediaMetadata `json:"media_metadata"`
	SecureMedia   *Media                   `json:"secure_media"`
	Over18        bool                     `json:"over_18"`
}

type Media struct {
	RedditVideo *RedditVideo `json:"reddit_video"`
}

type RedditVideo struct {
	FallbackURL      string `json:"fallback_url"`
	HLSURL           string `json:"hls_url"`
	DashURL          string `json:"dash_url"`
	Duration         int64  `json:"duration"`
	Height           int64  `json:"height"`
	Width            int64  `json:"width"`
	ScrubberMediaURL string `json:"scrubber_media_url"`
}

type Preview struct {
	Images             []Image             `json:"images"`
	RedditVideoPreview *RedditVideoPreview `json:"reddit_video_preview"`
}

type Image struct {
	Source   ImageSource   `json:"source"`
	Variants ImageVariants `json:"variants"`
}

type ImageSource struct {
	URL    string `json:"url"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

type ImageVariants struct {
	MP4 *MP4Variant `json:"mp4"`
}

type MP4Variant struct {
	Source ImageSource `json:"source"`
}

type RedditVideoPreview struct {
	FallbackURL string `json:"fallback_url"`
	Duration    int64  `json:"duration"`
}

type MediaMetadata struct {
	Status string `json:"status"`
	E      string `json:"e"`
	S      struct {
		U string `json:"u"`
		X int64  `json:"x"`
		Y int64  `json:"y"`
	} `json:"s"`
}
