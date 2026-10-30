package localfile

import "golang.org/x/sys/windows"

func room(dir string) (int64, error) {
	at, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(at, &available, &total, &free); err != nil {
		return 0, err
	}
	return int64(available), nil //nolint:gosec
}
