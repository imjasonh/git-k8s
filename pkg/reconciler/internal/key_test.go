package internal

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
			key:      "default/my-resource",
			wantNS:   "default",
			wantName: "my-resource",
		},
		{
			key:      "kube-system/controller",
			wantNS:   "kube-system",
			wantName: "controller",
		},
		{
			key:      "no-namespace",
			wantNS:   "",
			wantName: "no-namespace",
		},
		{
			key:      "ns/name/with/slashes",
			wantNS:   "ns",
			wantName: "name/with/slashes",
		},
		{
			key:      "",
			wantNS:   "",
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			ns, name := splitKey(tt.key)
			if ns != tt.wantNS {
				t.Errorf("namespace = %q, want %q", ns, tt.wantNS)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}
