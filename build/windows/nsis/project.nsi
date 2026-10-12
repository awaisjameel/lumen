Unicode true

####
## Lumen's installer. Build it with `task package` (build/windows/Taskfile.yml),
## which builds Lumen.exe and the notices first, and passes:
##   ARG_WAILS_AMD64_BINARY  Lumen.exe
##   LUMEN_RUNTIME           deps\runtime: the DLLs, ffmpeg\ and models\ that ship beside it
##   LUMEN_NOTICES           THIRD_PARTY_NOTICES.txt (scripts/notices.py)
##   LUMEN_WEBVIEW2          Microsoft's standalone WebView2 runtime installer (scripts/deps.sh)
##   LUMEN_SIGN              optional: a command that signs the file "%1"
## Decisions (docs/phase6-design-2026-10-03.md): installed per user, with no
## administrator prompt; WebView2 ships inside, so installing needs no internet.
####
!define WAILS_INSTALL_SCOPE "user"
!include "wails_tools.nsh"
!include "MUI2.nsh"
!include "nsDialogs.nsh"
!include "LogicLib.nsh"

!macro LUMEN_REQUIRE name
    !ifndef ${name}
        !error "Build the installer with task package: it passes ${name}."
    !endif
!macroend
!insertmacro LUMEN_REQUIRE LUMEN_RUNTIME
!insertmacro LUMEN_REQUIRE LUMEN_PAYLOAD
!insertmacro LUMEN_REQUIRE LUMEN_NOTICES
!insertmacro LUMEN_REQUIRE LUMEN_WEBVIEW2
!include "${LUMEN_PAYLOAD}"
!define LUMEN_INVENTORY ".lumen-runtime-files.txt"

!macro LUMEN_PAYLOAD_CHECK
    ${If} ${Errors}
        FileClose $R9
        SetErrorLevel 4
        Abort "Lumen's runtime files or inventory could not be written. Run Setup again."
    ${EndIf}
!macroend

# Where new versions are published: services.ReleasesURL, which
# TestInstallerNamesTheReleasesPage keeps this in step with.
!define LUMEN_RELEASES "https://github.com/awaisjameel/lumen/releases"
# What Lumen keeps per user: its library (os.UserCacheDir()\Lumen), and
# WebView2's data for its window (Wails' default, %APPDATA%\Lumen.exe).
!define LUMEN_LIBRARY "$LOCALAPPDATA\${INFO_PRODUCTNAME}"
!define LUMEN_WEBVIEW_DATA "$APPDATA\${PRODUCT_EXECUTABLE}"
# WebView2's client key: its "pv" value names the installed runtime.
!define WEBVIEW2_CLIENT "Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

# Version information: four parts.
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"
VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Setup"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PRODUCTNAME}-${INFO_PRODUCTVERSION}-x64-setup.exe"
InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
ManifestDPIAware true
# zlib, not LZMA: most of the payload is model weights, which neither packs
# (zlib keeps 92 % of them, LZMA nearly as much), so LZMA saved under 1 % of
# the installer while its decoder in Setup ran at about 4 MB/s, four minutes
# of installing. Files are packed one by one, so a skipped one (WebView2 on
# most PCs) costs nothing to pass over.
SetCompressor /FINAL zlib
ShowInstDetails show
ShowUninstDetails show

!ifdef LUMEN_SIGN
    !finalize '${LUMEN_SIGN}' = 0
    !uninstfinalize '${LUMEN_SIGN}' = 0
!endif

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_ABORTWARNING

!define MUI_WELCOMEPAGE_TEXT "Lumen organises your photos and videos with AI that runs on this PC. Nothing is uploaded. Update checking is off until you enable it or ask for a check.$\r$\n$\r$\nSetup is offline and installs Lumen for you alone; it needs no administrator rights.$\r$\n$\r$\nClick Next to continue."
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "Start Lumen"
# The finish page's second checkbox: a desktop shortcut, off unless asked for.
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED
!define MUI_FINISHPAGE_SHOWREADME_TEXT "Add a shortcut to the desktop"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION DesktopShortcut
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
UninstPage custom un.LibraryPage un.LibraryPageLeave
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Var RemoveLibrary    ; 1: the uninstaller also removes the library
Var RemoveLibraryBox

Function .onInit
    !insertmacro wails.checkArchitecture
FunctionEnd

