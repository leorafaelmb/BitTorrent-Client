package main

import (
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// Decode decodes bencoded data into Go types
func Decode(bencoded []byte) (interface{}, error) {
	result, _, err := decodeBencode(bencoded, 0)
	return result, err
}

// decode is the internal recursive decoder that processes bencoded data
// Returns string, int, []interace{}, map[string]interface{}, or []byte depending on input
func decodeBencode(bencoded []byte, index int) (interface{}, int, error) {
	identifier := rune(bencoded[index])
	if unicode.IsDigit(identifier) {
		decodedString, i, err := decodeString(bencoded, index)
		if utf8.Valid(decodedString) {
			return string(decodedString), i, err
		} else {
			return decodedString, i, err
		}

	} else if identifier == 'i' {
		return decodeInt(bencoded, index)

	} else if identifier == 'l' {
		return decodeList(bencoded, index)

	} else if identifier == 'd' {
		return decodeDict(bencoded, index)

	} else {
		return "", -1, fmt.Errorf("invalid identifier: %s", string(identifier))
	}
}

// decodeString decodes a bencoded string of format: <length>:<contents>
// Returns the decoded bytes (not converted to string), next index, and any error.
func decodeString(bencoded []byte, index int) ([]byte, int, error) {
	var firstColonIndex int

	for i := index; i < len(bencoded); i++ {
		if bencoded[i] == ':' {
			firstColonIndex = i
			break
		}
	}
	lengthStr := bencoded[index:firstColonIndex]

	length, err := strconv.Atoi(string(lengthStr))
	if err != nil {
		return make([]byte, 0), -1, fmt.Errorf("error converting lengthStr (%s) to an int: %v", lengthStr, err)
	}
	endIndex := firstColonIndex + 1 + length

	decodedString := bencoded[firstColonIndex+1 : endIndex]

	return decodedString, endIndex, nil
}

// decodeInt decodes a bencoded integer of format: i<number>e
// Example: "i42e" returns 42
func decodeInt(bencoded []byte, index int) (int, int, error) {
	i := index
	for ; bencoded[i] != 'e'; i++ {
	}

	decodedInt, err := strconv.Atoi(string(bencoded[index+1 : i]))
	if err != nil {
		return 0, -1, fmt.Errorf("error converting %s to an int: %v", bencoded[index+1:i], err)
	}

	i++

	return decodedInt, i, nil
}

// decodeList decodes a bencoded list of format: l<item1><item2>...e
// Returns a slice of decoded items (mixed types possible)
func decodeList(bencoded []byte, index int) ([]interface{}, int, error) {
	decodedList := make([]interface{}, 0)
	i := index + 1
	for {
		var val interface{}
		var err error

		if bencoded[i] == 'e' {
			i++
			break
		}

		val, i, err = decodeBencode(bencoded, i)
		if err != nil {
			return nil, -1, fmt.Errorf("error decoding bencoded value: %v", err)
		}
		decodedList = append(decodedList, val)

	}

	return decodedList, i, nil

}

// decodeDict decodes a bencoded dictionary of format: d<key1><val1><key2><val2>...e
// Keys must be strings and are sorted in lexicographical order.
// Returns a map with string keys and mixed-type values.
func decodeDict(bencoded []byte, index int) (map[string]interface{}, int, error) {
	decodedDict := make(map[string]interface{})
	i := index + 1
	for {
		var (
			key []byte
			val interface{}
			err error
		)
		identifier := bencoded[i]

		if identifier == 'e' {
			i++
			break
		}

		key, i, err = decodeString(bencoded, i)
		if err != nil {
			return nil, i, fmt.Errorf("error decoding dict key value: %w", err)
		}

		val, i, err = decodeBencode(bencoded, i)
		if err != nil {
			return nil, i, err
		}

		decodedDict[string(key)] = val

	}
	return decodedDict, i, nil
}
