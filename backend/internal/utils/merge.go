package utils

func MergeMaps(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if vMap, ok := v.(map[string]any); ok {
			if dstMap, ok := dst[k].(map[string]any); ok {
				dst[k] = MergeMaps(dstMap, vMap)
				continue
			}
		}
		dst[k] = v
	}
	return dst
}
