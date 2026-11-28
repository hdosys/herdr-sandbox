package sandbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
)

// configurationEdit binds a reviewed edit to the exact bytes read from the one
// host configuration owner. It is not persistent state or a second config model.
type configurationEdit struct {
	root          string
	original      []byte
	configuration globalConfiguration
	fields        map[string]json.RawMessage
}

func readConfigurationEdit(root string) (configurationEdit, error) {
	edit := configurationEdit{root: root}
	data, found, err := readBoundedRegularFile(filepath.Join(root, globalConfigurationName), 1024*1024)
	if err != nil {
		return edit, err
	}
	edit.original = data
	if !found {
		data = defaultGlobalConfiguration
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decodeGlobalConfiguration(decoder, &edit.configuration); err != nil {
		return edit, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return edit, errors.New("configuration contains trailing data")
	}
	if err := json.Unmarshal(data, &edit.fields); err != nil {
		return edit, err
	}
	return edit, nil
}

func (edit configurationEdit) save() error {
	data, err := json.MarshalIndent(edit.fields, "", "  ")
	if err != nil {
		return err
	}
	if _, err := ensurePhysicalDirectory(edit.root, "host configuration"); err != nil {
		return err
	}
	if err := writeConfigurationReferences(edit.root); err != nil {
		return err
	}
	return writeFileAtomicallyIfUnchanged(filepath.Join(edit.root, globalConfigurationName), edit.original, append(data, '\n'), 0o600)
}
