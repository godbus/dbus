//go:build !windows && !solaris && !js && !wasip1

package dbus

import (
	"reflect"
	"syscall"
	"unsafe"
)

// readFDs walks one recvmsg's control data. Descriptors from valid messages
// remain owned by the reader even if a later control message is malformed.
func (o *oobReader) readFDs(b []byte) error {
	for len(b) >= syscall.CmsgLen(0) {
		header := *(*syscall.Cmsghdr)(unsafe.Pointer(&b[0]))
		// Len has a platform-dependent width; bound it before converting to int.
		available := header
		available.SetLen(len(b))
		truncated := header.Len > available.Len
		if truncated {
			header.Len = available.Len
		}
		length := int(header.Len)
		if length < syscall.CmsgLen(0) {
			return syscall.EINVAL
		}
		data := b[syscall.CmsgLen(0):length]
		if header.Level == syscall.SOL_SOCKET && header.Type == syscall.SCM_RIGHTS {
			// A truncated rights message can still contain complete installed
			// descriptors. Retain those before reporting the malformed tail.
			for len(data) >= 4 {
				o.fds = append(o.fds, int(int32(nativeEndian.Uint32(data))))
				data = data[4:]
			}
			if len(data) != 0 {
				return syscall.EINVAL
			}
		}
		if truncated {
			return syscall.EINVAL
		}
		space := syscall.CmsgSpace(length - syscall.CmsgLen(0))
		if space >= len(b) {
			break
		}
		b = b[space:]
	}
	return nil
}

// collectUnixFDs examines the final body, so overwritten dictionary entries
// and descriptors only decoded before an error do not transfer ownership.
func collectUnixFDs(value any, fds map[int]struct{}) error {
	switch v := value.(type) {
	case UnixFD:
		fds[int(v)] = struct{}{}
		return nil
	case UnixFDIndex:
		return InvalidMessageError("invalid index for unix fd")
	case Variant:
		return collectUnixFDs(v.Value(), fds)
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Array, reflect.Slice:
		if !typeContainsUnixFD(v.Type().Elem()) {
			return nil
		}
		for i := 0; i < v.Len(); i++ {
			if err := collectUnixFDs(v.Index(i).Interface(), fds); err != nil {
				return err
			}
		}
	case reflect.Map:
		if !typeContainsUnixFD(v.Type().Key()) && !typeContainsUnixFD(v.Type().Elem()) {
			return nil
		}
		iter := v.MapRange()
		for iter.Next() {
			if err := collectUnixFDs(iter.Key().Interface(), fds); err != nil {
				return err
			}
			if err := collectUnixFDs(iter.Value().Interface(), fds); err != nil {
				return err
			}
		}
	}
	return nil
}

func typeContainsUnixFD(t reflect.Type) bool {
	if t == unixFDType || t == unixFDIndexType || t == variantType {
		return true
	}
	switch t.Kind() {
	case reflect.Interface:
		return true
	case reflect.Array, reflect.Slice:
		return typeContainsUnixFD(t.Elem())
	case reflect.Map:
		return typeContainsUnixFD(t.Key()) || typeContainsUnixFD(t.Elem())
	default:
		return false
	}
}
