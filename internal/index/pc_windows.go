package index

import (
	"log/slog"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                         = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemPowerStatus         = kernel32.NewProc("GetSystemPowerStatus")
	procGetSystemTimes               = kernel32.NewProc("GetSystemTimes")
	procGetTickCount64               = kernel32.NewProc("GetTickCount64")
	procGetLastInputInfo             = windows.NewLazySystemDLL("user32.dll").NewProc("GetLastInputInfo")
	procSHQueryUserNotificationState = windows.NewLazySystemDLL("shell32.dll").NewProc("SHQueryUserNotificationState")
)

// systemPC reads the real PC.
type systemPC struct{}

// systemPowerStatus is Windows' SYSTEM_POWER_STATUS.
type systemPowerStatus struct {
	ACLineStatus        byte // 0 offline (battery), 1 online, 255 unknown
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// onBattery reports whether the PC is running on its battery. Desktops, and
// any failure to tell, count as mains power.
func (systemPC) onBattery() bool {
	var s systemPowerStatus
	ok, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s)))
	return ok != 0 && s.ACLineStatus == 0
}

// The shell's QUERY_USER_NOTIFICATION_STATE values that mean something fills
// the screen: a full-screen app or game (busy, Direct3D), presentation mode,
// or a Store app in full screen.
const (
	qunsBusy                 = 2
	qunsRunningD3DFullScreen = 3
	qunsPresentationMode     = 4
	qunsApp                  = 7
)

// fullScreen asks the shell, which tells full-screen windows apart for its
// own notifications. Lumen's own window in front never counts: the user is
// looking at Lumen.
func (systemPC) fullScreen() bool {
	var state int32
	if hr, _, _ := procSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&state))); hr != 0 {
		return false
	}
	switch state {
	case qunsBusy, qunsRunningD3DFullScreen, qunsPresentationMode, qunsApp:
	default:
		return false
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.GetForegroundWindow(), &pid); err == nil &&
		pid == windows.GetCurrentProcessId() {
		return false
	}
	return true
}

// lastInputInfo is Windows' LASTINPUTINFO.
type lastInputInfo struct {
	size uint32
	time uint32 // the tick count of the last input
}

// sinceInput compares the last input's tick count with the current one. Both
// are milliseconds since Windows started, the first in 32 bits, so the
// subtraction is done in 32 bits too and survives its wrap every 49.7 days.
func (systemPC) sinceInput() time.Duration {
	in := lastInputInfo{size: uint32(unsafe.Sizeof(lastInputInfo{}))}
	if ok, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&in))); ok == 0 {
		return 0 // can't tell: assume the user is there
	}
	now, _, _ := procGetTickCount64.Call()
	return time.Duration(uint32(now)-in.time) * time.Millisecond
}

// cpuTimes reads the machine's times (GetSystemTimes: its kernel time
// includes the idle time) and Lumen's own.
func (systemPC) cpuTimes() (cpuTimes, bool) {
	var idle, kernel, user windows.Filetime
	if ok, _, _ := procGetSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user))); ok == 0 {
		return cpuTimes{}, false
	}
	own, ok := ownCPU()
	if !ok {
		return cpuTimes{}, false
	}
	total := ticks(kernel) + ticks(user)
	return cpuTimes{total: total, busy: total - ticks(idle), own: own}, true
}

// ticks is a FILETIME duration (100 ns units) as a time.Duration.
func ticks(f windows.Filetime) time.Duration {
	return time.Duration(int64(f.HighDateTime)<<32|int64(f.LowDateTime)) * 100
}

// jobAccounting is Windows' JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// CountOwnPrograms puts Lumen in a job object before it starts any other
// program, so the processor time of what it starts (WebView2, FFmpeg) counts
// as its own when indexing measures how busy other programs keep the PC. A
// program started before it is not in the job: main calls it before the
// window exists. Calling it again does nothing.
func CountOwnPrograms() { ownJob() }

// ownJob is a job object holding Lumen and every process it starts from
// then on, made once: its accounting adds up their processor time, that of
// processes which have already exited included, so Lumen's own short FFmpeg
// runs never count as other programs keeping the PC busy. It sets no limits.
// A process joins a job for good, so this is process-wide by nature; 0 if
// Windows refused it.
var ownJob = sync.OnceValue(func() windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err == nil {
		if err = windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err == nil {
			return job
		}
		_ = windows.CloseHandle(job)
	}
	slog.Warn("count Lumen's processor time with its programs'; counting its own alone", "err", err)
	return 0
})

// ownCPU is Lumen's processor time: its job's, else its own process's.
func ownCPU() (time.Duration, bool) {
	if job := ownJob(); job != 0 {
		var a jobAccounting
		err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&a)), uint32(unsafe.Sizeof(a)), nil)
		if err == nil {
			return time.Duration(a.TotalUserTime+a.TotalKernelTime) * 100, true
		}
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		return 0, false
	}
	return ticks(kernel) + ticks(user), true
}
