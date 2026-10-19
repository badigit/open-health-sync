// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// ArchiveFile accounts for an original attachment; its bytes stay in the source.
type ArchiveFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Role   string `json:"role"`
}

// ArchiveManifest is published only after complete, error-free conversion.
// Original files must be retained alongside this local derivative.
type ArchiveManifest struct {
	Version    int           `json:"version"`
	Complete   bool          `json:"complete"`
	Stats      Stats         `json:"stats"`
	DataSHA256 string        `json:"data_sha256"`
	Files      []ArchiveFile `json:"files"`
}

// WriteArchive creates a private local derivative, never an HTTP upload.
// It refuses an existing directory, so retries cannot append duplicate rows.
func WriteArchive(ctx context.Context, e *Export, directory string) (ArchiveManifest, error) {
	result := ArchiveManifest{Version: 1}
	if err := os.Mkdir(directory, 0700); err != nil {
		return result, err
	}
	file, err := os.OpenFile(filepath.Join(directory, "records.ndjson.gz"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	digest := sha256.New()
	compressed := gzip.NewWriter(io.MultiWriter(file, digest))
	stats, convertErr := Convert(ctx, e, Options{Archive: true, Deduplicate: true}, func(p Part) error {
		_, writeErr := compressed.Write(p.NDJSON)
		return writeErr
	})
	zipErr := compressed.Close()
	closeErr := file.Close()
	result.Stats = stats
	if err := errors.Join(convertErr, zipErr, closeErr); err != nil {
		return result, err
	}
	if len(stats.Invalid) > 0 || len(stats.Skipped) > 0 || stats.NoRoute > 0 {
		return result, errors.New("incomplete source coverage; archive not marked complete")
	}
	result.DataSHA256 = hex.EncodeToString(digest.Sum(nil))
	var names []string
	if e.zr != nil {
		for name, f := range e.files {
			if !f.FileInfo().IsDir() {
				names = append(names, name)
			}
		}
	} else {
		names = append(names, filepath.Base(e.xmlPath))
		for _, folder := range []string{"electrocardiograms", "workout-routes"} {
			entries, err := os.ReadDir(filepath.Join(e.root, folder))
			if err != nil && !os.IsNotExist(err) {
				return result, err
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					names = append(names, folder+"/"+entry.Name())
				}
			}
		}
		if _, err := os.Stat(filepath.Join(e.root, "export_cda.xml")); err == nil {
			names = append(names, "export_cda.xml")
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		reader, err := e.openFile(name)
		if err != nil {
			return result, err
		}
		hash := sha256.New()
		size, readErr := io.Copy(hash, reader)
		closeErr := reader.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return result, err
		}
		role := "source-attachment"
		switch {
		case name == "export_cda.xml":
			role = "cda-alternate-representation"
		case path.Ext(name) == ".xml":
			role = "healthdata-source"
		case path.Ext(name) == ".csv":
			role = "ecg-source"
		case path.Ext(name) == ".gpx":
			role = "route-source"
		}
		result.Files = append(result.Files, ArchiveFile{Name: name, SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: size, Role: role})
	}
	result.Complete = true
	manifest, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, err
	}
	err = os.WriteFile(filepath.Join(directory, "manifest.json"), append(manifest, '\n'), 0600)
	return result, err
}
