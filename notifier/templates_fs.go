package notifier

import (
	"io/fs"
	"os"
	"strings"
)

// TemplateDir is the on-disk directory holding editable message templates.
// main sets it to $DATA_DIR/templates when a data directory is configured,
// so a template edited through the Web UI persists across container
// recreation; without a data directory it stays "templates" next to the
// working directory (the Docker image ships the factory files there).
// Tests override this with a temp dir.
var TemplateDir = "templates"

// EmbeddedTemplates is the raw embedded template FS, with no disk overlay.
// main assigns it alongside TemplateFiles; GetTemplateDefaults reads the
// factory templates from here. It stays nil in tests that do not set it,
// in which case the disk-overlay FS is used as a fallback.
var EmbeddedTemplates fs.FS

// templateNames are the message templates the Web UI can read and write.
// The on-disk file for a name is "<name>.template".
var templateNames = []string{"plaintext", "markdown", "html"}

// TemplateNames returns the editable message template names.
func TemplateNames() []string {
	out := make([]string, len(templateNames))
	copy(out, templateNames)
	return out
}

// TemplateFileName returns the on-disk file name for a template name.
func TemplateFileName(name string) string {
	return name + ".template"
}

// TemplatesWithDiskFallback returns an fs.FS that serves message templates
// from the on-disk TemplateDir when present, falling back to the embedded
// templates for anything missing. Only paths under "templates/" are ever
// read from disk, and the file part must be a bare file name, so a template
// name can never escape the directory.
func TemplatesWithDiskFallback(embedded fs.FS) fs.FS {
	return overlayFS{disk: os.DirFS(TemplateDir), embedded: embedded}
}

type overlayFS struct {
	disk     fs.FS
	embedded fs.FS
}

func (o overlayFS) Open(name string) (fs.File, error) {
	if fs.ValidPath(name) && strings.HasPrefix(name, "templates/") {
		base := strings.TrimPrefix(name, "templates/")
		if base != "" && !strings.Contains(base, "/") && !strings.Contains(base, "\\") && base != "." && base != ".." {
			if f, err := o.disk.Open(base); err == nil {
				return f, nil
			}
		}
	}
	return o.embedded.Open(name)
}
