// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const defaultECGRate = 512.0

var ecgClassifications = map[string]int{
	"sinus rhythm": 1, "синусовый ритм": 1,
	"atrial fibrillation": 2, "фибрилляция предсердий": 2,
	"heart rate under 50": 3, "low heart rate": 3, "низкий пульс": 3,
	"heart rate over 120": 4, "heart rate over 150": 4, "high heart rate": 4, "высокий пульс": 4,
	"poor recording": 5, "плохая запись": 5,
	"inconclusive": 6, "неопределенный": 6, "неопределённый": 6,
}

func (c *converter) readECGs() error {
	files, err := c.exp.ecgFiles()
	if err != nil {
		return err
	}
	for _, name := range files {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		c.stats.Records++
		if err := c.ecg(name); err != nil {
			return err
		}
	}
	return nil
}

func ecgNumber(text string) (float64, bool) {
	text = strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "\""))
	if strings.Contains(text, ",") {
		if strings.Count(text, ",") != 1 || strings.Contains(text, ".") {
			return 0, false
		}
		text = strings.ReplaceAll(text, ",", ".")
	}
	n, err := strconv.ParseFloat(text, 64)
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}

// ecg reads whole numeric lines before CSV headers: in Russian exports an
// unquoted decimal comma is a decimal separator, not a second CSV column.
func (c *converter) ecg(name string) error {
	rc, err := c.exp.openFile(name)
	if err != nil {
		return err
	}
	defer rc.Close()
	digest := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(rc, digest))
	scanner.Buffer(make([]byte, 65536), 1<<20)
	var recorded time.Time
	classification, symptoms := 100, 0
	rate, scale := defaultECGRate, 1e-6
	var values []float64
	var rawValues, headers []string
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" {
			continue
		}
		if v, ok := ecgNumber(line); ok {
			values = append(values, v)
			if c.opts.Archive {
				rawValues = append(rawValues, line)
			}
			continue
		}
		if len(values) > 0 {
			return errors.New("invalid ECG waveform row; refusing a partial signal")
		}
		headers = append(headers, scanner.Text())
		row, readErr := csv.NewReader(strings.NewReader(line)).Read()
		if readErr != nil || len(row) < 2 {
			continue
		}
		key, value := strings.ToLower(strings.TrimSpace(row[0])), strings.TrimSpace(strings.Join(row[1:], ","))
		switch key {
		case "recorded date", "дата записи":
			recorded, err = parseExportTime(value)
			if err != nil {
				return errors.New("invalid ECG recording date")
			}
		case "classification", "классификация":
			if v, ok := ecgClassifications[strings.ToLower(value)]; ok {
				classification = v
			}
		case "symptoms", "симптомы":
			switch strings.ToLower(value) {
			case "":
				symptoms = 0
			case "none", "нет", "нет симптомов":
				symptoms = 1
			default:
				symptoms = 2
			}
		case "sample rate", "частота замеров", "частота дискретизации":
			number := strings.Fields(value)
			if len(number) == 0 {
				return errors.New("missing ECG sample rate")
			}
			var ok bool
			rate, ok = ecgNumber(number[0])
			if !ok || rate <= 0 {
				return errors.New("invalid ECG sample rate")
			}
		case "unit", "units", "единицы":
			switch strings.ToLower(value) {
			case "µv", "μv", "uv", "мкв", "microvolts", "микровольты":
				scale = 1e-6
			case "mv", "мв", "millivolts", "милливольты":
				scale = 1e-3
			case "v", "в", "volts", "вольты":
				scale = 1
			default:
				return errors.New("unsupported ECG voltage unit")
			}
		default:
			if recorded.IsZero() {
				if t, e := parseExportTime(value); e == nil {
					recorded = t
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if recorded.IsZero() || len(values) == 0 {
		return errors.New("ECG date or waveform missing; refusing a partial recording")
	}
	end := recorded.Add(time.Duration(float64(len(values)) / rate * float64(time.Second)))
	s, ok := c.newSample(TypeECG, commonAttrs{StartDate: recorded.Format(exportLayout), EndDate: end.Format(exportLayout)}, nil)
	if !ok {
		return errors.New("ECG recording has invalid dates")
	}
	// Export timestamps have whole seconds, but waveform duration need not.
	s.fields["endDate"] = wireTime(end)
	s.fields["classification"], s.fields["symptomsStatus"] = classification, symptoms
	s.fields["samplingFrequency"], s.fields["numberOfVoltageMeasurements"] = rate, len(values)
	fileID := hex.EncodeToString(digest.Sum(nil))
	if c.opts.Archive {
		s.fields["_source"] = map[string]any{"format": "ecg-csv", "sha256": fileID, "headers": headers}
	}
	parentID := sourceID(TypeECG, []any{s.fields["_source"], nil})
	p := c.newSolo(TypeECG, s.day)
	if err := p.add(s.fields); err != nil {
		return err
	}
	for i, value := range values {
		fields := map[string]any{"timeSinceSampleStart": float64(i) / rate, "value": value * scale}
		if c.opts.Archive {
			fields["parentSourceId"] = parentID
			fields["sourceFileSHA256"] = fileID
			fields["sampleIndex"] = i
			fields["rawValue"] = rawValues[i]
		}
		if err := p.add(fields); err != nil {
			return err
		}
	}
	return p.flush()
}
