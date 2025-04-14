package twitter

type APIResponse struct {
	Data struct {
		ThreadedConversationWithInjectionsV2 struct {
			Instructions []struct {
				Entries []struct {
					EntryID string `json:"entryId"`
					Content struct {
						ItemContent struct {
							TweetResults struct {
								Result TweetResult `json:"result"`
							} `json:"tweet_results"`
						} `json:"itemContent"`
					} `json:"content"`
				} `json:"entries"`
			} `json:"instructions"`
		} `json:"threaded_conversation_with_injections_v2"`
	} `json:"data"`
}

type TweetResult struct {
	Tweet  *Tweet `json:"tweet,omitempty"`
	Legacy *Tweet `json:"legacy,omitempty"`
	RestID string `json:"rest_id,omitempty"`
	Core   *Core  `json:"core,omitempty"`
}

type Core struct {
	UserResults struct {
		Result struct {
			Legacy *UserLegacy `json:"legacy,omitempty"`
		} `json:"result"`
	} `json:"user_results"`
}

type UserLegacy struct {
	ScreenName string `json:"screen_name"`
	Name       string `json:"name"`
}

type Tweet struct {
	FullText         string            `json:"full_text"`
	ExtendedEntities *ExtendedEntities `json:"extended_entities,omitempty"`
	Entities         *ExtendedEntities `json:"entities,omitempty"`
	CreatedAt        string            `json:"created_at"`
	ID               string            `json:"id_str"`
}

type ExtendedEntities struct {
	Media []MediaEntity `json:"media,omitempty"`
}

type MediaEntity struct {
	Type          string     `json:"type"`
	MediaURLHTTPS string     `json:"media_url_https"`
	ExpandedURL   string     `json:"expanded_url"`
	URL           string     `json:"url"`
	VideoInfo     *VideoInfo `json:"video_info,omitempty"`
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
