package main

import "encoding/json"

func jsonUnmarshalImpl(s string, out *string) error {
	return json.Unmarshal([]byte(s), out)
}
