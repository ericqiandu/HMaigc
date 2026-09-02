//go:build windows

package opsstate

import "golang.org/x/sys/windows"

func replaceFile(source string, destination string) error {
	sourcePointer, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPointer, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePointer,
		destinationPointer,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

// Windows does not expose the Unix directory fsync primitive. MoveFileEx with
// MOVEFILE_WRITE_THROUGH is the explicit durability boundary for replacements.
func syncDirectory(string) error {
	return nil
}
