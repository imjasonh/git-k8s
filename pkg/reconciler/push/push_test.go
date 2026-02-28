package push

import (
	"encoding/base64"
	"testing"
)

func TestBase64Decoding(t *testing.T) {
	// Verify that values typically stored in Kubernetes secrets
	// (base64-encoded via dynamic client) decode correctly.
	tests := []struct {
		name     string
		encoded  string
		wantText string
	}{
		{
			name:     "simple username",
			encoded:  base64.StdEncoding.EncodeToString([]byte("admin")),
			wantText: "admin",
		},
		{
			name:     "password with special chars",
			encoded:  base64.StdEncoding.EncodeToString([]byte("p@ss!w0rd#123")),
			wantText: "p@ss!w0rd#123",
		},
		{
			name:     "empty string",
			encoded:  base64.StdEncoding.EncodeToString([]byte("")),
			wantText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := base64.StdEncoding.DecodeString(tt.encoded)
			if err != nil {
				t.Fatalf("DecodeString() error = %v", err)
			}
			if string(decoded) != tt.wantText {
				t.Errorf("decoded = %q, want %q", string(decoded), tt.wantText)
			}
		})
	}
}
