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

	// DASH is the full video ladder from the manifest, tallest rendition
	// first, plus the audio track. Facebook only serves 1080p as AV1/VP9 in
	// DASH, never as a muxed progressive file, so the chosen track has to be
	// re-encoded and muxed with the audio before it can be sent. Nil when the
	// reel carries no manifest.
	DASH *dashTracks
	DASHDuration int32
}
