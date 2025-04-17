package av

import "github.com/asticode/go-astiav"

func GetVideoInfo(filePath string) (int64, int64, int64) {
	astiav.SetLogLevel(astiav.LogLevelQuiet)

	formatCtx := astiav.AllocFormatContext()
	if formatCtx == nil {
		return 0, 0, 0
	}
	defer formatCtx.Free()

	if err := formatCtx.OpenInput(filePath, nil, nil); err != nil {
		return 0, 0, 0
	}
	defer formatCtx.CloseInput()

	if err := formatCtx.FindStreamInfo(nil); err != nil {
		return 0, 0, 0
	}

	var width, height int64
	found := false
	for _, stream := range formatCtx.Streams() {
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			width = int64(stream.CodecParameters().Width())
			height = int64(stream.CodecParameters().Height())
			found = true
			break
		}
	}

	if !found {
		return 0, 0, 0
	}

	// get duration in seconds
	duration := formatCtx.Duration()
	durationSeconds := duration / int64(astiav.TimeBase)

	return durationSeconds, width, height
}
