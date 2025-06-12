package threads

type RelayData struct {
	Bbox *Bbox `json:"__bbox"`
}

type Caption struct {
	Text string `json:"text"`
	Pk   string `json:"pk"`
}

type Candidates struct {
	Height int    `json:"height"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
}

type ImageVersions2 struct {
	Candidates []*Candidates `json:"candidates"`
}

type VideoVersions struct {
	Type int    `json:"type"`
	URL  string `json:"url"`
}

type SharingFrictionInfo struct {
	ShouldHaveSharingFriction bool `json:"should_have_sharing_friction"`
	SharingFrictionPayload    any  `json:"sharing_friction_payload"`
}

type Post struct {
	ID             string           `json:"id"`
	Pk             string           `json:"pk"`
	Caption        Caption          `json:"caption"`
	CarouselMedia  []*Post          `json:"carousel_media"`
	Code           string           `json:"code"`
	ImageVersions2 *ImageVersions2  `json:"image_versions2"`
	OriginalHeight int              `json:"original_height"`
	OriginalWidth  int              `json:"original_width"`
	VideoVersions  []*VideoVersions `json:"video_versions"`
	HasAudio       bool             `json:"has_audio"`
	MediaType      int              `json:"media_type"`
}

type ThreadItems struct {
	Post *Post `json:"post"`
}

type Node struct {
	ThreadItems []*ThreadItems `json:"thread_items"`
	ThreadType  string         `json:"thread_type"`
	ID          string         `json:"id"`
	Typename    string         `json:"__typename"`
}

type Edges struct {
	Node *Node `json:"node"`
}

type PostData struct {
	Edges []*Edges `json:"edges"`
}

type Data struct {
	Data *PostData `json:"data"`
}

type Result struct {
	Data *Data `json:"data"`
}

type Bbox struct {
	Complete       bool    `json:"complete"`
	Result         *Result `json:"result"`
	SequenceNumber int     `json:"sequence_number"`
}
