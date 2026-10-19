# Offline Apple Health adapter

`apple-health-export` converts a Health app ZIP to a private local derivative.
It does not connect to a server, create a user, or upload medical records.

```sh
go build -o bin/apple-health-export ./cmd/apple-health-export
bin/apple-health-export -output /private/new-output export.zip
```

The output directory must not exist. A successful run creates
`records.ndjson.gz` and `manifest.json`. Failed runs leave an incomplete
directory without a complete manifest; never import that directory.
Retain the original ZIP alongside the derivative. The manifest hashes every
original file, including the alternate CDA document, without importing that
document a second time as measurements.

The ZIP entry is selected by its `HealthData` root, independently of the
phone language. Conversion handles numeric quantities outside the server's
current catalog, category samples, sleep goals, daily activity summaries,
correlations, workouts, GPX points, ECG waveforms, and nested instantaneous
BPM metadata associated with SDNN. An offline quantity does not imply support
in the separate Open Health Sync server storage registry.

Each source object retains its XML attributes and inner XML in `_source`.
This preserves lexical values, creation dates, device strings, metadata,
and currently unknown nested fields. Numeric values are additionally exposed
in the existing converter format. Sleep goals have `recordKind: goal`;
activity summaries have `recordKind: daily-summary`. Neither is treated as
an additional measured sample. Profile and export-header objects are retained.

Russian ECG exports accept decimal commas, localized field names, voltage
units, sample frequencies, and classification labels. Original headers and
lexical voltage values remain available. A malformed waveform fails conversion
instead of publishing a truncated recording. SDNN's `instantaneousBPM` values
remain BPM readings with their original time strings; they are not invented
R-R intervals.

`sourceId` is a SHA-256 identity of the complete source representation.
Only exact source duplicates are removed, with occurrence counts in
`stats.Duplicates`. Different devices, creation dates, metadata or nested
data remain distinct. This is intentionally stricter than merging similar
measurements: selecting preferred sources across phones and watches belongs
to a reviewed downstream mapping. Derived ECG and GPX rows carry
`parentSourceId`; ECG rows also carry the source file hash and sample index.
Correlations retain their member records and identities, not an independent
second copy of each measurement.

The derivative is a versioned offline adapter format, not the server upload
contract. Unknown non-quantity records are retained but prevent a complete
manifest. No automatic clinical write or aggregation is performed. Future
importers must check the manifest, map units and identities, and handle source
selection before applying any change.

Memory grows with the exact-identity set in addition to bounded conversion
buffers. Use the command's `-timeout` flag to set the conversion deadline.
Private archives, derivatives, manifests and audit reports must stay outside
this public repository. All committed fixtures use invented data.
