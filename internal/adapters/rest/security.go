package rest

import "sort"

func selectSecurity(root, operation map[string]any, bindings map[string]bool) ([]string, error) {
	raw, present := operation["security"]
	if !present {
		raw, present = root["security"]
	}
	if !present {
		return nil, nil
	}
	requirements, ok := raw.([]any)
	if !ok {
		return nil, ErrUnsupportedOpenAPI
	}
	if len(requirements) == 0 {
		return nil, nil
	}
	for _, rawRequirement := range requirements {
		requirement, ok := rawRequirement.(map[string]any)
		if !ok {
			return nil, ErrUnsupportedOpenAPI
		}
		selected := make([]string, 0, len(requirement))
		satisfied := true
		for scheme := range requirement {
			if !securitySchemeExists(root, scheme) || !bindings[scheme] {
				satisfied = false
				break
			}
			selected = append(selected, scheme)
		}
		if satisfied {
			sort.Strings(selected)
			return selected, nil
		}
	}
	return nil, ErrSecurityUnsatisfied
}

func securitySchemeExists(root map[string]any, name string) bool {
	components, _ := root["components"].(map[string]any)
	schemes, _ := components["securitySchemes"].(map[string]any)
	_, exists := schemes[name]
	return exists
}
