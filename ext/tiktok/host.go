package tiktok

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

var apiHosts = []string{
	"api9-normal-useast2a.tiktokv.com",
	"api22-normal-probe-useast2a.tiktokv.com",
	"api16-normal-probe-useast2a.tiktokv.com",
}

func GetRandomAPIHost() string {
	if len(apiHosts) == 0 {
		return ""
	}
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return apiHosts[time.Now().UnixNano()%int64(len(apiHosts))]
	}
	randInt := binary.BigEndian.Uint64(b)
	return apiHosts[randInt%uint64(len(apiHosts))]
}
