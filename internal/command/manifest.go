package command

import "strings"

func parseCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func buildManifestPayload(fsPathsCSV, networkMode, egressDomainsCSV string) map[string]any {
	fsPaths := parseCSV(fsPathsCSV)
	egressDomains := parseCSV(egressDomainsCSV)
	networkMode = strings.TrimSpace(networkMode)

	sandbox := map[string]any{}
	if len(fsPaths) > 0 {
		fs := make([]map[string]any, 0, len(fsPaths))
		for _, p := range fsPaths {
			fs = append(fs, map[string]any{
				"path":   p,
				"access": []string{"read", "write"},
			})
		}
		sandbox["filesystem"] = fs
	}
	if networkMode != "" || len(egressDomains) > 0 {
		net := map[string]any{}
		if networkMode != "" {
			net["mode"] = networkMode
		}
		if len(egressDomains) > 0 {
			net["allow"] = egressDomains
		}
		sandbox["network"] = net
	}

	if len(sandbox) == 0 {
		return nil
	}
	return map[string]any{"sandbox": sandbox}
}
