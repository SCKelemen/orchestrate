package command

import "testing"

func TestParseCSV(t *testing.T) {
	t.Parallel()

	got := parseCSV("a, b ,,a")
	if len(got) != 2 {
		t.Fatalf("len=%d want=2 got=%v", len(got), got)
	}
	if got[0] != "a" || got[1] != "b" {
		t.Fatalf("got=%v want=[a b]", got)
	}
}

func TestBuildManifestPayload(t *testing.T) {
	t.Parallel()

	manifest := buildManifestPayload("src,tests", "allowlist", "github.com:443,api.openai.com:443")
	if manifest == nil {
		t.Fatal("manifest should not be nil")
	}
	sandbox, ok := manifest["sandbox"].(map[string]any)
	if !ok {
		t.Fatalf("sandbox type=%T", manifest["sandbox"])
	}
	if _, ok := sandbox["filesystem"]; !ok {
		t.Fatal("filesystem not set")
	}
	net, ok := sandbox["network"].(map[string]any)
	if !ok {
		t.Fatalf("network type=%T", sandbox["network"])
	}
	if net["mode"] != "allowlist" {
		t.Fatalf("network mode=%v want=allowlist", net["mode"])
	}
}
