package push

import (
	"encoding/base64"
	"testing"
)

func TestSplitKey(t *testing.T) {
	tests := []struct {
		key       string
		wantNS    string
		wantName  string
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

func TestNestedFieldNoCopy(t *testing.T) {
	tests := []struct {
		name    string
		obj     map[string]interface{}
		fields  []string
		want    interface{}
		found   bool
	}{
		{
			name: "simple field",
			obj: map[string]interface{}{
				"key": "value",
			},
			fields: []string{"key"},
			want:   "value",
			found:  true,
		},
		{
			name: "nested field",
			obj: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": "deep-value",
				},
			},
			fields: []string{"level1", "level2"},
			want:   "deep-value",
			found:  true,
		},
		{
			name: "missing field",
			obj: map[string]interface{}{
				"key": "value",
			},
			fields: []string{"missing"},
			want:   nil,
			found:  false,
		},
		{
			name: "non-map intermediate",
			obj: map[string]interface{}{
				"key": "not-a-map",
			},
			fields: []string{"key", "subkey"},
			want:   nil,
			found:  false,
		},
		{
			name:   "empty object",
			obj:    map[string]interface{}{},
			fields: []string{"key"},
			want:   nil,
			found:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found, err := nestedFieldNoCopy(tt.obj, tt.fields...)
			if err != nil {
				t.Fatalf("nestedFieldNoCopy() error = %v", err)
			}
			if found != tt.found {
				t.Errorf("found = %v, want %v", found, tt.found)
			}
			if found && got != tt.want {
				t.Errorf("value = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnstructuredNestedStringMap(t *testing.T) {
	tests := []struct {
		name      string
		obj       map[string]interface{}
		fields    []string
		wantMap   map[string]string
		wantFound bool
		wantErr   bool
	}{
		{
			name: "valid string map",
			obj: map[string]interface{}{
				"data": map[string]interface{}{
					"username": base64.StdEncoding.EncodeToString([]byte("admin")),
					"password": base64.StdEncoding.EncodeToString([]byte("secret")),
				},
			},
			fields: []string{"data"},
			wantMap: map[string]string{
				"username": base64.StdEncoding.EncodeToString([]byte("admin")),
				"password": base64.StdEncoding.EncodeToString([]byte("secret")),
			},
			wantFound: true,
		},
		{
			name: "missing data field",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{},
			},
			fields:    []string{"data"},
			wantFound: false,
		},
		{
			name: "non-string values are skipped",
			obj: map[string]interface{}{
				"data": map[string]interface{}{
					"string-key": "string-value",
					"int-key":    42,
					"bool-key":   true,
				},
			},
			fields: []string{"data"},
			wantMap: map[string]string{
				"string-key": "string-value",
			},
			wantFound: true,
		},
		{
			name: "not a map type",
			obj: map[string]interface{}{
				"data": "just-a-string",
			},
			fields:  []string{"data"},
			wantErr: true,
		},
		{
			name: "empty map",
			obj: map[string]interface{}{
				"data": map[string]interface{}{},
			},
			fields:    []string{"data"},
			wantMap:   map[string]string{},
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found, err := unstructuredNestedStringMap(tt.obj, tt.fields...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if tt.wantMap != nil {
				if len(got) != len(tt.wantMap) {
					t.Errorf("map length = %d, want %d", len(got), len(tt.wantMap))
				}
				for k, v := range tt.wantMap {
					if got[k] != v {
						t.Errorf("map[%q] = %q, want %q", k, got[k], v)
					}
				}
			}
		})
	}
}

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
