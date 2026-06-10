package raml

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/safeopen"
)

// ResourceLoader loads a resource identified by an absolute URI.
// Implementations handle specific URI schemes (file://, https://, etc.).
type ResourceLoader interface {
	Load(uri string) (io.ReadCloser, error)
}

// OSFileLoader implements ResourceLoader for file:// URIs using os.Open
// directly. It performs no path-traversal or symlink-escape checks; the
// returned file is whatever path the URI names.
//
// SECURITY: OSFileLoader is appropriate when all URIs handed to it are
// trusted, or when the surrounding context is itself sandboxed (e.g. a CLI
// run by a developer over their own files). For untrusted input, or to
// constrain loads to a known directory subtree, use SafeOSFileLoader.
type OSFileLoader struct{}

func (OSFileLoader) Load(uri string) (io.ReadCloser, error) {
	path, err := fileURIToOSPath(uri)
	if err != nil {
		return nil, err
	}
	return os.Open(path) //nolint:gosec
}

// SafeOSFileLoader implements ResourceLoader for file:// URIs and constrains
// reads to the Root directory subtree. It uses safeopen.OpenBeneath, which
// relies on OS-level primitives that prevent traversal even via symlinks:
//
//   - Linux: openat2 with RESOLVE_BENEATH; symlinks that resolve inside Root
//     are followed, symlinks that would escape Root are rejected.
//   - Windows: each path component is opened via NtCreateFile with
//     FILE_OPEN_REPARSE_POINT, so symlinks and junctions are not followed.
//
// Root must be an absolute path. URIs whose file path is not lexically
// beneath Root are rejected before the open is attempted.
type SafeOSFileLoader struct {
	Root string
}

func (s SafeOSFileLoader) Load(uri string) (io.ReadCloser, error) {
	path, err := fileURIToOSPath(uri)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(s.Root, path)
	if err != nil {
		return nil, fmt.Errorf("path %q is outside workspace root %q: %w", path, s.Root, err)
	}
	// filepath.Rel returns a clean, non-empty relative path. The only shape
	// it can produce that escapes Root is a leading "..", which we reject
	// here for a clearer error than safeopen would give.
	//
	// Do not hand-construct rel without going through filepath.Rel. safeopen
	// on non-Linux Unix (macOS/BSDs/illumos/Solaris/AIX) accepts a leading
	// "/" in its argument as an escape from the base directory; filepath.Rel
	// cannot produce that shape, so we are not exposed through this path.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path %q is outside workspace root %q", path, s.Root)
	}
	f, err := safeopen.OpenBeneath(s.Root, rel)
	if err != nil {
		// safeopen returns raw syscall errors. Wrap as *os.PathError so callers
		// see the "open <path>: <reason>" shape produced by os.Open, and
		// normalize not-found errors to fs.ErrNotExist so the message and the
		// errors.Is(_, fs.ErrNotExist) predicate are stable across platforms.
		if isNotExistErr(err) {
			err = fs.ErrNotExist
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return f, nil
}

// isNotExistErr reports whether err denotes a missing file. safeopen on Linux
// returns a syscall.Errno that satisfies errors.Is(err, fs.ErrNotExist); on
// Windows it returns a raw windows.NTStatus that does not chain into the
// fs.ErrNotExist sentinel, so we match its message as a fallback.
func isNotExistErr(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Object Name not found") ||
		strings.Contains(msg, "Object Path not found")
}

// HTTPLoader implements ResourceLoader for http:// and https:// URIs.
type HTTPLoader struct {
	Client *http.Client
}

func (l HTTPLoader) Load(uri string) (io.ReadCloser, error) {
	c := l.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Get(uri) //nolint:noctx // callers that need context should wrap
	if err != nil {
		return nil, fmt.Errorf("http get %s: %w", uri, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("http get %s: status %d", uri, resp.StatusCode)
	}
	return resp.Body, nil
}

// SchemeLoader dispatches Load calls to per-scheme ResourceLoader implementations.
type SchemeLoader map[string]ResourceLoader

func (s SchemeLoader) Load(uri string) (io.ReadCloser, error) {
	scheme := uriScheme(uri)
	l, ok := s[scheme]
	if !ok {
		return nil, fmt.Errorf("no loader registered for URI scheme %q", scheme)
	}
	return l.Load(uri)
}

// buildSchemeLoader constructs the base ResourceLoader for a parse session
// from the resolved parserOptions and the effective workspace root.
//
// The file:// loader is chosen in this order of precedence:
//  1. pOpts.fileLoader if set via OptWithFileLoader — the caller owns its
//     safety; the workspace root no longer constrains file:// reads.
//  2. SafeOSFileLoader bound to workspaceRoot otherwise.
//
// HTTP/HTTPS loaders are included only if pOpts.httpClient was provided.
func buildSchemeLoader(pOpts *parserOptions, workspaceRoot string) ResourceLoader {
	var fileLoader ResourceLoader
	if pOpts.fileLoader != nil {
		fileLoader = pOpts.fileLoader
	} else {
		fileLoader = SafeOSFileLoader{Root: workspaceRoot}
	}
	sl := SchemeLoader{"file": fileLoader}
	if pOpts.httpClient != nil {
		hl := HTTPLoader{Client: pOpts.httpClient}
		sl["http"] = hl
		sl["https"] = hl
	}
	return sl
}
