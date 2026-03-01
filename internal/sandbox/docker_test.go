package sandbox

import "testing"

func TestUniqueCounterIncrements(t *testing.T) {
	t.Parallel()

	a := uniqueCounter()
	b := uniqueCounter()
	if b <= a {
		t.Fatalf("counter did not increment: a=%d b=%d", a, b)
	}
}

func TestNewDocker(t *testing.T) {
	t.Parallel()

	d := NewDocker("/tmp/orchestrate")
	if d == nil {
		t.Fatal("docker sandbox is nil")
	}
	if d.dataDir != "/tmp/orchestrate" {
		t.Fatalf("dataDir=%q want=%q", d.dataDir, "/tmp/orchestrate")
	}
}
