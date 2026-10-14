//go:build windows

package scanner

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"

	"golang.org/x/sys/windows"
)

// driveSeekPenalty probes the volume holding the resolved scan root, including
// volumes mounted in folders. Remote volumes are left to the existing default.
func driveSeekPenalty(ctx context.Context, root string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	path, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false, err
	}
	mount := make([]uint16, 32768)
	if err := windows.GetVolumePathName(path, &mount[0], uint32(len(mount))); err != nil {
		return false, err
	}
	kind := windows.GetDriveType(&mount[0])
	if kind != windows.DRIVE_FIXED && kind != windows.DRIVE_REMOVABLE {
		return false, errors.New("drive type is unknown or remote")
	}
	var volume [50]uint16
	if err := windows.GetVolumeNameForVolumeMountPoint(&mount[0], &volume[0], uint32(len(volume))); err != nil {
		return false, err
	}
	device, err := windows.UTF16PtrFromString(strings.TrimSuffix(windows.UTF16ToString(volume[:]), `\`))
	if err != nil {
		return false, err
	}
	handle, err := windows.CreateFile(device, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return false, err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// These byte buffers match STORAGE_PROPERTY_QUERY and DEVICE_SEEK_PENALTY_DESCRIPTOR.
	// A zero query type requests the standard descriptor; the property id is seven.
	var query, descriptor [12]byte
	binary.LittleEndian.PutUint32(query[:4], 7)
	var returned uint32
	const storageQueryProperty = 0x2d1400
	if err := windows.DeviceIoControl(handle, storageQueryProperty, &query[0], uint32(len(query)), &descriptor[0], uint32(len(descriptor)), &returned, nil); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return seekPenaltyDescriptor(descriptor[:], returned)
}

func seekPenaltyDescriptor(raw []byte, returned uint32) (bool, error) {
	if returned < 12 || len(raw) < 12 || binary.LittleEndian.Uint32(raw[:4]) < 12 || binary.LittleEndian.Uint32(raw[4:8]) < 12 || raw[8] > 1 {
		return false, errors.New("drive returned an invalid seek penalty descriptor")
	}
	return raw[8] != 0, nil
}
