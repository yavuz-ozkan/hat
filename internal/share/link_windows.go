package share

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// errPrivilegeNotHeld is ERROR_PRIVILEGE_NOT_HELD, what Windows returns for a
// file link when Developer Mode is off and the process isn't elevated.
const errPrivilegeNotHeld syscall.Errno = 1314

var ErrDeveloperModeOff = errors.New("Windows needs Developer Mode to share files between accounts without admin rights. " +
	"Turn it on in Settings > System > For developers > Developer Mode, then run hat again")

func makeLink(link, target string, isDir bool) error {
	if isDir {
		return makeJunction(link, target)
	}
	err := os.Symlink(target, link)
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) && errors.Is(linkErr.Err, errPrivilegeNotHeld) {
		return ErrDeveloperModeOff
	}
	return err
}

// makeJunction links folders with a junction, which needs no special rights,
// unlike a folder symlink. Go has no junction call, and going through
// `cmd /c mklink` would let cmd act on "&" or "%VAR%" in a path, so this sets
// the junction's reparse data directly. A junction is an empty folder until
// its reparse data is set, so it's built under a temporary name and renamed
// into place: an interruption never leaves a plain folder where the link goes.
func makeJunction(link, target string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	building, err := os.MkdirTemp(filepath.Dir(link), filepath.Base(link)+".hat-*")
	if err != nil {
		return err
	}
	if err := setMountPoint(building, absTarget); err != nil {
		_ = os.Remove(building)
		return fmt.Errorf("couldn't link %s to %s: %w", link, target, err)
	}
	if err := os.Rename(building, link); err != nil {
		_ = os.Remove(building)
		return err
	}
	return nil
}

// setMountPoint writes a REPARSE_DATA_BUFFER for a mount point, the layout
// Windows defines for junctions.
func setMountPoint(dir, target string) error {
	substitute := utf16.Encode([]rune(`\??\` + target))
	display := utf16.Encode([]rune(target))
	pathBytes := (len(substitute) + 1 + len(display) + 1) * 2
	buf := make([]byte, 16+pathBytes)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], windows.IO_REPARSE_TAG_MOUNT_POINT)
	le.PutUint16(buf[4:], uint16(8+pathBytes))
	le.PutUint16(buf[8:], 0)
	le.PutUint16(buf[10:], uint16(len(substitute)*2))
	le.PutUint16(buf[12:], uint16((len(substitute)+1)*2))
	le.PutUint16(buf[14:], uint16(len(display)*2))
	offset := 16
	for _, unit := range substitute {
		le.PutUint16(buf[offset:], unit)
		offset += 2
	}
	offset += 2
	for _, unit := range display {
		le.PutUint16(buf[offset:], unit)
		offset += 2
	}

	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var returned uint32
	return windows.DeviceIoControl(handle, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &returned, nil)
}

func isLink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true, nil
	}
	// Go reports junctions as irregular files. Other reparse points (cloud
	// placeholders, say) are irregular too, so only count it if it reads as a link.
	if info.Mode()&os.ModeIrregular != 0 {
		_, err := os.Readlink(path)
		return err == nil, nil
	}
	return false, nil
}

func readLink(path string) (string, error) {
	dest, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(dest, `\??\`), nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
