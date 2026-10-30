package main

import (
	"bufio"
	"net/http"
	"os"
	"strings"
)

func loadCookies(path string) ([]*http.Cookie, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []*http.Cookie
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 7 {
			continue
		}
		out = append(out, &http.Cookie{Name: parts[5], Value: parts[6]})
	}
	return out, sc.Err()
}
