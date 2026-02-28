package resolver

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestChangeName(t *testing.T) {
	tests := []struct {
		name   string
		change *object.Change
		want   string
	}{
		{
			name: "from name takes precedence",
			change: &object.Change{
				From: object.ChangeEntry{Name: "file-from.txt"},
				To:   object.ChangeEntry{Name: "file-to.txt"},
			},
			want: "file-from.txt",
		},
		{
			name: "to name used when from is empty (new file)",
			change: &object.Change{
				From: object.ChangeEntry{Name: ""},
				To:   object.ChangeEntry{Name: "new-file.txt"},
			},
			want: "new-file.txt",
		},
		{
			name: "rename uses from name",
			change: &object.Change{
				From: object.ChangeEntry{Name: "old-name.txt"},
				To:   object.ChangeEntry{Name: "new-name.txt"},
			},
			want: "old-name.txt",
		},
		{
			name: "deleted file (no to name)",
			change: &object.Change{
				From: object.ChangeEntry{Name: "deleted.txt"},
				To:   object.ChangeEntry{Name: ""},
			},
			want: "deleted.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := changeName(tt.change)
			if got != tt.want {
				t.Errorf("changeName() = %q, want %q", got, tt.want)
			}
		})
	}
}
