package threads

import "github.com/govdbot/govd/util"

var (
	ErrRelayDataNotFound       = &util.Error{Message: "failed to find relay data"}
	ErrRelayDataInvalid        = &util.Error{Message: "invalid relay data"}
	ErrRelayDataBboxNotFound   = &util.Error{Message: "failed to find bbox in relay data"}
	ErrRelayDataResultNotFound = &util.Error{Message: "failed to find result in relay data"}
	ErrRelayDataDataNotFound   = &util.Error{Message: "failed to find data in relay data"}
	ErrRelayDataEdgesNotFound  = &util.Error{Message: "failed to find edges in relay data"}
	ErrRelayDataPostNotFound   = &util.Error{Message: "post not found in relay data"}
)
