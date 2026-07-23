package confii

func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			result[k] = copyMap(sub)
		} else {
			result[k] = v
		}
	}
	return result
}

func copyLoaderLayers(layers []map[string]any) []map[string]any {
	if layers == nil {
		return nil
	}
	result := make([]map[string]any, len(layers))
	for i, layer := range layers {
		if layer != nil {
			result[i] = copyMap(layer)
		}
	}
	return result
}

func copyLoaderDependencies(dependencies [][]string) [][]string {
	if dependencies == nil {
		return nil
	}
	result := make([][]string, len(dependencies))
	for i, paths := range dependencies {
		result[i] = append([]string(nil), paths...)
	}
	return result
}