# LumenClosed returns once Lumen.exe in $INSTDIR can be replaced or removed:
# Windows keeps a running program's file open. It asks the user to close
# Lumen; silently, or if the user cancels, it stops.
!macro LUMEN_CLOSED prefix
Function ${prefix}LumenClosed
    ${IfNot} ${FileExists} "$INSTDIR\${PRODUCT_EXECUTABLE}"
        Return
    ${EndIf}
    retry:
    ClearErrors
    FileOpen $0 "$INSTDIR\${PRODUCT_EXECUTABLE}" a
    ${IfNot} ${Errors}
        FileClose $0
        Return
    ${EndIf}
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "Lumen is open. Close it, then choose Retry." /SD IDCANCEL IDRETRY retry
    SetErrorLevel 2
    Quit
FunctionEnd
!macroend
!insertmacro LUMEN_CLOSED ""
!insertmacro LUMEN_CLOSED "un."

Function DesktopShortcut
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
FunctionEnd

# HasWebView2 sets $0 to 1 if this PC has the WebView2 runtime, for all users
# or for this one, else 0. A "pv" of 0.0.0.0 is what an uninstalled one leaves.
Function HasWebView2
    SetRegView 64
    ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\${WEBVIEW2_CLIENT}" "pv"
    ${If} $0 == ""
    ${OrIf} $0 == "0.0.0.0"
        ReadRegStr $0 HKCU "Software\${WEBVIEW2_CLIENT}" "pv"
    ${EndIf}
    ${If} $0 == ""
    ${OrIf} $0 == "0.0.0.0"
        StrCpy $0 0
    ${Else}
        StrCpy $0 1
    ${EndIf}
FunctionEnd

# The previous install's inventory is the only authority for runtime cleanup.
# Paths must stay inside $INSTDIR, have no wildcards, and cross no junction or
# symbolic link. Directories are removed only when empty.
!macro LUMEN_REMOVE_RUNTIME prefix
Function ${prefix}RemoveRuntime
    ${IfNot} ${FileExists} "$INSTDIR\${LUMEN_INVENTORY}"
        Return
    ${EndIf}
    ClearErrors
    FileOpen $R0 "$INSTDIR\${LUMEN_INVENTORY}" r
    IfErrors failed
    GetFullPathName $R5 "$INSTDIR\"
    StrLen $R6 $R5
    next:
    ClearErrors
    FileReadUTF16LE $R0 $R1
    IfErrors done
    StrCpy $R7 $R1 1
    StrCpy $R2 $R1 2 -2
    StrCmp $R2 "$\r$\n" 0 invalid
    StrCpy $R2 $R1 -2 1
    StrCmp $R2 "" invalid
    StrCpy $R8 0
    checkChars:
    StrCpy $R4 $R2 1 $R8
    StrCmp $R4 "" checkedChars
    StrCmp $R4 "*" invalid
    StrCmp $R4 "?" invalid
    StrCmp $R4 ":" invalid
    IntOp $R8 $R8 + 1
    Goto checkChars
    checkedChars:
    GetFullPathName $R3 "$INSTDIR\$R2"
    StrCpy $R4 $R3 $R6
    StrCmp $R4 $R5 0 invalid
    StrCpy $R8 $R3
    checkLinks:
    System::Call 'kernel32::GetFileAttributesW(w R8) i.R4'
    StrCmp $R4 -1 noLink
    IntOp $R4 $R4 & 0x400 ; FILE_ATTRIBUTE_REPARSE_POINT
    StrCmp $R4 0 0 invalid
    noLink:
    ${GetParent} "$R8" $R8
    StrLen $R4 $R8
    IntCmp $R4 $R6 checkedLinks checkedLinks checkLinks
    checkedLinks:
    StrCmp $R7 "D" directory
    StrCmp $R7 "F" 0 invalid
    ClearErrors
    Delete "$R3"
    IfErrors invalid
    Goto next
    directory:
    RMDir "$R3" ; non-empty directories hold files Setup did not put there
    Goto next
    invalid:
    FileClose $R0
    Goto failed
    done:
    FileClose $R0
    Delete "$INSTDIR\${LUMEN_INVENTORY}"
    Return
    failed:
    MessageBox MB_ICONSTOP "Lumen's previous runtime could not be removed safely. Check the install folder and try again." /SD IDOK
    SetErrorLevel 4
    Quit
FunctionEnd
!macroend
!insertmacro LUMEN_REMOVE_RUNTIME ""
!insertmacro LUMEN_REMOVE_RUNTIME "un."

