package nicovideo

import "github.com/govdbot/govd/util"

var (
	ErrServerResponseNotFound = &util.Error{Message: "server response not found"}
	ErrNoDomandDataFound      = &util.Error{Message: "no domand data found in server response"}
	ErrNoClientDataFound      = &util.Error{Message: "no client data found in server response"}
	ErrNoVideoDataFound       = &util.Error{Message: "no video data found in server response"}
	ErrNoAudioDataFound       = &util.Error{Message: "no audio data found in server response"}
	ErrNoAccessKeyFound       = &util.Error{Message: "no access key found in server response"}
	ErrNoTrackIDFound         = &util.Error{Message: "no track ID found in server response"}
	ErrNoSessionIDFound       = &util.Error{Message: "no session ID found in server response"}
)
