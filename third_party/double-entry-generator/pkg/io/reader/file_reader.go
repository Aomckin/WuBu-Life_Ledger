//go:build !js
// +build !js

package reader

import (
	"bytes"
	"io"
	"os"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func GetReader(filename string) (io.Reader, error) {
	// Wubu patch (2026-11-03): this API returns io.Reader, so callers cannot
	// close its file. ReadFile closes the handle before returning the reader.
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(content), nil
}

func GetGBKReader(filename string) (io.Reader, error) {
	csvFile, err := GetReader(filename)
	if err != nil {
		return nil, err
	}

	gbkReader := transform.NewReader(csvFile, simplifiedchinese.GBK.NewDecoder())
	return gbkReader, nil
}
