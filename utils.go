package trimpb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadProtos(sourceRoots []string) (map[string]string, error) {
	protoContents := make(map[string]string)
	visitedFiles := make(map[string]struct{})

	for _, root := range sourceRoots {
		// filepath.Walk recursively traverses the file tree rooted at `root`.
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".proto") {
				// Use the absolute path to ensure a file is only processed once, even if
				// it's reachable from multiple overlapping source roots.
				absPath, err := filepath.Abs(path)
				if err != nil {
					return err
				}
				if _, exists := visitedFiles[absPath]; exists {
					return nil
				}
				visitedFiles[absPath] = struct{}{}

				// The map key is the file path relative to the import root, which is
				// how the protobuf compiler resolves `import` statements.
				relPath, err := filepath.Rel(root, path)
				if err != nil {
					return fmt.Errorf("could not get relative path for %s: %w", path, err)
				}

				content, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("could not read file %s: %w", path, err)
				}
				protoContents[relPath] = string(content)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("error walking path %s: %w", root, err)
		}
	}
	return protoContents, nil
}
