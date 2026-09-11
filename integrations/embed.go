// Package integrations embeds the portable agent connection bundle.
package integrations

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
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

//go:embed all:repotempo-skill install-skill.sh
var skillBundle embed.FS

var skillOnce sync.Once
var skillArchive []byte
var skillArchiveErr error

// SkillInstaller is public source; user credentials are supplied locally, never
// added to this response or to a download URL.
func SkillInstaller() ([]byte, error) { return skillBundle.ReadFile("install-skill.sh") }

// SkillArchive contains only the portable research Skill and shell helper.
func SkillArchive() ([]byte, error) {
	skillOnce.Do(func() {
		var output bytes.Buffer
		compressed := gzip.NewWriter(&output)
		writer := tar.NewWriter(compressed)
		skillArchiveErr = fs.WalkDir(skillBundle, "repotempo-skill", func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			contents, err := skillBundle.ReadFile(path)
			if err != nil {
				return err
			}
			name := "repotempo" + path[len("repotempo-skill"):]
			if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(contents)), ModTime: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)}); err != nil {
				return err
			}
			_, err = writer.Write(contents)
			return err
		})
		if err := writer.Close(); skillArchiveErr == nil {
			skillArchiveErr = err
		}
		if err := compressed.Close(); skillArchiveErr == nil {
			skillArchiveErr = err
		}
		if skillArchiveErr == nil {
			skillArchive = output.Bytes()
		}
	})
	return skillArchive, skillArchiveErr
}

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
