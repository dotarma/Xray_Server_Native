package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const currentStateSchemaVersion = 1

type stateSchema struct {
	Version    int    `json:"version"`
	MigratedAt string `json:"migratedAt"`
}

func (m *manager) migrateState() error {
	path := filepath.Join(m.stateDir, "state-schema.json")
	schema := stateSchema{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &schema); err != nil {
			return fmt.Errorf("read schema: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if schema.Version < 0 || schema.Version > currentStateSchemaVersion {
		return fmt.Errorf("unsupported state schema version %d", schema.Version)
	}
	for version := schema.Version + 1; version <= currentStateSchemaVersion; version++ {
		switch version {
		case 1:
			if err := m.migrateStateV1(); err != nil {
				return err
			}
		}
		schema.Version = version
		schema.MigratedAt = time.Now().UTC().Format(time.RFC3339)
		if err := writeJSONFile(path, schema); err != nil {
			return err
		}
	}
	return nil
}

// migrateStateV1 establishes schema tracking and corrects permissions on
// pre-schema secret files. Each write is atomic, so an interrupted upgrade
// leaves the previous valid state available for the next boot.
func (m *manager) migrateStateV1() error {
	for _, name := range []string{
		"deployment.json", "deployments.json", "mode2-preferences.json",
		"mode3-preferences.json", "panel-credentials.txt", "panel-tunnel-token.txt",
		"quick-xray.json", "service.env", "token.txt",
	} {
		path := filepath.Join(m.stateDir, name)
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("refusing non-regular state file %s", name)
			}
			if err := os.Chmod(path, 0o600); err != nil {
				return fmt.Errorf("secure %s: %w", name, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
