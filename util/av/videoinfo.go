package av

import (
	"github.com/tidwall/gjson"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func GetVideoInfo(filePath string) (int64, int64, int64) {
	probeData, err := ffmpeg.Probe(filePath)
	if err != nil {
		return 0, 0, 0
	}
	duration := gjson.Get(probeData, "format.duration").Float()
	width := gjson.Get(probeData, "streams.0.width").Int()
	height := gjson.Get(probeData, "streams.0.height").Int()

	return int64(duration), width, height
}
