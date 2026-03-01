package gitutil

import "testing"

func TestRepoName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "https://github.com/acme/repo.git", want: "acme_repo"},
		{in: "https://github.com/acme/repo", want: "acme_repo"},
		{in: "git@github.com:acme/repo.git", want: "acme_repo"},
		{in: "repo", want: "repo"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			if got := repoName(tc.in); got != tc.want {
				t.Fatalf("repoName(%q)=%q want=%q", tc.in, got, tc.want)
			}
		})
	}
}
