# Packaged beta acceptance

The versioned 0.12.0 layout also has a host-safe scratch acceptance script:

```sh
node e2e/installer/layout.mjs
```

It creates an isolated Unicode installation under `e2e/.work`, validates
bootstrap identity, managed/offline capabilities, process leases, stable
launching and inventory-only cleanup. It never runs NSIS or writes the host's
uninstall registration. It does not replace installed upgrade or signed
activation acceptance.

The package has two independent checks. Neither touches the host's installed
Lumen or its normal library.

## Extracted payload and native workflows

Build with `wails3 task package`, then extract with 7-Zip into an ignored
folder under `e2e/.work`. From Git Bash:

```sh
MSYS2_ARG_CONV_EXCL='*' 7z.exe x 'D:\Downloads\Lumen\bin\Lumen-0.11.0-x64-setup.exe' '-oD:\Downloads\Lumen\e2e\.work\package-extracted' -y
node e2e/installer/package.mjs e2e/.work/package-extracted
```

Use your repository path and current version. The script compares every
runtime file, the executable, notices and bundled offline WebView2 installer
against the build by SHA-256. It rejects unexpected files, apart from NSIS's
known temporary UI plugins. It then runs the standard suites sequentially
against the extracted executable, removing development runtime overrides.
Append suite names for a focused run. Payload evidence is written under
`e2e/.work/package-<version>/`; native evidence uses the standard suite folders.

## Offline Windows Sandbox

On Windows with Windows Sandbox and its `wsb.exe` CLI installed, from Git Bash:

```sh
powershell.exe -NoProfile -ExecutionPolicy Bypass -File e2e/installer/run-sandbox.ps1
```

The default previous installer is `bin/Lumen-0.10.0-x64-setup.exe`;
`-PreviousVersion` selects another already-built version. Both packages must
exist. Close any existing Sandbox first: the runner refuses to reuse it.

The runner maps only test inputs/scripts read-only and a fresh report folder
writable. Networking and clipboard sharing are disabled. Inside the disposable
guest it installs into a custom Unicode path, indexes a generated original,
sets a favourite, album membership and a preference, then checks over-install,
same-version reinstall, running-application refusal, silent uninstall with
library retention, installation again and original/unrelated-file preservation.
It also compares the installed payload, registry version/path and shortcut.
The guest scripts refuse to run in an ordinary host user session.

No Node, Python, developer runtime override or downloaded prerequisite is
supplied to the guest. An absent WebView2 runtime must be supplied by the
installer itself. `cdp.ps1` uses .NET WebSockets to call the bound methods inside
the WebView over its loopback DevTools endpoint, with time and response bounds.
Reports, transcript and the test library are written under
`e2e/.work/installer-<unique id>/results/` before the guest shuts down. A host
VM/container termination is a failed environment run, never passing acceptance.

This is Windows 11 Sandbox acceptance, not a Windows 10 or hardware matrix.
Silent uninstall tests library retention; interactive removal of the library,
SmartScreen behaviour, signing and two genuinely signed release upgrades still
need separate acceptance. A successful extracted-payload run cannot replace
the Sandbox/installed checks.
