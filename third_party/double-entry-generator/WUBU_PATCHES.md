# Fixed upstream source and Wubu patches

Source: https://github.com/deb-sig/double-entry-generator/tree/v2.16.0

Go module: github.com/deb-sig/double-entry-generator/v2 v2.16.0

Original module checksum: h1:djzBtmiwAsVpSTtk9jb+OabMyDQJxmR+8ZJblMs4RYI=

Original go.mod checksum: h1:/UYuY+dCAqya0jVvB/4Cqhd1zBGdEk7/WGGCKI6TfVQ=

Copyright © 2019 Ce Gao and double-entry-generator contributors.
Licensed under Apache-2.0; LICENSE is preserved. The pkg tree and module
manifests are copied from that exact module, including all bank providers.
Original command-line entry points, website and examples are not required by
this library integration. Public native examples used by Wubu regression tests
are retained separately at internal/statement/testdata with attribution.

Local changes, 2026-11-03:

- pkg/io/reader/file_reader.go: GetReader uses os.ReadFile and bytes.NewReader.
  Its io.Reader API gave callers no Close operation, and upstream left a live
  os.File after every CSV parse. This was reproduced as retained Windows temp
  directories. Closing the handle during loading preserves parsing behavior
  while allowing immediate deletion of Wubu's temporary envelopes.
- pkg/provider/spdb_debit/wubu_record.go exposes the existing record parser and
  IR converter, allowing independent errors per native XLS row. No parsing
  grammar is reimplemented. convert.go retains its native TransactionID in IR
  OrderID so overlapping statements can use the source ID for reconciliation.

Wubu's native input adaptation, source-row retention and reconciliation are in
internal/statement and internal/store/statements.go. No bank provider is removed.
No Beancount compiler, investment or crypto provider is exposed in Wubu's UI/API.
