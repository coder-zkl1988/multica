package daemon

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/designdocument"
)

// collectDesignDocumentLivePreview reads only regular presentation files rooted
// in this task's output. It never follows links into repository or user data.
func collectDesignDocumentLivePreview(envRoot string) (designdocument.LivePreview, error) {
	snapshot := designdocument.LivePreview{Files: map[string][]byte{}}
	root, err := os.OpenRoot(envRoot)
	if err != nil {
		return snapshot, err
	}
	defer root.Close()
	for _, name := range []string{"output", "output/design-document"} {
		info, err := root.Lstat(name)
		if err != nil {
			return snapshot, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return snapshot, errors.New("preview root is not a regular directory")
		}
	}
	output, err := root.OpenRoot("output/design-document")
	if err != nil {
		return snapshot, err
	}
	defer output.Close()
	total := 0
	for _, dir := range []string{"prototype", "assets"} {
		err = fs.WalkDir(output.FS(), dir, func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if name == dir && errors.Is(walkErr, fs.ErrNotExist) {
					return nil
				}
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("preview contains a symbolic link")
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() || !designdocument.LivePreviewPathAllowed(name) {
				return nil
			}
			if len(snapshot.Files) >= designdocument.LivePreviewMaxFiles {
				return errors.New("too many preview files")
			}
			file, err := output.Open(name)
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(file, int64(designdocument.LivePreviewMaxBytes-total+1)))
			file.Close()
			if err != nil {
				return err
			}
			total += len(data)
			if total > designdocument.LivePreviewMaxBytes {
				return errors.New("preview exceeds size limit")
			}
			snapshot.Files[filepath.ToSlash(name)] = data
			return nil
		})
		if err != nil {
			return snapshot, err
		}
	}
	if len(snapshot.Files["prototype/index.html"]) > 0 {
		snapshot.EntryPath = "prototype/index.html"
	} else {
		var pages []string
		for name := range snapshot.Files {
			if strings.HasPrefix(name, "prototype/") && path.Ext(name) == ".html" {
				pages = append(pages, name)
			}
		}
		sort.Strings(pages)
		if len(pages) > 0 {
			snapshot.EntryPath = pages[0]
		}
	}
	return snapshot, snapshot.Validate()
}

// startDesignDocumentLivePreview publishes changing output while the provider
// runs. Stopping joins the worker before final package validation can start.
func (d *Daemon) startDesignDocumentLivePreview(ctx context.Context, task Task, envRoot string) func() {
	if !isDesignDocumentTask(task) {
		return func() {}
	}
	liveCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		lastDigest := ""
		for {
			select {
			case <-liveCtx.Done():
				return
			case <-ticker.C:
				snapshot, err := collectDesignDocumentLivePreview(envRoot)
				if err != nil {
					continue
				}
				digest, err := snapshot.Digest()
				if err != nil || digest == lastDigest {
					continue
				}
				sendCtx, stop := context.WithTimeout(liveCtx, 5*time.Second)
				err = d.client.postJSON(sendCtx, "/api/daemon/tasks/"+task.ID+"/design-document-live-preview", snapshot, nil)
				stop()
				if err == nil {
					lastDigest = digest
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}
