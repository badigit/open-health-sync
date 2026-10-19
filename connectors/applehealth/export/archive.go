// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
)

// originalXML preserves lexical values, unknown attributes and nested data.
// The source archive remains the authority for original bytes and attachments.
func originalXML(element string, attrs []xml.Attr, inner string) map[string]any {
	attributes := map[string]string{}
	for _, a := range attrs {
		name := a.Name.Local
		if a.Name.Space != "" {
			name = "{" + a.Name.Space + "}" + name
		}
		attributes[name] = a.Value
	}
	return map[string]any{"element": element, "attributes": attributes, "innerXML": inner}
}

func sourceID(typ string, source any) string {
	b, _ := json.Marshal([]any{typ, source})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (c *converter) prepareLine(typ string, fields map[string]any) ([]byte, bool, error) {
	if c.opts.Deduplicate && !c.opts.Archive {
		return nil, false, errors.New("deduplication requires offline archive mode")
	}
	if c.opts.Archive {
		fields["recordType"] = typ
		identity := any(fields)
		if raw, ok := fields["_source"]; ok {
			identity = []any{raw, fields["parentSourceId"]}
		}
		id := sourceID(typ, identity)
		fields["sourceId"] = id
		c.stats.SourceRows[typ]++
		if c.opts.Deduplicate {
			var key [32]byte
			decoded, _ := hex.DecodeString(id)
			copy(key[:], decoded)
			if _, ok := c.seen[key]; ok {
				c.stats.Duplicates[typ]++
				return nil, true, nil
			}
			c.seen[key] = struct{}{}
		}
	}
	b, err := encodeLine(fields)
	return b, false, err
}

func (c *converter) sourceOnly(d *xml.Decoder, start xml.StartElement) error {
	var raw struct {
		Inner string `xml:",innerxml"`
	}
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	fields := map[string]any{"_source": originalXML(start.Name.Local, start.Attr, raw.Inner), "recordKind": "source"}
	day := "undated"
	for _, a := range start.Attr {
		if a.Name.Local == "dateComponents" {
			day = a.Value
		}
	}
	if start.Name.Local == "ActivitySummary" {
		fields["recordKind"] = "daily-summary"
	} else if start.Name.Local != "Me" && start.Name.Local != "ExportDate" {
		c.stats.Skipped[start.Name.Local]++
	}
	return c.add(start.Name.Local, day, fields)
}
