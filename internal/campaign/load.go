package campaign

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

const maxCampaignBytes = 1 << 20

// LoadFile parses, defaults, and validates one strict campaign document.
func LoadFile(path string) (Campaign, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Campaign{}, fmt.Errorf("read campaign %q: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes a strict YAML campaign. Unknown fields and trailing documents
// are rejected so misspelled safety or contract fields cannot be ignored.
func Parse(data []byte) (Campaign, error) {
	if len(data) > maxCampaignBytes {
		return Campaign{}, fieldError(ErrInvalidCampaign, "$", "document exceeds 1 MiB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var value Campaign
	if err := decoder.Decode(&value); err != nil {
		return Campaign{}, fieldError(ErrInvalidCampaign, "$", err.Error())
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Campaign{}, fieldError(ErrInvalidCampaign, "$", "multiple YAML documents are not allowed")
		}
		return Campaign{}, fieldError(ErrInvalidCampaign, "$", err.Error())
	}

	applyDefaults(&value)
	if err := value.Validate(); err != nil {
		return Campaign{}, err
	}
	value.Normalize()
	return value, nil
}
