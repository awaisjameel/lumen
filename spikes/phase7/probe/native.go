// Package probe contains disposable, offline Phase 7 media measurements.
package probe

/*
#cgo LDFLAGS: -lole32 -lwindowscodecs -luuid -lpsapi
#define COBJMACROS
#include <stdio.h>
#include <windows.h>
#include <wincodec.h>
#include <psapi.h>

static unsigned long long peak_ws(void) {
    PROCESS_MEMORY_COUNTERS p = {0};
    p.cb = sizeof(p);
    if (!GetProcessMemoryInfo(GetCurrentProcess(), &p, sizeof(p))) return 0;
    return p.PeakWorkingSetSize;
}

static unsigned long long peak_commit(void) {
    PROCESS_MEMORY_COUNTERS p = {0};
    p.cb = sizeof(p);
    if (!GetProcessMemoryInfo(GetCurrentProcess(), &p, sizeof(p))) return 0;
    return p.PeakPagefileUsage;
}

static int ac_line(void) {
    SYSTEM_POWER_STATUS p = {0};
    return GetSystemPowerStatus(&p) ? p.ACLineStatus : -1;
}

static int battery_percent(void) {
    SYSTEM_POWER_STATUS p = {0};
    return GetSystemPowerStatus(&p) ? p.BatteryLifePercent : -1;
}

static void machine(void) {
    MEMORYSTATUSEX m = {0}; m.dwLength = sizeof(m);
    SYSTEM_POWER_STATUS p = {0};
    if (GlobalMemoryStatusEx(&m)) printf("physical_bytes=%llu available_bytes=%llu\n", m.ullTotalPhys, m.ullAvailPhys);
    if (GetSystemPowerStatus(&p)) printf("ACLineStatus=%u BatteryFlag=%u BatteryLifePercent=%u\n", p.ACLineStatus, p.BatteryFlag, p.BatteryLifePercent);
    printf("logical_processors=%lu\n", GetActiveProcessorCount(ALL_PROCESSOR_GROUPS));
    fflush(stdout);
}

static void decoders(void) {
    IWICImagingFactory *f = NULL;
    IEnumUnknown *e = NULL;
    IUnknown *u = NULL;
    IWICBitmapDecoderInfo *d = NULL;
    HRESULT init = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    HRESULT hr = CoCreateInstance(&CLSID_WICImagingFactory, NULL, CLSCTX_INPROC_SERVER, &IID_IWICImagingFactory, (void **)&f);
    if (FAILED(hr)) { printf("factory HRESULT=0x%08lX\n", (unsigned long)hr); goto done; }
    hr = IWICImagingFactory_CreateComponentEnumerator(f, WICDecoder, WICComponentEnumerateDefault, &e);
    if (FAILED(hr)) { printf("enumerate HRESULT=0x%08lX\n", (unsigned long)hr); goto done; }
    while (IEnumUnknown_Next(e, 1, &u, NULL) == S_OK) {
        hr = IUnknown_QueryInterface(u, &IID_IWICBitmapDecoderInfo, (void **)&d);
        if (SUCCEEDED(hr)) {
            WCHAR name[1024] = {0}, ext[2048] = {0}, ver[256] = {0}; UINT n = 0;
            IWICBitmapDecoderInfo_GetFriendlyName(d, 1024, name, &n);
            IWICBitmapDecoderInfo_GetFileExtensions(d, 2048, ext, &n);
            IWICBitmapDecoderInfo_GetVersion(d, 256, ver, &n);
            printf("decoder=%ls version=%ls extensions=%ls\n", name, ver, ext);
            IWICBitmapDecoderInfo_Release(d); d = NULL;
        }
        IUnknown_Release(u); u = NULL;
    }
done:
    if (e) IEnumUnknown_Release(e);
    if (f) IWICImagingFactory_Release(f);
    if (SUCCEEDED(init)) CoUninitialize();
    fflush(stdout);
}
*/
import "C"

import "runtime"

func inventory() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	C.machine()
	C.decoders()
}

func peaks() (uint64, uint64) { return uint64(C.peak_ws()), uint64(C.peak_commit()) }

func power() (int, int) { return int(C.ac_line()), int(C.battery_percent()) }
