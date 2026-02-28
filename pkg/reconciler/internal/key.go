package internal

import "strings"

// splitKey splits a "namespace/name" key into its components.
// If the key has no slash, the namespace is empty and the whole key is the name.
func splitKey(key string) (namespace, name string) {
	namespace, name, _ = strings.Cut(key, "/")
	if name == "" {
		// No slash found: Cut returns (key, "", false). The key is the name.
		return "", namespace
	}
	return namespace, name
}
