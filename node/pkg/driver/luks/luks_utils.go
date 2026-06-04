/**
 * Copyright 2019 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package luks

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ibm/ibm-block-csi-driver/node/logger"
)

const (
	cryptsetupCmd = "cryptsetup"
	mapperPrefix  = "ibm_"

	// Timeouts for LUKS operations
	formatTimeout = 60000  // 60 seconds
	openTimeout   = 30000  // 30 seconds
	closeTimeout  = 30000  // 30 seconds
	resizeTimeout = 30000  // 30 seconds
	statusTimeout = 10000  // 10 seconds
)

// LuksInterface defines operations for LUKS encryption management
type LuksInterface interface {
	// LuksFormat formats a device with LUKS encryption
	LuksFormat(devicePath string, passphrase string) error

	// LuksOpen opens an encrypted device and returns the mapper path
	LuksOpen(devicePath string, volumeID string, passphrase string) (string, error)

	// LuksClose closes an encrypted device
	LuksClose(volumeID string) error

	// LuksResize resizes an encrypted device
	LuksResize(volumeID string) error

	// LuksStatus checks if a LUKS mapper is active
	LuksStatus(volumeID string) (bool, error)

	// IsLuks checks if a device is LUKS formatted
	IsLuks(devicePath string) (bool, error)
}

// LuksManager implements LuksInterface
type LuksManager struct{}

// NewLuksManager creates a new LuksManager instance
func NewLuksManager() *LuksManager {
	return &LuksManager{}
}

// sanitizeVolumeID replaces delimiter characters with underscores for mapper naming
func sanitizeVolumeID(volumeID string) string {
	// Replace : delimiter with _ for device mapper naming
	return strings.ReplaceAll(volumeID, ":", "_")
}

// getMapperName returns the mapper device name for a volume
func getMapperName(volumeID string) string {
	return mapperPrefix + sanitizeVolumeID(volumeID)
}

// getMapperPath returns the full mapper device path for a volume
func getMapperPath(volumeID string) string {
	return "/dev/mapper/" + getMapperName(volumeID)
}

// executeCommand runs a command with timeout and returns output
func executeCommand(timeoutMs int, command string, args []string, stdin []byte) ([]byte, error) {
	cmd := exec.Command(command, args...)

	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %v", err)
	}

	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(time.Duration(timeoutMs) * time.Millisecond):
		if err := cmd.Process.Kill(); err != nil {
			logger.Warningf("Failed to kill timed out process: %v", err)
		}
		return nil, fmt.Errorf("command timed out after %dms", timeoutMs)
	case err := <-done:
		if err != nil {
			return nil, fmt.Errorf("command failed: %v, stderr: %s", err, stderr.String())
		}
		return stdout.Bytes(), nil
	}
}

// LuksFormat formats a device with LUKS encryption
func (m *LuksManager) LuksFormat(devicePath string, passphrase string) error {
	logger.Infof("Formatting device %s with LUKS encryption", devicePath)

	args := []string{
		"luksFormat",
		"--type", "luks2",
		"--batch-mode",
		devicePath,
	}

	// Pass passphrase via stdin for security
	_, err := executeCommand(formatTimeout, cryptsetupCmd, args, []byte(passphrase))
	if err != nil {
		return &LuksFormatError{DevicePath: devicePath, Err: err}
	}

	logger.Infof("Successfully formatted device %s with LUKS", devicePath)
	return nil
}

// LuksOpen opens an encrypted device and returns the mapper path
func (m *LuksManager) LuksOpen(devicePath string, volumeID string, passphrase string) (string, error) {
	mapperName := getMapperName(volumeID)
	mapperPath := getMapperPath(volumeID)

	logger.Infof("Opening LUKS device %s as %s", devicePath, mapperName)

	// Check if already opened (idempotency)
	active, err := m.LuksStatus(volumeID)
	if err == nil && active {
		logger.Infof("LUKS mapper %s already active, reusing", mapperName)
		return mapperPath, nil
	}

	args := []string{
		"luksOpen",
		devicePath,
		mapperName,
	}

	// Pass passphrase via stdin for security
	_, err = executeCommand(openTimeout, cryptsetupCmd, args, []byte(passphrase))
	if err != nil {
		return "", &LuksOpenError{DevicePath: devicePath, Err: err}
	}

	logger.Infof("Successfully opened LUKS device %s at %s", devicePath, mapperPath)
	return mapperPath, nil
}

// LuksClose closes an encrypted device
func (m *LuksManager) LuksClose(volumeID string) error {
	mapperName := getMapperName(volumeID)

	logger.Infof("Closing LUKS mapper %s", mapperName)

	// Check if active before attempting to close (idempotency)
	active, err := m.LuksStatus(volumeID)
	if err == nil && !active {
		logger.Infof("LUKS mapper %s not active, nothing to close", mapperName)
		return nil
	}

	args := []string{
		"luksClose",
		mapperName,
	}

	_, err = executeCommand(closeTimeout, cryptsetupCmd, args, nil)
	if err != nil {
		// Check for "not active" error and treat as success (idempotency)
		if strings.Contains(err.Error(), "not active") {
			logger.Infof("LUKS mapper %s already closed", mapperName)
			return nil
		}
		return &LuksCloseError{MapperName: mapperName, Err: err}
	}

	logger.Infof("Successfully closed LUKS mapper %s", mapperName)
	return nil
}

// LuksResize resizes an encrypted device
func (m *LuksManager) LuksResize(volumeID string) error {
	mapperName := getMapperName(volumeID)

	logger.Infof("Resizing LUKS mapper %s", mapperName)

	args := []string{
		"resize",
		mapperName,
	}

	_, err := executeCommand(resizeTimeout, cryptsetupCmd, args, nil)
	if err != nil {
		return &LuksResizeError{MapperName: mapperName, Err: err}
	}

	logger.Infof("Successfully resized LUKS mapper %s", mapperName)
	return nil
}

// LuksStatus checks if a LUKS mapper is active
func (m *LuksManager) LuksStatus(volumeID string) (bool, error) {
	mapperName := getMapperName(volumeID)

	args := []string{
		"status",
		mapperName,
	}

	output, err := executeCommand(statusTimeout, cryptsetupCmd, args, nil)
	if err != nil {
		// If the device is not active, cryptsetup status returns non-zero
		if strings.Contains(err.Error(), "inactive") || strings.Contains(string(output), "inactive") {
			return false, nil
		}
		// Other errors are actual failures
		return false, err
	}

	// If command succeeded, check output for "active" status
	outputStr := string(output)
	return strings.Contains(outputStr, "is active"), nil
}

// IsLuks checks if a device is LUKS formatted
func (m *LuksManager) IsLuks(devicePath string) (bool, error) {
	args := []string{
		"isLuks",
		devicePath,
	}

	_, err := executeCommand(statusTimeout, cryptsetupCmd, args, nil)
	if err != nil {
		// isLuks returns non-zero exit code if device is not LUKS formatted
		// This is expected behavior, not an error
		return false, nil
	}

	return true, nil
}

// IsDeviceEmpty checks if a device appears to be empty (no filesystem or partition table)
// Returns true if device is safe to format with LUKS
func IsDeviceEmpty(devicePath string) (bool, error) {
	// Use blkid to detect any existing filesystem or partition table
	cmd := exec.Command("blkid", "-p", "-o", "value", "-s", "TYPE", devicePath)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Exit code 2 means no signature found (device is empty) - this is what we want
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			logger.Debugf("Device %s has no filesystem signature (empty)", devicePath)
			return true, nil
		}
		// Other errors are actual failures
		return false, fmt.Errorf("failed to check device signature: %v, output: %s", err, string(output))
	}

	// If blkid succeeded, it found a filesystem/signature
	fsType := strings.TrimSpace(string(output))
	if fsType != "" {
		logger.Warningf("Device %s contains existing data (type: %s)", devicePath, fsType)
		return false, nil
	}

	return true, nil
}
