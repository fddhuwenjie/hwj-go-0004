package flags

import "errors"

var (
	ErrInvalidInput      = errors.New("invalid input")
	ErrFlagNotFound      = errors.New("flag not found")
	ErrDuplicateFlag     = errors.New("duplicate flag")
	ErrRequestConflict   = errors.New("request id already used for different operation")
	ErrPrerequisiteCycle = errors.New("prerequisite cycle")
)
