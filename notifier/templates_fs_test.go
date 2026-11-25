package notifier

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestTemplatesWithDiskFallback(t *testing.T) {
	embedded := fstest.MapFS{
		"templates/plaintext.template": {Data: []byte("embedded plaintext")},
		"templates/markdown.template":  {Data: []byte("embedded markdown")},
		"templates/html.template":      {Data: []byte("embedded html")},
	}
	dir := t.TempDir()
	origDir := TemplateDir
	TemplateDir = dir
	defer func() { TemplateDir = origDir }()

	// A disk file wins over the embedded copy.
	if err := os.WriteFile(filepath.Join(dir, "plaintext.template"), []byte("disk plaintext"), 0644); err != nil {
		t.Fatal(err)
	}
	overlay := TemplatesWithDiskFallback(embedded)

	data, err := fs.ReadFile(overlay, "templates/plaintext.template")
	if err != nil || string(data) != "disk plaintext" {
		t.Errorf("disk template = %q, err = %v, want %q", string(data), err, "disk plaintext")
	}

	// A template missing on disk falls back to the embedded copy.
	data, err = fs.ReadFile(overlay, "templates/markdown.template")
	if err != nil || string(data) != "embedded markdown" {
		t.Errorf("fallback template = %q, err = %v, want %q", string(data), err, "embedded markdown")
	}

	// A path that tries to escape templates/ must not reach the disk.
	if _, err := fs.ReadFile(overlay, "templates/../secret"); err == nil {
		t.Error("expected an error reading templates/../secret")
	}
	if _, err := fs.ReadFile(overlay, "templates/sub/nested.template"); err == nil {
		t.Error("expected an error reading a nested template path")
	}
}
