package opsconfig

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

type Environment struct {
	Values map[string]string
}

func ReadFile(path string) (Environment, error) {
	file, err := os.Open(path)
	if err != nil {
		return Environment{}, err
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !validKey(key) {
			return Environment{}, fmt.Errorf("%s:%d contains an invalid environment assignment", path, lineNumber)
		}
		if _, exists := values[key]; exists {
			return Environment{}, fmt.Errorf("%s:%d duplicates %s", path, lineNumber, key)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return Environment{}, err
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			return Environment{}, err
		}
		if info.Mode().Perm()&0o077 != 0 {
			return Environment{}, fmt.Errorf("%s must not be accessible by group or other users", path)
		}
	}
	return Environment{Values: values}, nil
}

func ValidateFiles(productionPath, controlPath string) error {
	production, err := ReadFile(productionPath)
	if err != nil {
		return fmt.Errorf("read canonical production configuration: %w", err)
	}
	control, err := ReadFile(controlPath)
	if err != nil {
		return fmt.Errorf("read canonical control configuration: %w", err)
	}
	if err := ValidateProduction(production); err != nil {
		return err
	}
	if err := ValidateControl(control); err != nil {
		return err
	}
	if production.Values["HMAIGC_OPS_STATE_VOLUME"] != control.Values["HMAIGC_OPS_STATE_VOLUME"] {
		return errors.New("business and controller configurations name different ops-state volumes")
	}
	return nil
}

func ValidateProduction(environment Environment) error {
	if err := requireProduction(environment); err != nil {
		return err
	}
	if !immutableImage(environment.Values["HMAIGC_BACKEND_IMAGE"]) {
		return errors.New("HMAIGC_BACKEND_IMAGE must be an immutable SHA-256 image reference")
	}
	if !immutableImage(environment.Values["HMAIGC_WEB_IMAGE"]) {
		return errors.New("HMAIGC_WEB_IMAGE must be an immutable SHA-256 image reference")
	}
	if !immutableImage(environment.Values["BACKUP_HELPER_IMAGE"]) {
		return errors.New("BACKUP_HELPER_IMAGE must be an immutable SHA-256 image reference")
	}
	return nil
}

func ValidateControl(environment Environment) error {
	required := []string{
		"HMAIGC_OPS_IMAGE", "HMAIGC_OPS_VERSION", "HMAIGC_OPS_PROTOCOL_VERSION",
		"HMAIGC_OPS_STATE_VOLUME", "CANVAS_ENVIRONMENT",
	}
	for _, key := range required {
		if strings.TrimSpace(environment.Values[key]) == "" {
			return fmt.Errorf("canonical control configuration is missing %s", key)
		}
	}
	if !immutableImage(environment.Values["HMAIGC_OPS_IMAGE"]) {
		return errors.New("HMAIGC_OPS_IMAGE must be an immutable SHA-256 image reference")
	}
	if environment.Values["CANVAS_ENVIRONMENT"] != "production" {
		return errors.New("CANVAS_ENVIRONMENT must be production")
	}
	if environment.Values["HMAIGC_OPS_PROTOCOL_VERSION"] != "1" {
		return errors.New("unsupported HMAIGC_OPS_PROTOCOL_VERSION")
	}
	return nil
}

func requireProduction(environment Environment) error {
	required := []string{
		"HMAIGC_VERSION", "HMAIGC_BACKEND_IMAGE", "HMAIGC_WEB_IMAGE",
		"BACKUP_HELPER_IMAGE", "HMAIGC_OPS_STATE_VOLUME", "CANVAS_ENVIRONMENT",
	}
	for _, key := range required {
		if strings.TrimSpace(environment.Values[key]) == "" {
			return fmt.Errorf("canonical production configuration is missing %s", key)
		}
	}
	if environment.Values["CANVAS_ENVIRONMENT"] != "production" {
		return errors.New("CANVAS_ENVIRONMENT must be production")
	}
	return nil
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for index, character := range key {
		if character >= 'A' && character <= 'Z' || character == '_' ||
			index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}

func immutableImage(reference string) bool {
	separator := strings.LastIndex(reference, "@sha256:")
	if separator <= 0 {
		return false
	}
	digest := reference[separator+len("@sha256:"):]
	if len(digest) != 64 || digest != strings.ToLower(digest) {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}
