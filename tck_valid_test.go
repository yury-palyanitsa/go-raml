package raml

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// staticRoundTripper serves a fixed body for a specific URL and 404s for everything else.
type staticRoundTripper struct {
	url  string
	body string
}

func (s staticRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != s.url {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       http.NoBody,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

// tckValidSpecialCases maps fixture relative paths (forward-slash) to extra ParseOpts
// needed to run them without real network access.
var tckValidSpecialCases = map[string][]ParseOpt{
	// valid-https.raml includes a resource type fragment over HTTPS.
	// Serve the fragment content via a mock transport so the test runs offline
	// while still exercising the full HTTPS loading code path.
	"Root/include-02/valid-https.raml": {
		OptWithHTTPClient(&http.Client{Transport: staticRoundTripper{
			url:  "https://gist.githubusercontent.com/y0an/3a089190b27596bed143/raw/7adea5ea4ac1ae70febb38e1cce69aa98bb90c40/base.yaml",
			body: "#%RAML 1.0 ResourceType\ndelete?:\n  responses:\n    503:\n      description: |\n        Server Unavailable. Check Your Rate Limits.\npost?:\n  responses:\n    503:\n      description: |\n        Server Unavailable. Check Your Rate Limits.\nget?:\n  responses:\n    503:\n      description: |\n        Server Unavailable. Check Your Rate Limits.\n",
		}}),
	},
}

func Test_TCKValid(t *testing.T) {
	tckDir := "./raml-tck"
	if _, err := os.Stat(tckDir); err != nil {
		t.Skipf("raml-tck directory not found at %s: %v", tckDir, err)
	}

	// This test case is parsing, but !include is not resolved
	// raml-tck\ResourceTypes\include-parameter\valid.raml

	// Fixtures whose relative path starts with one of these prefixes are skipped.
	// Use forward-slash prefixes matching the raml-tck directory structure.
	skippedCategories := []string{
		// Overlay and Extension support not yet implemented.
		"Overlays/",
		"EdgeCases/overlay-overrides-resources/",
		"Fragments/extend-with-new-method/",
		"Fragments/extension/",
		"Fragments/extension-relative-include/",
		"Libraries/used-by-extension/",
		"Libraries/used-by-overlay/",
		// JSON pointer fragment syntax in !include paths is not yet supported.
		"Types/include-json-schema-element/",
	}

	var totalTests int
	var skippedTests int

	err := filepath.WalkDir(tckDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".raml") {
			return nil
		}
		if !strings.HasPrefix(filepath.Base(path), "valid") {
			return nil
		}

		relPath, _ := filepath.Rel(tckDir, path)
		relPath = filepath.ToSlash(relPath)

		shouldSkip := false
		for _, skip := range skippedCategories {
			if strings.HasPrefix(relPath, skip) {
				shouldSkip = true
				break
			}
		}

		if shouldSkip {
			skippedTests++
			return nil
		}

		totalTests++
		t.Run(relPath, func(t *testing.T) {
			opts := []ParseOpt{OptWithUnwrap(), OptWithValidate()}
			opts = append(opts, tckValidSpecialCases[relPath]...)
			_, err := ParseFromPath(path, opts...)
			require.NoError(t, err)
		})

		return nil
	})
	require.NoError(t, err)

	t.Logf("TCK Valid: %d tests run, %d tests skipped", totalTests, skippedTests)
}
