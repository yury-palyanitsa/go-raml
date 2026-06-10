package raml

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// PathToFileURI converts an absolute OS path to a file:// URI string.
// It is idempotent: if osPath already starts with "file://", it is returned
// unchanged. The path is cleaned before encoding so that equivalent paths
// (e.g. "/a/b/../c" and "/a/c") always produce the same URI.
func PathToFileURI(osPath string) string {
	if strings.HasPrefix(osPath, "file://") {
		return osPath
	}
	slashPath := filepath.ToSlash(filepath.Clean(osPath))
	if runtime.GOOS == "windows" {
		// On Windows, absolute paths look like "C:/foo/bar".
		// A proper file URI needs a leading slash: "file:///C:/foo/bar".
		if !strings.HasPrefix(slashPath, "/") {
			slashPath = "/" + slashPath
		}
	}
	u := url.URL{Scheme: "file", Host: "", Path: slashPath}
	return u.String()
}

// FileURIToPath converts a file:// URI to an OS filesystem path.
//
// Internal code operates exclusively on file:// URIs: OS paths supplied by
// the user are canonicalized to URIs at the boundary by PathToFileURI (in
// ParseFromPath, applyParserOptions, the LSP NormalizeURI, etc.). This
// function therefore requires a file URI as input and returns an empty
// string for any other shape (bare path, http URI, garbage); a non-empty
// caller seeing "" indicates a programmer error upstream of the boundary.
//
// File-loading code that needs to surface the parse error rather than
// fail silently should use fileURIToOSPath.
func FileURIToPath(uri string) string {
	p, err := fileURIToOSPath(uri)
	if err != nil {
		return ""
	}
	return p
}

// fileURIToOSPath converts a file:// URI to a filesystem path. Any other
// input (bare OS path, http://, https://, garbage) is rejected with an
// error. Loaders that pass the result to the OS (os.Open,
// safeopen.OpenBeneath, filepath.Rel, ...) must use this so a stray
// non-file value cannot be silently interpreted as a path.
func fileURIToOSPath(uri string) (string, error) {
	if !strings.HasPrefix(uri, "file://") {
		return "", fmt.Errorf("not a file URI: %q", uri)
	}
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid file URI %q: %w", uri, err)
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		// "file:///C:/foo/bar" -> Path="/C:/foo/bar" -> strip leading slash
		p = strings.TrimPrefix(p, "/")
		p = filepath.FromSlash(p)
	}
	// Clean so that "dir/../other" resolves to "other" consistently.
	return filepath.Clean(p), nil
}

// uriBase returns the last element of a URI path, analogous to filepath.Base.
func uriBase(uri string) string {
	u, err := url.Parse(uri)
	if err == nil && u.Scheme != "" {
		return path.Base(u.Path)
	}
	return path.Base(uri)
}

// uriScheme returns the scheme portion of a URI (e.g. "file", "http", "https"),
// or an empty string if the URI has no scheme. Only the known schemes produced
// by this package are matched to avoid false positives on arbitrary strings.
func uriScheme(uri string) string {
	switch {
	case strings.HasPrefix(uri, "file://"):
		return "file"
	case strings.HasPrefix(uri, "https://"):
		return "https"
	case strings.HasPrefix(uri, "http://"):
		return "http"
	default:
		return ""
	}
}

// resolveURIRef resolves ref (which may be relative) against the absolute base
// URI using standard RFC 3986 resolution (net/url.URL.ResolveReference).
// On Windows, ref is normalised from OS-style backslashes to forward slashes
// before parsing so that relative paths produced by filepath.Rel resolve correctly.
func resolveURIRef(base, ref string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid base URI \"%q\": %w", base, err)
	}
	// Normalise the reference: OS path separators (backslashes on Windows) are
	// not URL path separators — convert before parsing to get correct resolution.
	refURL, err := url.Parse(filepath.ToSlash(ref))
	if err != nil {
		return "", fmt.Errorf("invalid reference \"%q\": %w", ref, err)
	}
	return baseURL.ResolveReference(refURL).String(), nil
}
