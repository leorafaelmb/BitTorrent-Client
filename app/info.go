package main

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Info represents the 'info' dictionary from a torrent file.
// This contains all metadata about the file(s) being shared.
type Info struct {
	Length      int
	Name        string
	PieceLength int
	Pieces      []byte
	InfoHash    [20]byte
	Files       []FileInfo
}

type FileInfo struct {
	Length int
	Path   []string
}

// newInfo constructs an Info struct from the 'info' dictionary
func newInfo(infoMap map[string]interface{}) (*Info, error) {
	name, ok := infoMap["name"].(string)
	if !ok {
		return nil, fmt.Errorf("error accessing info name: not a string")
	}
	pieceLength, ok := infoMap["piece length"].(int)
	if !ok {
		return nil, fmt.Errorf("error accessing info piece length: not an int")
	}
	pieces, ok := infoMap["pieces"].([]byte)
	if !ok {
		return nil, fmt.Errorf("error accessing info pieces: not a byte slice")
	}

	info := &Info{
		Name:        name,
		PieceLength: pieceLength,
		Pieces:      pieces,
	}
	if length, ok := infoMap["length"].(int); ok {
		info.Length = length
	} else if filesInterface, ok := infoMap["files"].([]interface{}); ok {
		files, err := parseFiles(filesInterface)
		if err != nil {
			return nil, err
		}
		info.Files = files

		info.Length = 0
		for _, f := range files {
			info.Length += f.Length
		}
	} else {
		return nil, fmt.Errorf("error creating info")
	}
	if !ok {
		return nil, fmt.Errorf("error accessing info length: not an int")
	}

	return info, nil
}

func parseFiles(filesInterface []interface{}) ([]FileInfo, error) {
	var files []FileInfo

	for i, fileInterface := range filesInterface {
		fileMap, ok := fileInterface.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("file %d is not a map", i)
		}

		length, ok := fileMap["length"].(int)
		if !ok {
			return nil, fmt.Errorf("file %d length is not an int", i)
		}

		pathInterface, ok := fileMap["path"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("file %d path is not a list", i)
		}

		// Convert path components to strings
		var path []string
		for j, component := range pathInterface {
			pathStr, ok := component.(string)
			if !ok {
				return nil, fmt.Errorf("file %d path component %d is not a string", i, j)
			}
			path = append(path, pathStr)
		}

		files = append(files, FileInfo{
			Length: length,
			Path:   path,
		})
	}

	return files, nil
}

func (i Info) IsSingleFile() bool {
	return len(i.Files) == 0
}

func (i Info) GetFiles() []FileInfo {
	if i.IsSingleFile() {
		// Single-file mode: return the file with the torrent name
		return []FileInfo{
			{
				Length: i.Length,
				Path:   []string{i.Name},
			},
		}
	}
	// Multi-file mode: return the files list
	return i.Files
}

// getInfoHash returns the SHA1 hash of the bencoded info dictionary
func (i Info) getInfoHash() [20]byte {
	infoHash := [20]byte{}
	hasher := sha1.New()
	bencodedBytes := i.serializeInfo()
	hasher.Write(bencodedBytes)

	sha := hasher.Sum(nil)
	copy(infoHash[:], sha)
	return infoHash
}

// getHexInfoHash returns the info hash in hexadecimal representation
func (i Info) getHexInfoHash() string {
	return fmt.Sprintf("%x", i.getInfoHash())
}

// serializeInfo bencodes the Info struct
func (i Info) serializeInfo() []byte {
	var infoB []byte = []byte{'d'}

	if i.IsSingleFile() {
		// Single-file mode
		lengthB := []byte(fmt.Sprintf("6:lengthi%de", i.Length))
		nameB := []byte(fmt.Sprintf("4:name%d:%s", len(i.Name), i.Name))
		pLB := []byte(fmt.Sprintf("12:piece lengthi%de", i.PieceLength))
		pieces := []byte(fmt.Sprintf("6:pieces%d:", len(i.Pieces)))
		pieces = append(pieces, i.Pieces...)

		infoB = append(infoB, lengthB...)
		infoB = append(infoB, nameB...)
		infoB = append(infoB, pLB...)
		infoB = append(infoB, pieces...)
	} else {
		// Multi-file mode
		filesB := []byte("5:filesl")
		for _, f := range i.Files {
			fileB := []byte(fmt.Sprintf("d6:lengthi%de4:pathl", f.Length))
			for _, pathComponent := range f.Path {
				fileB = append(fileB, []byte(fmt.Sprintf("%d:%s", len(pathComponent), pathComponent))...)
			}
			fileB = append(fileB, 'e', 'e') // Close path list and file dict
			filesB = append(filesB, fileB...)
		}
		filesB = append(filesB, 'e') // Close files list

		nameB := []byte(fmt.Sprintf("4:name%d:%s", len(i.Name), i.Name))
		pLB := []byte(fmt.Sprintf("12:piece lengthi%de", i.PieceLength))
		pieces := []byte(fmt.Sprintf("6:pieces%d:", len(i.Pieces)))
		pieces = append(pieces, i.Pieces...)

		infoB = append(infoB, filesB...)
		infoB = append(infoB, nameB...)
		infoB = append(infoB, pLB...)
		infoB = append(infoB, pieces...)
	}

	infoB = append(infoB, 'e')
	return infoB
}

// SaveFile saves downloaded data to appropriate file(s)
func (t *TorrentFile) SaveFile(downloadPath string, data []byte) error {
	files := t.Info.GetFiles()

	if t.Info.IsSingleFile() {
		// Single file: just write it
		return os.WriteFile(downloadPath, data, 0644)
	}
	// Multi-file: create directory structure and split data
	baseDir := filepath.Join(filepath.Dir(downloadPath), t.Info.Name)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("error creating base directory: %w", err)
	}

	offset := 0
	for _, fileInfo := range files {
		// Construct file path
		pathComponents := append([]string{baseDir}, fileInfo.Path...)
		filePath := filepath.Join(pathComponents...)

		// Create parent directories
		parentDir := filepath.Dir(filePath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("error creating directory %s: %w", parentDir, err)
		}

		// Extract file data
		fileData := data[offset : offset+fileInfo.Length]

		// Write file
		if err := os.WriteFile(filePath, fileData, 0644); err != nil {
			return fmt.Errorf("error writing file %s: %w", filePath, err)
		}

		fmt.Printf("Wrote file: %s (%d bytes)\n", filePath, fileInfo.Length)
		offset += fileInfo.Length
	}

	return nil
}

// HexPieceHashes formats piece hashes for display in hexadecimal format
func (i Info) HexPieceHashes() []string {
	var pieceHashes []string
	pieces := i.Pieces
	for j := 0; j < len(pieces); j += 20 {
		piece := pieces[j : j+20]
		pieceHashes = append(pieceHashes, fmt.Sprintf("%x", piece))
	}

	return pieceHashes
}

// pieceHashes returns piece hashes as [][]byte
func (i Info) pieceHashes() [][]byte {
	pieces := i.Pieces
	var piecesSlice [][]byte
	for j := 0; j < len(pieces); j += 20 {
		piecesSlice = append(piecesSlice, pieces[j:j+20])
	}
	return piecesSlice
}

// getPieceHashesStr formats piece hashes for display
func (i Info) GetPieceHashesStr() string {
	pieceHashes := i.HexPieceHashes()
	pieceHashesStr := ""
	for _, h := range pieceHashes {
		pieceHashesStr += fmt.Sprintf("%s\n", h)
	}
	return strings.TrimSpace(pieceHashesStr)
}
