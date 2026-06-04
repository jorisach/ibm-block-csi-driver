package luks

import "fmt"

// LuksFormatError represents an error during LUKS format operation
type LuksFormatError struct {
	DevicePath string
	Err        error
}

func (e *LuksFormatError) Error() string {
	return fmt.Sprintf("failed to format device %s with LUKS: %v", e.DevicePath, e.Err)
}

func (e *LuksFormatError) Unwrap() error {
	return e.Err
}

// LuksOpenError represents an error during LUKS open operation
type LuksOpenError struct {
	DevicePath string
	Err        error
}

func (e *LuksOpenError) Error() string {
	return fmt.Sprintf("failed to open LUKS device %s: %v", e.DevicePath, e.Err)
}

func (e *LuksOpenError) Unwrap() error {
	return e.Err
}

// LuksCloseError represents an error during LUKS close operation
type LuksCloseError struct {
	MapperName string
	Err        error
}

func (e *LuksCloseError) Error() string {
	return fmt.Sprintf("failed to close LUKS mapper %s: %v", e.MapperName, e.Err)
}

func (e *LuksCloseError) Unwrap() error {
	return e.Err
}

// LuksResizeError represents an error during LUKS resize operation
type LuksResizeError struct {
	MapperName string
	Err        error
}

func (e *LuksResizeError) Error() string {
	return fmt.Sprintf("failed to resize LUKS mapper %s: %v", e.MapperName, e.Err)
}

func (e *LuksResizeError) Unwrap() error {
	return e.Err
}

// PassphraseRetrievalError represents an error retrieving encryption passphrase
type PassphraseRetrievalError struct {
	SecretName      string
	SecretNamespace string
	Err             error
}

func (e *PassphraseRetrievalError) Error() string {
	// Never include the passphrase in the error message
	return fmt.Sprintf("failed to retrieve encryption passphrase from secret %s/%s: %v",
		e.SecretNamespace, e.SecretName, e.Err)
}

func (e *PassphraseRetrievalError) Unwrap() error {
	return e.Err
}

// DeviceNotEmptyError represents an error when trying to encrypt a non-empty device
type DeviceNotEmptyError struct {
	DevicePath     string
	ExistingFormat string
}

func (e *DeviceNotEmptyError) Error() string {
	if e.ExistingFormat != "" {
		return fmt.Sprintf("cannot encrypt device %s: device contains existing data (format: %s). "+
			"LUKS encryption can only be applied to new, empty volumes. "+
			"To encrypt existing data, create a new encrypted volume and migrate the data.",
			e.DevicePath, e.ExistingFormat)
	}
	return fmt.Sprintf("cannot encrypt device %s: device contains existing data. "+
		"LUKS encryption can only be applied to new, empty volumes. "+
		"To encrypt existing data, create a new encrypted volume and migrate the data.",
		e.DevicePath)
}
