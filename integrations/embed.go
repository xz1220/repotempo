// Package integrations embeds the portable agent connection bundle.
package integrations

import (
	"archive/zip"
	"bytes"
	"embed"
	"io/fs"
	"sync"
	"time"
)

//go:embed all:repotempo-agent
var bundle embed.FS
var once sync.Once
var archive []byte
var archiveErr error

func AgentArchive() ([]byte, error) {
	once.Do(func() {
		var output bytes.Buffer
		writer := zip.NewWriter(&output)
		archiveErr = fs.WalkDir(bundle, "repotempo-agent", func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			contents, err := bundle.ReadFile(path)
			if err != nil {
				return err
			}
			header := &zip.FileHeader{Name: path, Method: zip.Deflate}
			header.SetModTime(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
			header.SetMode(0644)
			file, err := writer.CreateHeader(header)
			if err != nil {
				return err
			}
			_, err = file.Write(contents)
			return err
		})
		if err := writer.Close(); archiveErr == nil {
			archiveErr = err
		}
		if archiveErr == nil {
			archive = output.Bytes()
		}
	})
	return archive, archiveErr
}
