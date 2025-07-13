package util

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/govdbot/govd/config"
	"go.uber.org/zap"
)

var (
	// track open files to prevent leaks
	openFiles      = make(map[string]*os.File)
	openFilesMutex sync.RWMutex
)

// creates a file and tracks it for cleanup
func SafeCreateFile(path string) (*os.File, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	openFilesMutex.Lock()
	openFiles[path] = file
	openFilesMutex.Unlock()

	return file, nil
}

// closes a file and removes it from tracking
func SafeCloseFile(file *os.File) error {
	if file == nil {
		return nil
	}

	path := file.Name()
	err := file.Close()

	openFilesMutex.Lock()
	delete(openFiles, path)
	openFilesMutex.Unlock()

	return err
}

// opens a file and tracks it for cleanup
func SafeOpenFile(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	openFilesMutex.Lock()
	openFiles[path] = file
	openFilesMutex.Unlock()

	return file, nil
}

// forcefully closes all tracked files
func CleanupOpenFiles() {
	openFilesMutex.Lock()
	defer openFilesMutex.Unlock()

	for path, file := range openFiles {
		if file != nil {
			zap.S().Warnf("force closing leaked file: %s", path)
			file.Close()
		}
	}
	openFiles = make(map[string]*os.File)
}

// ensures a file path is within the downloads directory
func EnsureFileInDownloadsDir(fileName string) string {
	if filepath.IsAbs(fileName) {
		return fileName
	}
	return filepath.Join(config.Env.DownloadsDirectory, fileName)
}

// removes files older than the specified duration
func CleanupOldFiles(dir string, maxAge time.Duration) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == dir {
			return nil
		}

		if time.Since(info.ModTime()) > maxAge {
			if info.IsDir() {
				zap.S().Debugf("removing old directory: %s", path)
				os.RemoveAll(path)
			} else {
				zap.S().Debugf("removing old file: %s", path)
				os.Remove(path)
			}
		}

		return nil
	})
}
