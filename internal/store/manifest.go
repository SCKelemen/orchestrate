package store

import (
	"encoding/json"
	"strings"
)

const DefaultManifestJSON = "{}"

const (
	ManifestNetworkModeDefault   = "default"
	ManifestNetworkModeNone      = "none"
	ManifestNetworkModeAllowlist = "allowlist"
)

// PermissionManifest declares task/schedule sandbox capabilities.
type PermissionManifest struct {
	Sandbox SandboxManifest `json:"sandbox,omitempty"`
}

// SandboxManifest captures filesystem/network capability intent.
type SandboxManifest struct {
	Filesystem []FilesystemPermission `json:"filesystem,omitempty"`
	Network    NetworkPermission      `json:"network,omitempty"`
}

// FilesystemPermission scopes access to a repo subpath.
type FilesystemPermission struct {
	Path   string   `json:"path"`
	Access []string `json:"access,omitempty"`
}

// NetworkPermission describes desired network isolation behavior.
type NetworkPermission struct {
	Mode  string   `json:"mode,omitempty"`
	Allow []string `json:"allow,omitempty"`
}

func ParsePermissionManifest(raw string) (PermissionManifest, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultManifestJSON
	}
	var manifest PermissionManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return PermissionManifest{}, err
	}
	return manifest, nil
}

func MarshalPermissionManifest(manifest PermissionManifest) (string, error) {
	if manifest.IsZero() {
		return DefaultManifestJSON, nil
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m PermissionManifest) IsZero() bool {
	return len(m.Sandbox.Filesystem) == 0 &&
		strings.TrimSpace(m.Sandbox.Network.Mode) == "" &&
		len(m.Sandbox.Network.Allow) == 0
}
