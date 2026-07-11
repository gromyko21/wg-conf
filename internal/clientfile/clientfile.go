package clientfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Load reads angristan-style client config: {dir}/{iface}-client-{name}.conf
func Load(dirs []string, iface, name string) (string, error) {
	filename := fmt.Sprintf("%s-client-%s.conf", iface, name)
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, filename)
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
	}
	return "", os.ErrNotExist
}

// Remove deletes angristan-style client config file if present.
func Remove(dirs []string, iface, name string) {
	filename := fmt.Sprintf("%s-client-%s.conf", iface, name)
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		_ = os.Remove(filepath.Join(dir, filename))
	}
}

// Save writes client config in angristan format.
func Save(dir, iface, name, content string) error {
	if dir == "" {
		return nil
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-client-%s.conf", iface, name))
	return os.WriteFile(path, []byte(content), 0o600)
}