Section "Lumen"
    !insertmacro wails.setShellContext
    Call LumenClosed

    ${If} ${FileExists} "$INSTDIR\lumen-update.exe"
        ExecWait '"$INSTDIR\lumen-update.exe" --prepare-install "$INSTDIR"' $0
        ${If} $0 != 0
            SetErrorLevel 2
            Abort "Close every Lumen library before installing."
        ${EndIf}
        ExecWait '"$INSTDIR\lumen-update.exe" --remove-generations "$INSTDIR"' $0
        ${If} $0 != 0
            Abort "Lumen's owned runtime generations could not be removed safely."
        ${EndIf}
    ${EndIf}

    SetOutPath $INSTDIR
    Call RemoveRuntime
    !insertmacro wails.files
    ClearErrors
    FileOpen $R9 "$INSTDIR\${LUMEN_INVENTORY}" w
    ${If} ${Errors}
        SetErrorLevel 4
        Abort "Lumen's runtime inventory could not be written."
    ${EndIf}
    !insertmacro LUMEN_RUNTIME_FILES
    FileClose $R9
    File "${LUMEN_NOTICES}"
    ExecWait '"$INSTDIR\lumen-update.exe" --bootstrap "$INSTDIR"' $0
    ${If} $0 != 0
        Abort "Lumen's installation could not be initialised. Run Setup again."
    ${EndIf}

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    !insertmacro wails.writeUninstaller
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINST_KEY}" "URLInfoAbout" "${LUMEN_RELEASES}"
    WriteRegStr HKCU "${UNINST_KEY}" "URLUpdateInfo" "${LUMEN_RELEASES}"
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
SectionEnd

# Last in the installer's data, so an install that doesn't need the 200 MB
# runtime never unpacks it.
Section "WebView2"
    Call HasWebView2
    ${If} $0 == 1
        DetailPrint "Microsoft Edge WebView2 Runtime is already installed."
        Return
    ${EndIf}
    DetailPrint "Installing Microsoft Edge WebView2 Runtime..."
    InitPluginsDir
    SetOutPath $PLUGINSDIR
    File "/oname=$PLUGINSDIR\MicrosoftEdgeWebView2RuntimeInstallerX64.exe" "${LUMEN_WEBVIEW2}"
    ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebView2RuntimeInstallerX64.exe" /silent /install' $1
    SetOutPath $INSTDIR ; let go of $PLUGINSDIR, so it can be removed
    Call HasWebView2
    ${If} $0 != 1
        MessageBox MB_ICONSTOP "Lumen is installed, but the Microsoft Edge WebView2 Runtime it needs could not be (error $1). Run Setup again, or install the runtime from https://developer.microsoft.com/microsoft-edge/webview2/" /SD IDOK
        SetErrorLevel 3
        Abort
    ${EndIf}
SectionEnd

Function un.LibraryPage
    !insertmacro MUI_HEADER_TEXT "Your library" "Choose whether to keep what Lumen has learned."
    nsDialogs::Create 1018
    Pop $0
    ${NSD_CreateLabel} 0 0 100% 48u "Lumen keeps your library in ${LUMEN_LIBRARY}: the names, albums and blocks you made, what Lumen learned about your photos, thumbnails and its log. Keep it to pick up where you left off if you install Lumen again.$\r$\n$\r$\nYour photos and videos themselves are never touched."
    Pop $0
    ${NSD_CreateCheckbox} 0 56u 100% 12u "&Also remove my Lumen library"
    Pop $RemoveLibraryBox
    nsDialogs::Show
FunctionEnd

Function un.LibraryPageLeave
    ${NSD_GetState} $RemoveLibraryBox $RemoveLibrary
FunctionEnd

Section "uninstall"
    !insertmacro wails.setShellContext
    Call un.LumenClosed
    ${If} ${FileExists} "$INSTDIR\lumen-update.exe"
        ExecWait '"$INSTDIR\lumen-update.exe" --prepare-install "$INSTDIR"' $0
        ${If} $0 != 0
            SetErrorLevel 2
            Abort "Close every Lumen library before uninstalling."
        ${EndIf}
        ExecWait '"$INSTDIR\lumen-update.exe" --remove-generations "$INSTDIR"' $0
        ${If} $0 != 0
            Abort "Lumen's runtime generations could not be removed safely."
        ${EndIf}
    ${EndIf}
    Call un.RemoveRuntime

    # Only what Setup put there: a folder chosen by hand may hold other files.
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\THIRD_PARTY_NOTICES.txt"
    Delete "$INSTDIR\.lumen-install.json"
    Delete "$INSTDIR\.lumen-active.json"
    Delete "$INSTDIR\.lumen-installing"
    Delete "$INSTDIR\.lumen-activating.json"
    Delete "$INSTDIR\.lumen-process-lock"
    RMDir "$INSTDIR\runtime"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    # Silently, the library stays: only a person choosing removes it.
    ${If} $RemoveLibrary == ${BST_CHECKED}
        RMDir /r "${LUMEN_LIBRARY}"
        RMDir /r "${LUMEN_WEBVIEW_DATA}"
    ${EndIf}

    !insertmacro wails.deleteUninstaller
    RMDir $INSTDIR ; only if nothing else is in it
SectionEnd
