package sync

import (
	"testing"
)

func TestSplitKey(t *testing.T) {
	tests := []struct {
		key      string
		wantNS   string
		wantName string
	}{
		{
			key:      "default/my-sync",
			wantNS:   "default",
			wantName: "my-sync",
		},
		{
			key:      "git-system/sync-controller",
			wantNS:   "git-system",
			wantName: "sync-controller",
		},
		{
			key:      "just-name",
			wantNS:   "",
			wantName: "just-name",
		},
		{
			key:      "",
			wantNS:   "",
			wantName: "",
		},
		{
			key:      "ns/name/extra",
			wantNS:   "ns",
			wantName: "name/extra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			ns, name, err := splitKey(tt.key)
			if err != nil {
				t.Fatalf("splitKey(%q) error = %v", tt.key, err)
			}
			if ns != tt.wantNS {
				t.Errorf("namespace = %q, want %q", ns, tt.wantNS)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}
