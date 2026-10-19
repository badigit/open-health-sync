// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalArchiveIsCompleteAndDoesNotAppend(t *testing.T) {
	e, err := Open(fixtureZip(t))
	require.NoError(t, err)
	defer e.Close()
	output := filepath.Join(t.TempDir(), "result")
	result, err := WriteArchive(context.Background(), e, output)
	require.NoError(t, err)
	require.True(t, result.Complete)
	require.NotEmpty(t, result.Files)
	require.Len(t, result.DataSHA256, 64)
	_, err = WriteArchive(context.Background(), e, output)
	require.Error(t, err)
}

func TestUnknownTypeCannotPublishCompleteManifest(t *testing.T) {
	input := filepath.Join(t.TempDir(), "export.xml")
	require.NoError(t, os.WriteFile(input, []byte(`<HealthData><Record type="HKUnknownFutureType" startDate="2026-01-02 12:00:00 +0300" endDate="2026-01-02 12:00:00 +0300" value="1"/></HealthData>`), 0600))
	e, err := Open(input)
	require.NoError(t, err)
	defer e.Close()
	output := filepath.Join(t.TempDir(), "result")
	result, err := WriteArchive(context.Background(), e, output)
	require.Error(t, err)
	require.False(t, result.Complete)
	_, err = os.Stat(filepath.Join(output, "manifest.json"))
	require.True(t, os.IsNotExist(err))
}
