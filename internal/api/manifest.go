package api

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/SCKelemen/orchestrate/internal/store"
)

func parseStoredManifest(raw string) store.PermissionManifest {
	manifest, err := store.ParsePermissionManifest(raw)
	if err != nil {
		return store.PermissionManifest{}
	}
	return manifest
}

func normalizeAndMarshalManifest(in *store.PermissionManifest, repoURL string) (store.PermissionManifest, string, error) {
	if in == nil {
		return store.PermissionManifest{}, store.DefaultManifestJSON, nil
	}

	manifest := store.PermissionManifest{}
	fs, err := normalizeFilesystemPermissions(in.Sandbox.Filesystem)
	if err != nil {
		return store.PermissionManifest{}, "", err
	}
	manifest.Sandbox.Filesystem = fs

	network, err := normalizeNetworkPermission(in.Sandbox.Network, repoURL)
	if err != nil {
		return store.PermissionManifest{}, "", err
	}
	manifest.Sandbox.Network = network

	encoded, err := store.MarshalPermissionManifest(manifest)
	if err != nil {
		return store.PermissionManifest{}, "", err
	}
	return manifest, encoded, nil
}

func normalizeFilesystemPermissions(in []store.FilesystemPermission) ([]store.FilesystemPermission, error) {
	if len(in) == 0 {
		return nil, nil
	}

	type accessSet struct {
		read  bool
		write bool
	}
	paths := map[string]accessSet{}
	fullRepo := false
	for _, p := range in {
		normalizedPath, err := normalizeRepoSubpath(p.Path)
		if err != nil {
			return nil, err
		}
		if normalizedPath == "." {
			fullRepo = true
			break
		}

		access, err := normalizeFilesystemAccess(p.Access)
		if err != nil {
			return nil, err
		}

		current := paths[normalizedPath]
		for _, a := range access {
			switch a {
			case "read":
				current.read = true
			case "write":
				current.write = true
			}
		}
		paths[normalizedPath] = current
	}

	if fullRepo {
		return nil, nil
	}
	if len(paths) == 0 {
		return nil, nil
	}

	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]store.FilesystemPermission, 0, len(keys))
	for _, k := range keys {
		set := paths[k]
		access := make([]string, 0, 2)
		if set.read {
			access = append(access, "read")
		}
		if set.write {
			access = append(access, "write")
		}
		out = append(out, store.FilesystemPermission{
			Path:   k,
			Access: access,
		})
	}

	return out, nil
}

func normalizeRepoSubpath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", fmt.Errorf("manifest filesystem.path is required")
	}
	p = filepath.ToSlash(p)
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("manifest filesystem.path must be relative: %s", raw)
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("manifest filesystem.path cannot escape repo root: %s", raw)
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git/") {
		return "", fmt.Errorf("manifest filesystem.path cannot target .git: %s", raw)
	}
	return clean, nil
}

func normalizeFilesystemAccess(in []string) ([]string, error) {
	if len(in) == 0 {
		return []string{"read", "write"}, nil
	}

	var hasRead, hasWrite bool
	for _, a := range in {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "read":
			hasRead = true
		case "write":
			hasWrite = true
		case "":
			// Skip empty values.
		default:
			return nil, fmt.Errorf("manifest filesystem.access value %q is invalid (allowed: read, write)", a)
		}
	}
	if !hasRead && !hasWrite {
		return []string{"read", "write"}, nil
	}

	access := make([]string, 0, 2)
	if hasRead {
		access = append(access, "read")
	}
	if hasWrite {
		access = append(access, "write")
	}
	return access, nil
}

func normalizeNetworkPermission(in store.NetworkPermission, repoURL string) (store.NetworkPermission, error) {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))

	allowSet := map[string]struct{}{}
	allow := make([]string, 0, len(in.Allow))
	for _, domain := range in.Allow {
		normalized, err := normalizeAllowDomain(domain)
		if err != nil {
			return store.NetworkPermission{}, err
		}
		if _, seen := allowSet[normalized]; seen {
			continue
		}
		allowSet[normalized] = struct{}{}
		allow = append(allow, normalized)
	}
	sort.Strings(allow)

	if mode == "" && len(allow) > 0 {
		mode = store.ManifestNetworkModeAllowlist
	}

	switch mode {
	case "", store.ManifestNetworkModeDefault:
		// Accept as-is.
	case store.ManifestNetworkModeNone:
		if len(allow) > 0 {
			return store.NetworkPermission{}, fmt.Errorf("manifest network.allow cannot be set when mode is none")
		}
	case store.ManifestNetworkModeAllowlist:
		if len(allow) == 0 {
			return store.NetworkPermission{}, fmt.Errorf("manifest network.allow is required when mode is allowlist")
		}
		repoHost := extractHostFromRepoURL(repoURL)
		if repoHost != "" && !domainListIncludesHost(allow, repoHost) {
			return store.NetworkPermission{}, fmt.Errorf("manifest network.allow must include repo host %q", repoHost)
		}
	default:
		return store.NetworkPermission{}, fmt.Errorf("manifest network.mode %q is invalid (allowed: default, none, allowlist)", in.Mode)
	}

	return store.NetworkPermission{
		Mode:  mode,
		Allow: allow,
	}, nil
}

func normalizeAllowDomain(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", fmt.Errorf("manifest network.allow contains an empty value")
	}
	if strings.Contains(v, "*") {
		return "", fmt.Errorf("manifest network.allow does not support wildcards: %s", raw)
	}

	if strings.Contains(v, "://") {
		u, err := url.Parse(v)
		if err != nil {
			return "", fmt.Errorf("manifest network.allow value %q is invalid: %w", raw, err)
		}
		if u.Host == "" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return "", fmt.Errorf("manifest network.allow value %q must be host[:port] or URL without path/query", raw)
		}
		v = u.Host
	}

	u, err := url.Parse("https://" + v)
	if err != nil || u.Host == "" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("manifest network.allow value %q is invalid", raw)
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("manifest network.allow value %q is invalid", raw)
	}

	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("manifest network.allow value %q has invalid port", raw)
		}
		return net.JoinHostPort(host, port), nil
	}
	return host, nil
}

func extractHostFromRepoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err == nil {
			return strings.ToLower(strings.TrimSpace(u.Hostname()))
		}
	}

	// git@github.com:org/repo.git
	if at := strings.LastIndex(raw, "@"); at != -1 {
		rest := raw[at+1:]
		if i := strings.Index(rest, ":"); i > 0 {
			return strings.ToLower(strings.TrimSpace(rest[:i]))
		}
	}

	return ""
}

func domainListIncludesHost(allow []string, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, entry := range allow {
		if strings.EqualFold(entry, host) {
			return true
		}
		if h, _, err := net.SplitHostPort(entry); err == nil && strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}
