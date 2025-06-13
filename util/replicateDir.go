package util

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

func WatchReplicateDir(ctx context.Context, origdir string, replicdir string) {
	if !fs.ValidPath(replicdir) {
		os.Mkdir(replicdir, 0755)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	watcher.Add("./")

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			path := event.Name
			temp_path := filepath.Join(replicdir, path)

			if event.Has(fsnotify.Create) {
				if event.RenamedFrom == "" {
					fi, err := os.Stat(path)
					if err != nil {
						break
					}
					if fi.IsDir() {
						os.Mkdir(temp_path, fi.Mode().Perm())
					} else {
						f, err := os.Create(temp_path)
						if err != nil {
							panic(err)
						}
						f.Chmod(fi.Mode())
						f.Close()
					}
				} else {
					old_temp_path := filepath.Join(replicdir, event.RenamedFrom)
					if fs.ValidPath(temp_path) && fs.ValidPath(old_temp_path) {
						err := os.Rename(old_temp_path, temp_path)
						if err != nil {
							break
						}
					}
				}
			}

			if event.Has(fsnotify.Remove) {
				os.Remove(temp_path)
			}
			if event.Has(fsnotify.Write) {
				contents, _ := os.ReadFile(path)
				os.WriteFile(temp_path, contents, fs.FileMode(os.O_TRUNC))
			}
			if event.Has(fsnotify.Rename) {
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		case <-ctx.Done():
			watcher.Close()
			return
		}
	}
}
