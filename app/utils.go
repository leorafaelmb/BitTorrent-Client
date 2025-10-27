package main

import (
	"crypto/sha1"
	"fmt"
)

// hashPiece computes the SHA1 hash of a piece for verification
func hashPiece(piece []byte) []byte {
	hasher := sha1.New()
	hasher.Write(piece)
	sha := hasher.Sum(nil)
	return sha
}

// urlEncodeInfoHash URL-encodes a hexadecimal-represented info hash
func urlEncodeInfoHash(infoHash string) string {
	urlEncodedHash := ""
	for i := 0; i < len(infoHash); i += 2 {
		urlEncodedHash += fmt.Sprintf("%%%s%s", string(infoHash[i]), string(infoHash[i+1]))
	}
	return urlEncodedHash
}
