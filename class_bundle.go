package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func classBundleFromClassPath() (*SourceBundle, error) {
	seenDirs := map[string]bool{}
	seenFiles := map[string]bool{}
	var files []SourceBundleFile
	for _, dir := range classPath {
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		abs, err = filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		if seenDirs[abs] {
			continue
		}
		seenDirs[abs] = true
		collected, err := collectBundleFilesFromDir(abs)
		if err != nil {
			return nil, err
		}
		for _, file := range collected {
			if seenFiles[file.Path] {
				continue
			}
			seenFiles[file.Path] = true
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if err := validateSourceBundleSize(files); err != nil {
		return nil, err
	}
	digest := digestSourceBundle(files)
	return &SourceBundle{Digest: digest, Files: files}, nil
}

func collectBundleFilesFromDir(dir string) ([]SourceBundleFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	var files []SourceBundleFile
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)
		if entry.Type().IsRegular() && (strings.HasSuffix(name, ".smt") || strings.HasSuffix(name, ".st")) {
			file, err := readBundleFile(dir, path)
			if err != nil {
				return nil, err
			}
			files = append(files, file)
			continue
		}
		if !entry.IsDir() {
			continue
		}
		manifest := filepath.Join(path, name+".smt")
		if _, err := os.Stat(manifest); err != nil {
			continue
		}
		walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "target" || d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || skipForeignGeneratedFile(name, p) {
				return nil
			}
			file, err := readBundleFile(dir, p)
			if err != nil {
				return err
			}
			files = append(files, file)
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return files, nil
}

func skipForeignGeneratedFile(className string, path string) bool {
	base := filepath.Base(path)
	if base == className || base == className+".exe" {
		return true
	}
	return strings.HasPrefix(base, "_st_") || base == ".gitignore"
}

func readBundleFile(root string, path string) (SourceBundleFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SourceBundleFile{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return SourceBundleFile{}, err
	}
	return SourceBundleFile{Path: filepath.ToSlash(rel), Data: data}, nil
}

func digestSourceBundle(files []SourceBundleFile) string {
	h := sha256.New()
	for _, file := range files {
		h.Write([]byte(file.Path))
		h.Write([]byte{0})
		h.Write(file.Data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validateSourceBundleSize(files []SourceBundleFile) error {
	maxFiles := sourceBundleMaxFiles()
	if len(files) > maxFiles {
		return fmt.Errorf("source bundle has %d files, maximum is %d", len(files), maxFiles)
	}
	maxBytes := sourceBundleMaxBytes()
	total := 0
	for _, file := range files {
		total += len(file.Data)
		if total > maxBytes {
			return fmt.Errorf("source bundle has %d bytes, maximum is %d", total, maxBytes)
		}
	}
	return nil
}

func stageSourceBundle(bundle *SourceBundle) (string, error) {
	if bundle == nil || bundle.Digest == "" {
		return "", nil
	}
	if err := validateSourceBundleSize(bundle.Files); err != nil {
		return "", err
	}
	if got := digestSourceBundle(bundle.Files); got != bundle.Digest {
		return "", fmt.Errorf("source bundle digest mismatch: got %s expected %s", got, bundle.Digest)
	}
	root, err := sourceBundleCacheRoot()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(root, bundle.Digest)
	if err := os.MkdirAll(dst, 0755); err != nil {
		return "", err
	}
	for _, file := range bundle.Files {
		rel, err := safeBundlePath(file.Path)
		if err != nil {
			return "", err
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(out, file.Data, 0644); err != nil {
			return "", err
		}
	}
	return dst, nil
}

func sourceBundleCacheRoot() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	root := filepath.Join(base, "servicetalk", "source-bundles")
	if root == "" {
		return "", fmt.Errorf("empty source bundle cache root")
	}
	return root, nil
}

func safeBundlePath(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", fmt.Errorf("invalid bundle path %q", path)
	}
	if strings.Contains(path, "\\") || (len(path) >= 2 && path[1] == ':') {
		return "", fmt.Errorf("invalid bundle path %q", path)
	}
	rel := filepath.Clean(filepath.FromSlash(path))
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("invalid bundle path %q", path)
	}
	return rel, nil
}
