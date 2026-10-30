package facebook

// VideoData holds extracted video information from Facebook page HTML.
type VideoData struct {
	HDURL     string
	SDURL     string
	ImageURL  string
	ImageURLs []string
	Title     string
	Width     int32
	Height    int32

	// DASHURL is the video-only high-resolution rendition taken from the DASH
	// manifest. Facebook only serves 1080p as AV1/VP9 in DASH, never as a
	// muxed progressive file, so this track has to be re-encoded and muxed
	// with DASHAudioURL before it can be sent. All DASH fields are zero when
	// the reel carries no manifest.
	DASHURL        string
	DASHAudioURL   string
	DASHWidth      int32
	DASHHeight     int32
	DASHSourceSize int64
	DASHDuration   int32
}
