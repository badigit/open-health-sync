// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalizedArchiveName(t *testing.T) {
	name := filepath.Join(t.TempDir(), "archive.zip")
	f, err := os.Create(name)
	require.NoError(t, err)
	z := zip.NewWriter(f)
	for n, body := range map[string]string{"apple_health_export/export_cda.xml": "<ClinicalDocument/>", "apple_health_export/экспорт.xml": "<HealthData locale=\"ru_RU\"><ExportDate value=\"2026-01-02 12:00:00 +0300\"/></HealthData>"} {
		w, err := z.Create(n)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	require.NoError(t, f.Close())
	e, err := Open(name)
	require.NoError(t, err)
	defer e.Close()
	date, err := e.ExportDate()
	require.NoError(t, err)
	require.Equal(t, "2026-01-02 12:00:00 +0300", date)
}

func TestRussianECGDecimalComma(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "electrocardiograms"), 0700))
	name := filepath.Join(dir, "export.xml")
	require.NoError(t, os.WriteFile(name, []byte("<HealthData/>"), 0600))
	data := "Дата записи,2026-01-02 12:00:00 +0300\nКлассификация,Синусовый ритм\nСимптомы,Нет\nЧастота замеров,256 Гц\nЕдиницы,мкВ\nОтведение,I\n\n12,5\n-3,25\n7\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "electrocardiograms", "sample.csv"), []byte(data), 0600))
	parts, stats := convertFile(t, name, Options{})
	require.Empty(t, stats.Invalid)
	require.Len(t, parts, 1)
	rows := lines(t, parts[0])
	require.Len(t, rows, 4)
	require.Equal(t, float64(3), rows[0]["numberOfVoltageMeasurements"])
	require.Equal(t, float64(256), rows[0]["samplingFrequency"])
	require.Equal(t, float64(1), rows[0]["classification"])
	require.Equal(t, "2026-01-02T09:00:00.01171875Z", rows[0]["endDate"])
	require.InDelta(t, 12.5e-6, rows[1]["value"], 1e-15)
	require.InDelta(t, -3.25e-6, rows[2]["value"], 1e-15)
}

func TestArchivePreservesHRVAndDeduplicates(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "export.xml")
	rec := `<Record type="HKQuantityTypeIdentifierHeartRateVariabilitySDNN" sourceName="Invented Watch" sourceVersion="1" device="sample-device" unit="ms" creationDate="2026-01-03 00:00:00 +0300" startDate="2026-01-02 23:59:59 +0300" endDate="2026-01-03 00:00:01 +0300" value="42"><HeartRateVariabilityMetadataList><InstantaneousBeatsPerMinute bpm="61.25" time="23:59:59.500"/><InstantaneousBeatsPerMinute bpm="62.5" time="00:00:00.500"/></HeartRateVariabilityMetadataList></Record>`
	require.NoError(t, os.WriteFile(name, []byte("<HealthData>"+rec+rec+`<Record type="HKDataTypeSleepDurationGoal" unit="hr" startDate="2026-01-02 12:00:00 +0300" endDate="2026-01-02 12:00:00 +0300" value="8"/><ActivitySummary dateComponents="2026-01-02" activeEnergyBurned="12.50"/></HealthData>`), 0600))
	parts, stats := convertFile(t, name, Options{Archive: true, Deduplicate: true})
	require.Empty(t, stats.Skipped)
	require.Empty(t, stats.Invalid)
	require.Equal(t, 1, stats.Duplicates["HKQuantityTypeIdentifierHeartRateVariabilitySDNN"])
	var rows []map[string]any
	for _, part := range parts {
		rows = append(rows, lines(t, part)...)
	}
	require.Len(t, rows, 4)
	for _, row := range rows {
		require.NotEmpty(t, row["sourceId"])
		require.NotNil(t, row["_source"])
		if row["quantityType"] == "HKQuantityTypeIdentifierHeartRateVariabilitySDNN" {
			bpm := row["instantaneousBPM"].([]any)
			require.Len(t, bpm, 2)
			require.Equal(t, "00:00:00.500", bpm[1].(map[string]any)["time"])
			require.Equal(t, "62.5", bpm[1].(map[string]any)["bpm"])
		}
	}
}

func TestArchiveDoesNotMergeDifferentDevices(t *testing.T) {
	name := filepath.Join(t.TempDir(), "export.xml")
	rec := `<Record type="HKQuantityTypeIdentifierHeartRate" device="DEVICE" unit="count/min" startDate="2026-01-02 12:00:00 +0300" endDate="2026-01-02 12:00:00 +0300" value="70"/>`
	require.NoError(t, os.WriteFile(name, []byte("<HealthData>"+strings.ReplaceAll(rec, "DEVICE", "A")+strings.ReplaceAll(rec, "DEVICE", "B")+"</HealthData>"), 0600))
	_, stats := convertFile(t, name, Options{Archive: true, Deduplicate: true})
	require.Equal(t, 3, stats.Lines)
	require.Empty(t, stats.Duplicates)
}

func TestArchiveStopsOnBrokenWaveform(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "electrocardiograms"), 0700))
	name := filepath.Join(dir, "export.xml")
	require.NoError(t, os.WriteFile(name, []byte("<HealthData/>"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "electrocardiograms", "sample.csv"), []byte("Recorded Date,2026-01-02 12:00:00 +0300\n1.5\nbroken-sample\n2.5\n"), 0600))
	e, err := Open(name)
	require.NoError(t, err)
	defer e.Close()
	_, err = Convert(context.Background(), e, Options{Archive: true}, func(Part) error { return nil })
	require.Error(t, err)
}

func TestArchiveKeepsMissingQuantityTypes(t *testing.T) {
	for _, typ := range []string{"AppleWalkingSteadiness", "HeartRateRecoveryOneMinute", "SixMinuteWalkTestDistance", "StairAscentSpeed", "StairDescentSpeed", "SwimmingStrokeCount", "TimeInDaylight", "WalkingAsymmetryPercentage", "WalkingDoubleSupportPercentage", "WalkingStepLength"} {
		t.Run(typ, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "export.xml")
			xml := `<HealthData><Record type="HKQuantityTypeIdentifier` + typ + `" unit="count" startDate="2026-01-02 12:00:00 +0300" endDate="2026-01-02 12:00:00 +0300" value="1.25" customAttribute="preserve"/></HealthData>`
			require.NoError(t, os.WriteFile(name, []byte(xml), 0600))
			parts, stats := convertFile(t, name, Options{Archive: true, Deduplicate: true})
			require.Empty(t, stats.Skipped)
			var found bool
			for _, p := range parts {
				for _, row := range lines(t, p) {
					if row["quantityType"] == "HKQuantityTypeIdentifier"+typ {
						found = true
						require.Equal(t, float64(1.25), row["value"])
						raw := row["_source"].(map[string]any)
						require.Equal(t, "preserve", raw["attributes"].(map[string]any)["customAttribute"])
					}
				}
			}
			require.True(t, found)
		})
	}
}

func TestArchiveDerivedRowsLinkToParents(t *testing.T) {
	parts, _ := convertFile(t, fixtureZip(t), Options{Archive: true, Deduplicate: true})
	ids := map[string]bool{}
	var parents []string
	for _, p := range parts {
		for _, row := range lines(t, p) {
			ids[row["sourceId"].(string)] = true
			if parent, ok := row["parentSourceId"].(string); ok {
				parents = append(parents, parent)
			}
		}
	}
	require.NotEmpty(t, parents)
	for _, parent := range parents {
		require.True(t, ids[parent], "derived row must reference its retained parent")
	}
}
