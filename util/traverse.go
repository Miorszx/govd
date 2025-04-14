package util

func TraverseJSON(
	data interface{},
	keys interface{},
) interface{} {
	var keySlice []string
	switch k := keys.(type) {
	case string:
		keySlice = []string{k}
	case []string:
		keySlice = k
	default:
		return nil // unsupported keys type
	}

	return traverseObject(data, keySlice)
}

func traverseObject(data interface{}, keys []string) interface{} {
	if len(keys) == 0 {
		return data
	}

	key := keys[0]
	remainingKeys := keys[1:]

	switch d := data.(type) {
	case map[string]interface{}:
		if value, exists := d[key]; exists {
			return traverseObject(value, remainingKeys)
		}

		for _, value := range d {
			result := traverseObject(value, keys)
			if result != nil {
				return result
			}
		}
	case []interface{}:
		for _, item := range d {
			result := traverseObject(item, keys)
			if result != nil {
				return result
			}
		}
	}
	return nil
}
