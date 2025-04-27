package twitter

type APIResponse struct {
	Data struct {
		TweetResult struct {
			Result *TweetResult `json:"result,omitempty"`
		} `json:"tweetResult"`
	} `json:"data"`
}

type TweetResult struct {
	Tweet       *Tweet     `json:"tweet,omitempty"`
	Legacy      *Tweet     `json:"legacy,omitempty"`
	RestID      string     `json:"rest_id,omitempty"`
	Core        *Core      `json:"core,omitempty"`
	Views       *ViewsInfo `json:"views,omitempty"`
	Source      string     `json:"source,omitempty"`
	EditControl *EditInfo  `json:"edit_control,omitempty"`
	TypeName    string     `json:"__typename,omitempty"`
}

type EditInfo struct {
	EditTweetIDs   []string `json:"edit_tweet_ids,omitempty"`
	EditableUntil  string   `json:"editable_until_msecs,omitempty"`
	IsEditEligible bool     `json:"is_edit_eligible,omitempty"`
	EditsRemaining string   `json:"edits_remaining,omitempty"`
}

type ViewsInfo struct {
	Count string `json:"count,omitempty"`
	State string `json:"state,omitempty"`
}

type Core struct {
	UserResults struct {
		Result struct {
			TypeName string      `json:"__typename,omitempty"`
			RestID   string      `json:"rest_id,omitempty"`
			Legacy   *UserLegacy `json:"legacy,omitempty"`
		} `json:"result"`
	} `json:"user_results"`
}

type UserLegacy struct {
	ScreenName           string `json:"screen_name"`
	Name                 string `json:"name"`
	ProfileImageURLHTTPS string `json:"profile_image_url_https,omitempty"`
	CreatedAt            string `json:"created_at,omitempty"`
}

type Tweet struct {
	FullText          string            `json:"full_text"`
	ExtendedEntities  *ExtendedEntities `json:"extended_entities,omitempty"`
	Entities          *ExtendedEntities `json:"entities,omitempty"`
	CreatedAt         string            `json:"created_at"`
	ID                string            `json:"id_str"`
	BookmarkCount     int               `json:"bookmark_count,omitempty"`
	FavoriteCount     int               `json:"favorite_count,omitempty"`
	ReplyCount        int               `json:"reply_count,omitempty"`
	RetweetCount      int               `json:"retweet_count,omitempty"`
	QuoteCount        int               `json:"quote_count,omitempty"`
	PossiblySensitive bool              `json:"possibly_sensitive,omitempty"`
	ConversationID    string            `json:"conversation_id_str,omitempty"`
	Lang              string            `json:"lang,omitempty"`
	UserIDStr         string            `json:"user_id_str,omitempty"`
}

type ExtendedEntities struct {
	Media []MediaEntity `json:"media,omitempty"`
}

type MediaEntity struct {
	Type              string             `json:"type"`
	MediaURLHTTPS     string             `json:"media_url_https"`
	ExpandedURL       string             `json:"expanded_url"`
	URL               string             `json:"url"`
	DisplayURL        string             `json:"display_url,omitempty"`
	IDStr             string             `json:"id_str,omitempty"`
	MediaKey          string             `json:"media_key,omitempty"`
	VideoInfo         *VideoInfo         `json:"video_info,omitempty"`
	Sizes             *MediaSizes        `json:"sizes,omitempty"`
	OriginalInfo      *OriginalInfo      `json:"original_info,omitempty"`
	MediaAvailability *MediaAvailability `json:"ext_media_availability,omitempty"`
}

type MediaAvailability struct {
	Status string `json:"status"`
}

type MediaSizes struct {
	Large  SizeInfo `json:"large,omitempty"`
	Medium SizeInfo `json:"medium,omitempty"`
	Small  SizeInfo `json:"small,omitempty"`
	Thumb  SizeInfo `json:"thumb,omitempty"`
}

type SizeInfo struct {
	H      int    `json:"h"`
	W      int    `json:"w"`
	Resize string `json:"resize,omitempty"`
}

type OriginalInfo struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

type VideoInfo struct {
	DurationMillis int       `json:"duration_millis"`
	Variants       []Variant `json:"variants"`
	AspectRatio    []int     `json:"aspect_ratio"`
}

type Variant struct {
	Bitrate     int    `json:"bitrate,omitempty"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
}
