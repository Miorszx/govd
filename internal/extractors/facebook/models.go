package facebook

// VideoData holds extracted video information from Facebook page HTML.
type VideoData struct {
	HDURL    string
	SDURL    string
	ImageURL string
	Title    string
	Width    int32
	Height   int32
}
