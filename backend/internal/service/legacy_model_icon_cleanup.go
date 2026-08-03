package service

import (
	"os"
	"path/filepath"
)

// RemoveLegacyChannelModelIcons removes the retired, server-managed model icon directory.
// Model presentation now uses the built-in brand registry exclusively.
func (s *Service) RemoveLegacyChannelModelIcons() error {
	return os.RemoveAll(filepath.Join(s.dataDir, "model-icons"))
}
