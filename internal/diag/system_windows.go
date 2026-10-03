package diag

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// Version is Lumen.exe's own version, as Explorer shows it: the version
// resource's ProductVersion, which the build sets from build/config.yml's
// info.version, else its fixed file version. "dev" for a build without one
// (go run, tests).
func Version() string {
	exe, err := os.Executable()
	if err != nil {
		return devVersion
	}
	size, err := windows.GetFileVersionInfoSize(exe, nil)
	if err != nil || size == 0 {
		return devVersion
	}
	buf := make([]byte, size)
	block := unsafe.Pointer(&buf[0])
	if err := windows.GetFileVersionInfo(exe, 0, size, block); err != nil {
		return devVersion
	}
	// The strings are kept per language and code page, which the
	// translation table lists; Lumen's are language neutral.
	var lang *[2]uint16
	var n uint32
	if err := windows.VerQueryValue(block, `\VarFileInfo\Translation`, unsafe.Pointer(&lang), &n); err == nil && n >= 4 {
		var v *uint16
		key := fmt.Sprintf(`\StringFileInfo\%04x%04x\ProductVersion`, lang[0], lang[1])
		if err := windows.VerQueryValue(block, key, unsafe.Pointer(&v), &n); err == nil && n > 1 {
			if s := windows.UTF16PtrToString(v); s != "" {
				return s
			}
		}
	}
	var fixed *windows.VS_FIXEDFILEINFO
	if err := windows.VerQueryValue(block, `\`, unsafe.Pointer(&fixed), &n); err != nil ||
		fixed == nil || n < uint32(unsafe.Sizeof(*fixed)) || fixed.FileVersionMS|fixed.FileVersionLS == 0 {
		return devVersion
	}
	return versionString(fixed.FileVersionMS>>16, fixed.FileVersionMS&0xffff,
		fixed.FileVersionLS>>16, fixed.FileVersionLS&0xffff)
}

// windowsVersion is the Windows version and build, as "10.0.26200".
func windowsVersion() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}

// memoryStatusEx is Windows' MEMORYSTATUSEX.
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

// System describes this PC. A part that can't be read is left empty.
func System() PC {
	pc := PC{Windows: windowsVersion(), Threads: threads()}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`,
		registry.QUERY_VALUE); err == nil {
		name, _, _ := k.GetStringValue("ProcessorNameString")
		pc.CPU = strings.TrimSpace(name)
		_ = k.Close()
	}
	m := memoryStatusEx{length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if ok, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); ok != 0 {
		pc.MemoryBytes = m.totalPhys
	}
	pc.Graphics = graphicsAdapters()
	return pc
}

// displayClass is the device class of display adapters: each adapter has a
// numbered subkey naming its driver.
const displayClass = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`

// graphicsAdapters names the display adapters Windows has drivers for.
func graphicsAdapters() []string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, displayClass, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer func() { _ = k.Close() }()
	subs, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var names []string
	for _, s := range subs {
		a, err := registry.OpenKey(k, s, registry.QUERY_VALUE)
		if err != nil {
			continue // "Properties" and the like: not an adapter, or not readable
		}
		if name, _, err := a.GetStringValue("DriverDesc"); err == nil && name != "" {
			names = append(names, name)
		}
		_ = a.Close()
	}
	return names
}
