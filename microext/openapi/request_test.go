package openapi

import (
	"testing"
)

func TestExtractParams(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		subject string
		want    map[string]string
	}{
		{
			name:    "no wildcards",
			pattern: "users.create",
			subject: "users.create",
			want:    nil,
		},
		{
			name:    "named param",
			pattern: "users.{id}.profile",
			subject: "users.alice.profile",
			want:    map[string]string{"id": "alice"},
		},
		{
			name:    "multiple named params",
			pattern: "orgs.{org}.users.{user}.profile",
			subject: "orgs.acme.users.alice.profile",
			want:    map[string]string{"org": "acme", "user": "alice"},
		},
		{
			name:    "bare star gets default name",
			pattern: "users.*.profile",
			subject: "users.alice.profile",
			want:    map[string]string{"param1": "alice"},
		},
		{
			name:    "gt wildcard captures remainder",
			pattern: "files.>",
			subject: "files.docs.readme.txt",
			want:    map[string]string{"*": "docs.readme.txt"},
		},
		{
			name:    "gt wildcard single token",
			pattern: "files.>",
			subject: "files.readme",
			want:    map[string]string{"*": "readme"},
		},
		{
			name:    "named param and gt combined",
			pattern: "users.{id}.files.>",
			subject: "users.alice.files.docs.notes.txt",
			want:    map[string]string{"id": "alice", "*": "docs.notes.txt"},
		},
		{
			name:    "named param at subject boundary returns nil",
			pattern: "users.{id}",
			subject: "users",
			want:    nil,
		},
		{
			name:    "gt at subject boundary returns nil",
			pattern: "files.>",
			subject: "files",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractParams(tt.pattern, tt.subject)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %v, got nil", tt.want)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d params, got %d: %v", len(tt.want), len(got), got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("param %q: expected %q, got %q", k, v, got[k])
				}
			}
		})
	}
}

func TestParseSubjectPattern(t *testing.T) {
	tests := []struct {
		name        string
		subject     string
		wantSubject string
		wantParams  []string
	}{
		{
			name:        "no params",
			subject:     "users.create",
			wantSubject: "users.create",
			wantParams:  nil,
		},
		{
			name:        "single param",
			subject:     "users.{id}.profile",
			wantSubject: "users.*.profile",
			wantParams:  []string{"id"},
		},
		{
			name:        "multiple params",
			subject:     "orgs.{org}.users.{user}",
			wantSubject: "orgs.*.users.*",
			wantParams:  []string{"org", "user"},
		},
		{
			name:        "bare star unchanged",
			subject:     "users.*.profile",
			wantSubject: "users.*.profile",
			wantParams:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSubject, gotParams := parseSubjectPattern(tt.subject)
			if gotSubject != tt.wantSubject {
				t.Errorf("subject: expected %q, got %q", tt.wantSubject, gotSubject)
			}
			if len(gotParams) != len(tt.wantParams) {
				t.Fatalf("params: expected %v, got %v", tt.wantParams, gotParams)
			}
			for i := range tt.wantParams {
				if gotParams[i] != tt.wantParams[i] {
					t.Errorf("params[%d]: expected %q, got %q", i, tt.wantParams[i], gotParams[i])
				}
			}
		})
	}
}
