package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type SourceRef struct {
	Requested string `json:"requested"`
	Resolved  string `json:"resolved"`
	Size      int64  `json:"size"`
}

type SourceError struct {
	Code string
	Path string
	Err  error
}

func (e *SourceError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Path)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Path, e.Err)
}

func (e *SourceError) Unwrap() error { return e.Err }

func ReadLocal(baseDir, requested string, roots []string, maxBytes int64) ([]byte, SourceRef, error) {
	if maxBytes <= 0 {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_LIMIT_INVALID", Path: requested}
	}
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_BASE_INVALID", Path: baseDir, Err: err}
	}
	candidate := requested
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}
	candidate, err = filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_PATH_INVALID", Path: requested, Err: err}
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_OPEN_FAILED", Path: requested, Err: err}
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_PATH_INVALID", Path: requested, Err: err}
	}

	if len(roots) == 0 {
		roots = []string{base}
	}
	allowed := false
	for _, root := range roots {
		canonicalRoot, rootErr := filepath.Abs(root)
		if rootErr != nil {
			continue
		}
		if evaluated, evalErr := filepath.EvalSymlinks(canonicalRoot); evalErr == nil {
			canonicalRoot = evaluated
		}
		if withinRoot(canonicalRoot, resolved) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_ROOT_ESCAPE", Path: requested}
	}

	file, err := os.Open(resolved)
	if err != nil {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_OPEN_FAILED", Path: requested, Err: err}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_STAT_FAILED", Path: requested, Err: err}
	}
	if !info.Mode().IsRegular() {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_NOT_REGULAR", Path: requested}
	}

	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_READ_FAILED", Path: requested, Err: err}
	}
	if int64(len(content)) > maxBytes {
		return nil, SourceRef{}, &SourceError{Code: "SOURCE_TOO_LARGE", Path: requested}
	}
	return content, SourceRef{Requested: requested, Resolved: resolved, Size: int64(len(content))}, nil
}

func withinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
