package app

import (
	"encoding/json"
	"fmt"
)

// uniqueJSONKeys rejects duplicate keys and unknown nesting in a JSON value.
func uniqueJSONKeys(dec *json.Decoder, depth int) error {
	if depth > 32 {
		return errWorkspaceNesting
	}

	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		if err = uniqueJSONObject(dec, depth); err != nil {
			return err
		}
	case '[':
		for dec.More() {
			if err = uniqueJSONKeys(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return errWorkspaceDelimiter
	}
	_, err = dec.Token()

	return err
}

// uniqueJSONObject walks one JSON object and rejects a repeated key.
func uniqueJSONObject(dec *json.Decoder, depth int) error {
	seen := make(map[string]bool)

	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		name, isString := key.(string)
		if !isString || seen[name] {
			return fmt.Errorf("%w: %q", errWorkspaceJSONKey, key)
		}
		seen[name] = true

		if err = uniqueJSONKeys(dec, depth+1); err != nil {
			return err
		}
	}

	return nil
}
