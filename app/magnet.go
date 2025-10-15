package main

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

type MagnetLink struct {
	TrackerURL  string
	InfoHash    [20]byte
	HexInfoHash string
}

func DeserializeMagnet(uri string) (*MagnetLink, error) {
	magnetUri, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	fmt.Println(magnetUri.Query())
	trackerURL := magnetUri.Query()["tr"][0]
	hexInfoHash := strings.ReplaceAll(magnetUri.Query()["xt"][0], "urn:btih:", "")

	var infoHash [20]byte
	decodedHash, err := hex.DecodeString(hexInfoHash)
	if err != nil {
		return nil, err
	}
	copy(infoHash[:], decodedHash)

	return &MagnetLink{
		TrackerURL:  trackerURL,
		InfoHash:    infoHash,
		HexInfoHash: hexInfoHash,
	}, nil
}
